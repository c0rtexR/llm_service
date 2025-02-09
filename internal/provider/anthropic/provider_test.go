package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c0rtexR/llm_service/internal/provider"
	pb "github.com/c0rtexR/llm_service/proto"
)

func TestNew(t *testing.T) {
	// Test with empty config
	cfg := provider.NewConfig("test-key", "")
	p := New(cfg)
	require.NotNil(t, p)
	require.Equal(t, defaultModel, p.config.DefaultModel)
	require.Equal(t, defaultBaseURL, p.config.BaseURL)

	// Test with custom config
	cfg = provider.NewConfig("test-key", "custom-model").
		WithBaseURL("https://custom.api.com")
	p = New(cfg)
	require.NotNil(t, p)
	require.Equal(t, "custom-model", p.config.DefaultModel)
	require.Equal(t, "https://custom.api.com", p.config.BaseURL)
}

func TestInvoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/messages", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		// Read request body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer r.Body.Close()

		var reqBody requestBody
		err = json.Unmarshal(body, &reqBody)
		require.NoError(t, err)

		// Verify request body
		require.Equal(t, defaultModel, reqBody.Model)
		require.Equal(t, "user", reqBody.Messages[0].Role)
		require.Equal(t, "Hello", reqBody.Messages[0].Content)

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := `{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": "Hello world!",
			"model": "claude-3",
			"usage": {
				"input_tokens": 10,
				"output_tokens": 20
			}
		}`
		_, err = w.Write([]byte(response))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create provider
	config := provider.NewConfig("test-key", defaultModel).WithBaseURL(server.URL)
	p := New(config)

	// Create request
	req := &pb.LLMRequest{
		Model: defaultModel,
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// Invoke request
	resp, err := p.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify response
	require.Equal(t, "Hello world!", resp.Content)
	require.NotNil(t, resp.Usage)
	require.Equal(t, int32(10), resp.Usage.PromptTokens)
	require.Equal(t, int32(20), resp.Usage.CompletionTokens)
	require.Equal(t, int32(30), resp.Usage.TotalTokens)
}

func TestInvokeWithCacheHit(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send response with cache hit
		resp := responseBody{
			ID:    "test-id",
			Model: "test-model",
			Type:  "message",
			Role:  "assistant",
			Content: []contentBlock{
				{
					Type: "text",
					Text: "Hello, how can I help?",
				},
			},
			Usage: struct {
				InputTokens              int32 `json:"input_tokens"`
				OutputTokens             int32 `json:"output_tokens"`
				CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
				CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
			}{
				InputTokens:          5,
				OutputTokens:         10,
				CacheReadInputTokens: 5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with test server URL
	cfg := provider.NewConfig("test-key", "test-model").
		WithBaseURL(server.URL)
	p := New(cfg)

	// Create test request
	req := &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// Test successful request with cache hit
	resp, err := p.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "Hello, how can I help?", resp.Content)
	require.Equal(t, int32(5), resp.Usage.PromptTokens)
	require.Equal(t, int32(10), resp.Usage.CompletionTokens)
	require.Equal(t, int32(15), resp.Usage.TotalTokens)
}

func TestInvokeErrors(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid request"))
	}))
	defer server.Close()

	// Create provider with test server URL
	cfg := provider.NewConfig("test-key", "test-model").
		WithBaseURL(server.URL)
	p := New(cfg)

	// Create test request
	req := &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// Test error response
	resp, err := p.Invoke(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "request failed with status 400")
}

func TestInvokeStream(t *testing.T) {
	chunks := []string{"Hello", ", ", "how", " ", "can", " ", "I", " ", "help", "?"}

	// Create a test server that sends SSE chunks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/messages", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "test-key", r.Header.Get("X-API-Key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))

		// Parse request body
		var reqBody requestBody
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request contents
		require.Equal(t, "test-model", reqBody.Model)
		require.True(t, reqBody.Stream)

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		// Send chunks
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// Send content block start
		startResp := streamResponseBody{
			Type: "content_block_start",
		}
		data, err := json.Marshal(startResp)
		require.NoError(t, err)
		_, err = fmt.Fprintf(w, "data: %s\n\n", data)
		require.NoError(t, err)
		flusher.Flush()

		// Send content chunks
		for i, chunk := range chunks {
			resp := streamResponseBody{
				Type: "content_block",
				Content: []contentBlock{
					{
						Type: "text",
						Text: chunk,
					},
				},
			}

			// Add usage info to last chunk
			if i == len(chunks)-1 {
				resp.Usage = &struct {
					InputTokens              int32 `json:"input_tokens"`
					OutputTokens             int32 `json:"output_tokens"`
					CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
					CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
				}{
					InputTokens:              5,
					OutputTokens:             10,
					CacheReadInputTokens:     5,
					CacheCreationInputTokens: 0,
				}
			}

			data, err := json.Marshal(resp)
			require.NoError(t, err)

			_, err = fmt.Fprintf(w, "data: %s\n\n", data)
			require.NoError(t, err)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond) // Simulate network delay
		}

		// Send content block stop
		stopResp := streamResponseBody{
			Type: "content_block_stop",
		}
		data, err = json.Marshal(stopResp)
		require.NoError(t, err)
		_, err = fmt.Fprintf(w, "data: %s\n\n", data)
		require.NoError(t, err)
		flusher.Flush()

		// Send final [DONE] message
		_, err = fmt.Fprintf(w, "data: [DONE]\n\n")
		require.NoError(t, err)
		flusher.Flush()
	}))
	defer server.Close()

	// Create provider with test server URL
	cfg := provider.NewConfig("test-key", "test-model").
		WithBaseURL(server.URL)
	p := New(cfg)

	// Create test request
	req := &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// Test streaming
	respChan, errChan := p.InvokeStream(context.Background(), req)
	require.NotNil(t, respChan)
	require.NotNil(t, errChan)

	// Collect all chunks
	var receivedChunks []string
	var lastUsage *pb.UsageInfo

	for resp := range respChan {
		require.NotNil(t, resp)
		switch resp.Type {
		case pb.ResponseType_TYPE_CONTENT:
			receivedChunks = append(receivedChunks, resp.Content)
		case pb.ResponseType_TYPE_USAGE:
			lastUsage = resp.Usage
		}
	}

	// Check for errors
	select {
	case err := <-errChan:
		require.NoError(t, err)
	default:
		// No error is good
	}

	// Verify received chunks
	require.Equal(t, chunks, receivedChunks)
	require.NotNil(t, lastUsage)
	require.Equal(t, int32(5), lastUsage.PromptTokens)
	require.Equal(t, int32(10), lastUsage.CompletionTokens)
	require.Equal(t, int32(15), lastUsage.TotalTokens)
}

func TestInvokeStreamError(t *testing.T) {
	// Create a test server that sends an error event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		// Send error event
		resp := streamResponseBody{
			Type: "error",
			Content: []contentBlock{
				{
					Type: "text",
					Text: "Rate limit exceeded",
				},
			},
		}
		data, err := json.Marshal(resp)
		require.NoError(t, err)

		_, err = fmt.Fprintf(w, "data: %s\n\n", data)
		require.NoError(t, err)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	// Create provider with test server URL
	cfg := provider.NewConfig("test-key", "test-model").
		WithBaseURL(server.URL)
	p := New(cfg)

	// Create test request
	req := &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	// Test streaming error
	respChan, errChan := p.InvokeStream(context.Background(), req)
	require.NotNil(t, respChan)
	require.NotNil(t, errChan)

	// Should receive error
	err := <-errChan
	require.Error(t, err)
	require.Contains(t, err.Error(), "Rate limit exceeded")

	// Response channel should be closed
	_, ok := <-respChan
	require.False(t, ok)
}
