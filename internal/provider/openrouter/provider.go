package openrouter

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
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultModel   = "openai/gpt-3.5-turbo"
)

// Provider implements the LLMProvider interface for OpenRouter
type Provider struct {
	config     *provider.Config
	httpClient *http.Client
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

// New creates a new OpenRouter provider instance
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

		// Create scanner for SSE stream
		scanner := bufio.NewScanner(resp.Body)
		var usage *pb.UsageInfo
		var finishReason string

		for scanner.Scan() {
			// Get the line
			line := scanner.Text()

			// Skip empty lines
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
				// Send final message with accumulated usage
				responseChan <- &pb.LLMStreamResponse{
					ContentChunk: "",
					IsFinal:      true,
					Usage:        usage,
				}
				return
			}

			// Parse the SSE data
			var streamResp streamResponseBody
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				errorChan <- fmt.Errorf("failed to parse SSE data: %w", err)
				return
			}

			// Check if we have choices
			if len(streamResp.Choices) == 0 {
				continue
			}

			// Update usage if available
			if streamResp.Usage != nil {
				usage = &pb.UsageInfo{
					PromptTokens:     streamResp.Usage.PromptTokens,
					CompletionTokens: streamResp.Usage.CompletionTokens,
					TotalTokens:      streamResp.Usage.TotalTokens,
				}
			}

			// Get the content chunk and finish reason
			chunk := streamResp.Choices[0].Delta.Content
			if streamResp.Choices[0].FinishReason != "" {
				finishReason = streamResp.Choices[0].FinishReason
			}

			// Send non-empty chunks
			if chunk != "" {
				responseChan <- &pb.LLMStreamResponse{
					ContentChunk: chunk,
					IsFinal:      false,
					Usage:        usage,
				}
			}

			// If we have a finish reason but haven't sent the final message yet
			if finishReason != "" && usage != nil {
				responseChan <- &pb.LLMStreamResponse{
					ContentChunk: "",
					IsFinal:      true,
					Usage:        usage,
				}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading stream: %w", err)
			return
		}
	}()

	return responseChan, errorChan
}
