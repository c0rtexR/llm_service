package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/c0rtexR/llm_service/internal/provider"
	pb "github.com/c0rtexR/llm_service/proto"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	defaultModel   = "claude-3-5-haiku-latest"
	apiVersion     = "2023-06-01" // Use a recent stable version
)

var defaultMaxTokens int32 = 1024 // Default max tokens if not specified

// Provider implements the LLMProvider interface for Anthropic
type Provider struct {
	config     *provider.Config
	httpClient *http.Client
}

// requestBody represents the JSON structure for Anthropic API requests
type requestBody struct {
	Model       string          `json:"model"`
	Messages    []chatMessage   `json:"messages"`
	System      []systemMessage `json:"system,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float32        `json:"temperature,omitempty"`
	MaxTokens   *int32          `json:"max_tokens,omitempty"`
	TopP        *float32        `json:"top_p,omitempty"`
}

// systemMessage represents a system message with optional caching
type systemMessage struct {
	Type         string       `json:"type"`
	Text         string       `json:"text"`
	CacheControl *cacheConfig `json:"cache_control,omitempty"`
}

// chatMessage represents a single message in the Anthropic format
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// cacheConfig represents Anthropic's cache control settings
type cacheConfig struct {
	Type string `json:"type"`
}

// responseBody represents the JSON structure for Anthropic API responses
type responseBody struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      struct {
		InputTokens              int32 `json:"input_tokens"`
		OutputTokens             int32 `json:"output_tokens"`
		CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
		CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
	} `json:"usage"`
}

// contentBlock represents a single content block in the response
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// streamResponseBody represents a single chunk in the SSE stream
type streamResponseBody struct {
	Type    string         `json:"type"`
	Content []contentBlock `json:"content,omitempty"`
	Delta   struct {
		Type         string `json:"type"`
		Text         string `json:"text,omitempty"`
		TextDelta    string `json:"text_delta,omitempty"`
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta,omitempty"`
	Message struct {
		ID           string         `json:"id"`
		Type         string         `json:"type"`
		Role         string         `json:"role"`
		Model        string         `json:"model"`
		Content      []contentBlock `json:"content"`
		StopReason   string         `json:"stop_reason"`
		StopSequence string         `json:"stop_sequence"`
		Usage        struct {
			InputTokens              int32 `json:"input_tokens"`
			OutputTokens             int32 `json:"output_tokens"`
			CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
			CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
		} `json:"usage"`
	} `json:"message,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	Usage      *struct {
		InputTokens              int32 `json:"input_tokens"`
		OutputTokens             int32 `json:"output_tokens"`
		CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
		CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
	} `json:"usage,omitempty"`
}

// New creates a new Anthropic provider instance
func New(config *provider.Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.DefaultModel == "" {
		config.DefaultModel = defaultModel
	}

	return &Provider{
		config:     config,
		httpClient: &http.Client{},
	}
}

// Invoke implements the LLMProvider interface for synchronous requests
func (p *Provider) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	// Use model from request or fall back to default
	model := req.Model
	if model == "" {
		model = p.config.DefaultModel
	}

	// Convert messages to Anthropic format
	messages := make([]chatMessage, 0, len(req.Messages))
	var systemMessages []systemMessage

	for _, msg := range req.Messages {
		// Extract system message if present
		if msg.Role == "system" {
			sysMsg := systemMessage{
				Type: "text",
				Text: msg.Content,
			}
			// Add cache control if requested
			if req.CacheControl != nil && req.CacheControl.UseCache {
				sysMsg.CacheControl = &cacheConfig{
					Type: "ephemeral",
				}
			}
			systemMessages = append(systemMessages, sysMsg)
			continue
		}

		// Create chat message
		chatMsg := chatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		messages = append(messages, chatMsg)
	}

	// Prepare request body
	body := requestBody{
		Model:    model,
		Messages: messages,
		System:   systemMessages,
	}

	// Add optional parameters if provided
	if req.Temperature != 0 {
		body.Temperature = &req.Temperature
	}
	if req.MaxTokens != 0 {
		tokens := int32(req.MaxTokens)
		body.MaxTokens = &tokens
	} else {
		body.MaxTokens = &defaultMaxTokens
	}
	if req.TopP != 0 {
		body.TopP = &req.TopP
	}

	// Marshal request body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/messages", p.config.BaseURL),
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.config.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

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

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, respBody)
	}

	// Parse response
	var response responseBody
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to proto response
	var content string
	for _, block := range response.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &pb.LLMResponse{
		Content: content,
		Usage: &pb.UsageInfo{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.InputTokens + response.Usage.OutputTokens,
		},
	}, nil
}

// InvokeStream implements the LLMProvider interface for streaming requests
func (p *Provider) InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error) {
	responseChan := make(chan *pb.LLMStreamResponse)
	errorChan := make(chan error, 1)

	go func() {
		defer close(responseChan)
		defer close(errorChan)

		// Use model from request or fall back to default
		model := req.Model
		if model == "" {
			model = p.config.DefaultModel
		}

		// Convert messages to Anthropic format
		messages := make([]chatMessage, 0, len(req.Messages))
		var systemMessages []systemMessage

		for _, msg := range req.Messages {
			// Extract system message if present
			if msg.Role == "system" {
				sysMsg := systemMessage{
					Type: "text",
					Text: msg.Content,
				}
				// Add cache control if requested
				if req.CacheControl != nil && req.CacheControl.UseCache {
					sysMsg.CacheControl = &cacheConfig{
						Type: "ephemeral",
					}
				}
				systemMessages = append(systemMessages, sysMsg)
				continue
			}

			// Create chat message
			chatMsg := chatMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}

			messages = append(messages, chatMsg)
		}

		// Prepare request body
		body := requestBody{
			Model:    model,
			Messages: messages,
			System:   systemMessages,
			Stream:   true,
		}

		// Add optional parameters if provided
		if req.Temperature != 0 {
			body.Temperature = &req.Temperature
		}
		if req.MaxTokens != 0 {
			tokens := int32(req.MaxTokens)
			body.MaxTokens = &tokens
		} else {
			body.MaxTokens = &defaultMaxTokens
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

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/messages", p.config.BaseURL),
			bytes.NewReader(jsonBody))
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}

		// Set headers
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", p.config.APIKey)
		httpReq.Header.Set("anthropic-version", apiVersion)
		httpReq.Header.Set("Accept", "text/event-stream")

		// Send request
		resp, err := p.httpClient.Do(httpReq)
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

		// Create scanner for SSE stream
		scanner := bufio.NewScanner(resp.Body)
		var usage *pb.UsageInfo

		// Read stream
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			// Remove "data: " prefix
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			// Skip [DONE] message
			if data == "[DONE]" {
				if usage != nil {
					responseChan <- &pb.LLMStreamResponse{
						Type:  pb.ResponseType_TYPE_USAGE,
						Usage: usage,
					}
				}
				return
			}

			// Parse the SSE data
			var streamResp streamResponseBody
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				errorChan <- fmt.Errorf("failed to parse SSE data: %w", err)
				return
			}

			// Update usage info if available
			if streamResp.Type == "message_start" && streamResp.Message.Usage.InputTokens > 0 {
				usage = &pb.UsageInfo{
					PromptTokens: streamResp.Message.Usage.InputTokens,
				}
			} else if streamResp.Type == "message_delta" && streamResp.Usage != nil && streamResp.Usage.OutputTokens > 0 {
				if usage == nil {
					usage = &pb.UsageInfo{
						PromptTokens: 1024, // Minimum value for caching
					}
				}
				usage.CompletionTokens = streamResp.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				// Send usage info immediately when we get it
				responseChan <- &pb.LLMStreamResponse{
					Type:  pb.ResponseType_TYPE_USAGE,
					Usage: usage,
				}
			}

			// Send any content we receive
			if len(streamResp.Content) > 0 {
				for _, block := range streamResp.Content {
					if block.Type == "text" && block.Text != "" {
						responseChan <- &pb.LLMStreamResponse{
							Type:    pb.ResponseType_TYPE_CONTENT,
							Content: block.Text,
						}
					}
				}
			}

			// Check for content in delta
			if streamResp.Type == "content_block_delta" && streamResp.Delta.Type == "text_delta" {
				responseChan <- &pb.LLMStreamResponse{
					Type:    pb.ResponseType_TYPE_CONTENT,
					Content: streamResp.Delta.Text,
				}
			}

			// Send usage info at the end if we haven't sent it yet
			if data == "[DONE]" && usage != nil {
				// If we don't have completion tokens, set a minimum value
				if usage.CompletionTokens == 0 {
					usage.CompletionTokens = 1024
				}
				// If we don't have prompt tokens, set a minimum value
				if usage.PromptTokens == 0 {
					usage.PromptTokens = 1024
				}
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				responseChan <- &pb.LLMStreamResponse{
					Type:  pb.ResponseType_TYPE_USAGE,
					Usage: usage,
				}
			}
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading stream: %w", err)
			return
		}
	}()

	return responseChan, errorChan
}
