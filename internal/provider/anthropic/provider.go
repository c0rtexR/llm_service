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

	"llmservice/internal/provider"
	pb "llmservice/proto"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	defaultModel   = "claude-2"
	apiVersion     = "2023-06-01" // Use a recent stable version
)

// Provider implements the LLMProvider interface for Anthropic
type Provider struct {
	config     *provider.Config
	httpClient *http.Client
}

// requestBody represents the JSON structure for Anthropic API requests
type requestBody struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	System      string        `json:"system,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float32      `json:"temperature,omitempty"`
	MaxTokens   *int32        `json:"max_tokens,omitempty"`
	TopP        *float32      `json:"top_p,omitempty"`
}

// chatMessage represents a single message in the Anthropic format
type chatMessage struct {
	Role         string       `json:"role"`
	Content      string       `json:"content"`
	CacheControl *cacheConfig `json:"cache_control,omitempty"`
}

// cacheConfig represents Anthropic's cache control settings
type cacheConfig struct {
	Type string `json:"type"`
}

// responseBody represents the JSON structure for Anthropic API responses
type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Usage   struct {
		InputTokens              int32 `json:"input_tokens"`
		OutputTokens             int32 `json:"output_tokens"`
		CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
		CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
	} `json:"usage"`
}

// streamResponseBody represents a single chunk in the SSE stream
type streamResponseBody struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Usage   *struct {
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
	var systemMsg string

	for _, msg := range req.Messages {
		// Extract system message if present
		if msg.Role == "system" {
			systemMsg = msg.Content
			continue
		}

		// Create chat message
		chatMsg := chatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle cache control if present
		if msg.CacheControl != nil && msg.CacheControl.Type == "ephemeral" {
			chatMsg.CacheControl = &cacheConfig{
				Type: "ephemeral",
			}
		}

		messages = append(messages, chatMsg)
	}

	// Prepare request body
	body := requestBody{
		Model:    model,
		Messages: messages,
		System:   systemMsg,
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
		fmt.Sprintf("%s/messages", p.config.BaseURL),
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", p.config.APIKey)
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
	return &pb.LLMResponse{
		Content: response.Content,
		Usage: &pb.UsageInfo{
			PromptTokens:             response.Usage.InputTokens,
			CompletionTokens:         response.Usage.OutputTokens,
			TotalTokens:              response.Usage.InputTokens + response.Usage.OutputTokens,
			CacheReadInputTokens:     response.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: response.Usage.CacheCreationInputTokens,
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
		var systemMsg string

		for _, msg := range req.Messages {
			// Extract system message if present
			if msg.Role == "system" {
				systemMsg = msg.Content
				continue
			}

			// Create chat message
			chatMsg := chatMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}

			// Handle cache control if present
			if msg.CacheControl != nil && msg.CacheControl.Type == "ephemeral" {
				chatMsg.CacheControl = &cacheConfig{
					Type: "ephemeral",
				}
			}

			messages = append(messages, chatMsg)
		}

		// Prepare request body
		body := requestBody{
			Model:    model,
			Messages: messages,
			System:   systemMsg,
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
			fmt.Sprintf("%s/messages", p.config.BaseURL),
			bytes.NewReader(jsonBody))
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}

		// Set headers
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-API-Key", p.config.APIKey)
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

			// Update usage if available
			if streamResp.Usage != nil {
				usage = &pb.UsageInfo{
					PromptTokens:             streamResp.Usage.InputTokens,
					CompletionTokens:         streamResp.Usage.OutputTokens,
					TotalTokens:              streamResp.Usage.InputTokens + streamResp.Usage.OutputTokens,
					CacheReadInputTokens:     streamResp.Usage.CacheReadInputTokens,
					CacheCreationInputTokens: streamResp.Usage.CacheCreationInputTokens,
				}
			}

			// Handle different event types
			switch streamResp.Type {
			case "content_block_start":
				// Nothing to do, just wait for content
				continue
			case "content_block":
				// Send the content chunk
				if streamResp.Content != "" {
					responseChan <- &pb.LLMStreamResponse{
						ContentChunk: streamResp.Content,
						IsFinal:      false,
						Usage:        usage,
					}
				}
			case "content_block_stop":
				// Send final message with usage
				responseChan <- &pb.LLMStreamResponse{
					ContentChunk: "",
					IsFinal:      true,
					Usage:        usage,
				}
				return
			case "error":
				errorChan <- fmt.Errorf("received error event: %s", streamResp.Content)
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
