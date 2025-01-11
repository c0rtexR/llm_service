package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"llmservice/internal/provider"
	pb "llmservice/proto"
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
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/messages", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "test-key", r.Header.Get("X-API-Key"))
		require.Equal(t, apiVersion, r.Header.Get("anthropic-version"))

		// Parse request body
		var reqBody requestBody
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request contents
		require.Equal(t, "test-model", reqBody.Model)
		require.Equal(t, "This is a system message", reqBody.System)
		require.Len(t, reqBody.Messages, 1)
		require.Equal(t, "user", reqBody.Messages[0].Role)
		require.Equal(t, "Hello", reqBody.Messages[0].Content)
		require.NotNil(t, reqBody.Messages[0].CacheControl)
		require.Equal(t, "ephemeral", reqBody.Messages[0].CacheControl.Type)
		require.NotNil(t, reqBody.Temperature)
		require.Equal(t, float32(0.7), *reqBody.Temperature)

		// Send response
		resp := responseBody{
			ID:      "test-id",
			Model:   "test-model",
			Type:    "message",
			Role:    "assistant",
			Content: "Hello, how can I help?",
			Usage: struct {
				InputTokens              int32 `json:"input_tokens"`
				OutputTokens             int32 `json:"output_tokens"`
				CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
				CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
			}{
				InputTokens:              5,
				OutputTokens:             10,
				CacheCreationInputTokens: 5,
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
				Role:    "system",
				Content: "This is a system message",
			},
			{
				Role:    "user",
				Content: "Hello",
				CacheControl: &pb.CacheControl{
					Type: "ephemeral",
				},
			},
		},
		Temperature: 0.7,
	}

	// Test successful request
	resp, err := p.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "Hello, how can I help?", resp.Content)
	require.Equal(t, int32(5), resp.Usage.PromptTokens)
	require.Equal(t, int32(10), resp.Usage.CompletionTokens)
	require.Equal(t, int32(15), resp.Usage.TotalTokens)
	require.Equal(t, int32(5), resp.Usage.CacheCreationInputTokens)
}

func TestInvokeWithCacheHit(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send response with cache hit
		resp := responseBody{
			ID:      "test-id",
			Model:   "test-model",
			Type:    "message",
			Role:    "assistant",
			Content: "Hello, how can I help?",
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
				CacheControl: &pb.CacheControl{
					Type: "ephemeral",
				},
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
	require.Equal(t, int32(5), resp.Usage.CacheReadInputTokens)
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
	// Create provider
	cfg := provider.NewConfig("test-key", "test-model")
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
		EnableStream: true,
	}

	// Test that streaming is not yet implemented
	respChan, errChan := p.InvokeStream(context.Background(), req)
	require.NotNil(t, respChan)
	require.NotNil(t, errChan)

	// Should receive "not implemented" error
	err := <-errChan
	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet implemented")

	// Response channel should be closed
	_, ok := <-respChan
	require.False(t, ok)
}
