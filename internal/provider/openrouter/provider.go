package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/c0rtexR/llm_service/internal/provider"
	pb "github.com/c0rtexR/llm_service/proto"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultModel   = "google/gemini-flash-1.5-8b"
)

// Provider implements the LLMProvider interface for OpenRouter
type Provider struct {
	config     *provider.Config
	httpClient *http.Client
}

var defaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	MaxIdleConnsPerHost:   10,
	ReadBufferSize:        64 * 1024,
	WriteBufferSize:       64 * 1024,
	DisableCompression:    true,
}

// requestBody represents the JSON structure for OpenRouter API requests
type requestBody struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float32      `json:"temperature,omitempty"`
	MaxTokens   *int32        `json:"max_tokens,omitempty"`
	TopP        *float32      `json:"top_p,omitempty"`
}

// chatMessage represents a single message in the OpenRouter format
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// responseBody represents the JSON structure for OpenRouter API responses
type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage"`
}

// streamResponseBody represents a single chunk in the SSE stream
type streamResponseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// streamProcessor handles the SSE stream processing
type streamProcessor struct {
	reader       *bufio.Reader
	responseChan chan<- *pb.LLMStreamResponse
	errorChan    chan<- error
	ctx          context.Context
}

func newStreamProcessor(ctx context.Context, body io.Reader, responseChan chan<- *pb.LLMStreamResponse, errorChan chan<- error) *streamProcessor {
	return &streamProcessor{
		reader:       bufio.NewReaderSize(body, 64*1024),
		responseChan: responseChan,
		errorChan:    errorChan,
		ctx:          ctx,
	}
}

func (sp *streamProcessor) process() {
	var usage *pb.UsageInfo
	dataChan := make(chan string, 100)
	errChan := make(chan error, 1)

	// Start reading goroutine
	go func() {
		defer close(dataChan)
		defer close(errChan)

		for {
			select {
			case <-sp.ctx.Done():
				return
			default:
				line, err := sp.reader.ReadString('\n')
				if err != nil {
					if err != io.EOF {
						errChan <- fmt.Errorf("error reading stream: %w", err)
					}
					return
				}

				select {
				case dataChan <- line:
				case <-sp.ctx.Done():
					return
				}
			}
		}
	}()

	// Process data
	for {
		select {
		case <-sp.ctx.Done():
			return
		case err := <-errChan:
			if err != nil {
				sp.errorChan <- err
			}
			return
		case line, ok := <-dataChan:
			if !ok {
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				if usage != nil {
					sp.sendResponse(&pb.LLMStreamResponse{
						Type:  pb.ResponseType_TYPE_USAGE,
						Usage: usage,
					})
				}
				return
			}

			var streamResp streamResponseBody
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				sp.errorChan <- fmt.Errorf("failed to parse SSE data: %w", err)
				return
			}

			if len(streamResp.Choices) == 0 {
				continue
			}

			if streamResp.Usage != nil {
				usage = &pb.UsageInfo{
					PromptTokens:     streamResp.Usage.PromptTokens,
					CompletionTokens: streamResp.Usage.CompletionTokens,
					TotalTokens:      streamResp.Usage.TotalTokens,
				}
			}

			chunk := streamResp.Choices[0].Delta.Content
			if streamResp.Choices[0].FinishReason != "" {
				sp.sendResponse(&pb.LLMStreamResponse{
					Type:         pb.ResponseType_TYPE_FINISH_REASON,
					FinishReason: streamResp.Choices[0].FinishReason,
				})
			}

			if chunk != "" {
				sp.sendResponse(&pb.LLMStreamResponse{
					Type:    pb.ResponseType_TYPE_CONTENT,
					Content: chunk,
				})
			}
		}
	}
}

func (sp *streamProcessor) sendResponse(resp *pb.LLMStreamResponse) {
	select {
	case sp.responseChan <- resp:
	case <-sp.ctx.Done():
	}
}

// New creates a new OpenRouter provider instance
func New(config *provider.Config) *Provider {
	logger := zap.L()

	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.DefaultModel == "" {
		config.DefaultModel = defaultModel
	}

	logger.Info("initializing OpenRouter provider",
		zap.String("base_url", config.BaseURL),
		zap.String("default_model", config.DefaultModel),
		zap.String("api_key_length", fmt.Sprintf("%d", len(config.APIKey))))

	return &Provider{
		config: config,
		httpClient: &http.Client{
			Transport: defaultTransport,
			Timeout:   30 * time.Second,
		},
	}
}

// Invoke implements the LLMProvider interface for synchronous requests
func (p *Provider) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	logger := zap.L()

	// Use model from request or fall back to default
	model := req.Model
	if model == "" {
		model = p.config.DefaultModel
	}

	logger.Info("preparing OpenRouter request",
		zap.String("model", model),
		zap.Int("messages_count", len(req.Messages)),
		zap.String("api_key_length", fmt.Sprintf("%d", len(p.config.APIKey))))

	// Convert messages to OpenRouter format
	messages := make([]chatMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = chatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Prepare request body
	body := requestBody{
		Model:    model,
		Messages: messages,
	}

	// Add optional parameters if provided
	if req.Temperature != 0 {
		body.Temperature = &req.Temperature
	}
	if req.MaxTokens != 0 {
		body.MaxTokens = &req.MaxTokens
	}
	if req.TopP != 0 {
		body.TopP = &req.TopP
	}

	// Marshal request body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Debug("request body", zap.String("body", string(jsonBody)))

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/chat/completions", p.config.BaseURL),
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	authHeader := fmt.Sprintf("Bearer %s", p.config.APIKey)
	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/your-username/llm-service")
	httpReq.Header.Set("X-Title", "LLM Service - Cursor IDE")
	httpReq.Header.Set("User-Agent", "github.com/c0rtexR/llm_service/1.0.0")

	logger.Info("sending request to OpenRouter",
		zap.String("url", httpReq.URL.String()),
		zap.Strings("headers", []string{
			"Content-Type: " + httpReq.Header.Get("Content-Type"),
			"HTTP-Referer: " + httpReq.Header.Get("HTTP-Referer"),
			"X-Title: " + httpReq.Header.Get("X-Title"),
			"User-Agent: " + httpReq.Header.Get("User-Agent"),
			"Authorization-Length: " + fmt.Sprintf("%d", len(authHeader)),
		}))

	// Send request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.Info("received response from OpenRouter",
		zap.Int("status_code", resp.StatusCode),
		zap.String("response", string(respBody)))

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, respBody)
	}

	// Parse response
	var response responseBody
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if we have any choices
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices in response")
	}

	logger.Info("successfully processed OpenRouter response",
		zap.String("model", response.Model),
		zap.Int32("total_tokens", response.Usage.TotalTokens))

	// Convert to proto response
	return &pb.LLMResponse{
		Content: response.Choices[0].Message.Content,
		Usage: &pb.UsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
	}, nil
}

// InvokeStream implements the LLMProvider interface for streaming requests
func (p *Provider) InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error) {
	responseChan := make(chan *pb.LLMStreamResponse, 1000)
	errorChan := make(chan error, 1)

	go func() {
		defer close(responseChan)
		defer close(errorChan)

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		// Use model from request or fall back to default
		model := req.Model
		if model == "" {
			model = p.config.DefaultModel
		}

		// Pre-allocate messages slice with capacity
		messages := make([]chatMessage, 0, len(req.Messages))
		for _, msg := range req.Messages {
			messages = append(messages, chatMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}

		// Prepare request body
		body := requestBody{
			Model:    model,
			Messages: messages,
			Stream:   true,
		}

		// Add optional parameters if provided
		if req.Temperature != 0 {
			body.Temperature = &req.Temperature
		}
		if req.MaxTokens != 0 {
			body.MaxTokens = &req.MaxTokens
		}
		if req.TopP != 0 {
			body.TopP = &req.TopP
		}

		// Marshal request body
		jsonBody, err := json.Marshal(body)
		if err != nil {
			errorChan <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}

		// Create HTTP request with optimized buffer
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/chat/completions", p.config.BaseURL),
			bytes.NewBuffer(jsonBody))
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}

		// Set headers
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("HTTP-Referer", "https://github.com/your-username/llm-service")
		httpReq.Header.Set("X-Title", "LLM Service - Cursor IDE")
		httpReq.Header.Set("User-Agent", "github.com/c0rtexR/llm_service/1.0.0")
		httpReq.Header.Set("Connection", "keep-alive")
		httpReq.Header.Set("Cache-Control", "no-cache")
		httpReq.Header.Set("Transfer-Encoding", "chunked")

		// Send request with timeout
		client := &http.Client{
			Transport: defaultTransport,
			Timeout:   60 * time.Second,
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			errorChan <- fmt.Errorf("failed to send request: %w", err)
			return
		}
		defer resp.Body.Close()

		// Check for error response
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("request failed with status %d: %s", resp.StatusCode, body)
			return
		}

		// Create stream processor
		processor := newStreamProcessor(ctx, resp.Body, responseChan, errorChan)
		processor.process()
	}()

	return responseChan, errorChan
}
