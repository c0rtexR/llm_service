package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"llmservice/internal/provider"
	pb "llmservice/proto"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-3.5-turbo"
)

// Provider implements the LLMProvider interface for OpenAI
type Provider struct {
	config     *provider.Config
	httpClient *http.Client
}

// requestBody represents the JSON structure for OpenAI API requests
type requestBody struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float32      `json:"temperature,omitempty"`
	MaxTokens   *int32        `json:"max_tokens,omitempty"`
	TopP        *float32      `json:"top_p,omitempty"`
}

// chatMessage represents a single message in the OpenAI format
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// responseBody represents the JSON structure for OpenAI API responses
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
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// New creates a new OpenAI provider instance
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

	// Convert messages to OpenAI format
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

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/chat/completions", p.config.BaseURL),
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))

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

	// Check if we have any choices
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices in response")
	}

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

		// Convert messages to OpenAI format
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

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/chat/completions", p.config.BaseURL),
			bytes.NewReader(jsonBody))
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}

		// Set headers
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
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

		// Create scanner to read SSE stream
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
			line = strings.TrimPrefix(line, "data: ")

			// Check for stream end
			if line == "[DONE]" {
				if usage != nil {
					responseChan <- &pb.LLMStreamResponse{
						Type:  pb.ResponseType_TYPE_USAGE,
						Usage: usage,
					}
				}
				return
			}

			// Parse response chunk
			var chunk streamResponseBody
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				errorChan <- fmt.Errorf("failed to parse chunk: %w", err)
				return
			}

			// Check if we have any choices
			if len(chunk.Choices) == 0 {
				continue
			}

			// If we have usage info, save it for the final message
			if chunk.Usage != nil {
				usage = &pb.UsageInfo{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
				continue
			}

			// Get content from delta
			content := chunk.Choices[0].Delta.Content
			if content == "" {
				continue
			}

			// Send content chunk
			responseChan <- &pb.LLMStreamResponse{
				Type:    pb.ResponseType_TYPE_CONTENT,
				Content: content,
			}

			// Check for finish reason
			if chunk.Choices[0].FinishReason != "" {
				responseChan <- &pb.LLMStreamResponse{
					Type:         pb.ResponseType_TYPE_FINISH_REASON,
					FinishReason: chunk.Choices[0].FinishReason,
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
