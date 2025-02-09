package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c0rtexR/llm_service/internal/provider"
	pb "github.com/c0rtexR/llm_service/proto"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	config := provider.NewConfig("test-key", defaultModel)
	p := New(config)

	require.NotNil(t, p)
	require.Equal(t, defaultBaseURL, p.config.BaseURL)
	require.Equal(t, defaultModel, p.config.DefaultModel)
	require.Equal(t, "test-key", p.config.APIKey)
}

func TestInvoke(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// Parse request body
		var reqBody requestBody
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request body
		require.Equal(t, "test-model", reqBody.Model)
		require.Len(t, reqBody.Messages, 1)
		require.Equal(t, "user", reqBody.Messages[0].Role)
		require.Equal(t, "test message", reqBody.Messages[0].Content)

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(responseBody{
			ID:    "test-id",
			Model: "test-model",
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: "test response",
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int32 `json:"prompt_tokens"`
				CompletionTokens int32 `json:"completion_tokens"`
				TotalTokens      int32 `json:"total_tokens"`
			}{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		})
	}))
	defer server.Close()

	// Create provider with test server URL
	config := provider.NewConfig("test-key", defaultModel).WithBaseURL(server.URL)
	p := New(config)

	// Test successful request
	resp, err := p.Invoke(context.Background(), &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "test message",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "test response", resp.Content)
	require.NotNil(t, resp.Usage)
	require.Equal(t, int32(10), resp.Usage.PromptTokens)
	require.Equal(t, int32(20), resp.Usage.CompletionTokens)
	require.Equal(t, int32(30), resp.Usage.TotalTokens)
}

func TestInvokeError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("test error"))
	}))
	defer server.Close()

	// Create provider with test server URL
	config := provider.NewConfig("test-key", defaultModel).WithBaseURL(server.URL)
	p := New(config)

	// Test error response
	resp, err := p.Invoke(context.Background(), &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "test message",
			},
		},
	})

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "request failed with status 400")
}

func TestInvokeStream(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))

		// Parse request body
		var reqBody requestBody
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request body
		require.Equal(t, "test-model", reqBody.Model)
		require.True(t, reqBody.Stream)
		require.Len(t, reqBody.Messages, 1)
		require.Equal(t, "user", reqBody.Messages[0].Role)
		require.Equal(t, "test message", reqBody.Messages[0].Content)

		// Set response headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Write response chunks
		chunks := []string{
			`{"id":"1","model":"test-model","choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
			`{"id":"2","model":"test-model","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"3","model":"test-model","choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}`,
			`{"id":"4","model":"test-model","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`,
		}

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		for _, chunk := range chunks {
			_, err := fmt.Fprintf(w, "data: %s\n\n", chunk)
			require.NoError(t, err)
			flusher.Flush()
		}

		_, err = fmt.Fprintf(w, "data: [DONE]\n\n")
		require.NoError(t, err)
		flusher.Flush()
	}))
	defer server.Close()

	// Create provider with test server URL
	config := provider.NewConfig("test-key", defaultModel).WithBaseURL(server.URL)
	p := New(config)

	// Test successful request
	respChan, errChan := p.InvokeStream(context.Background(), &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "test message",
			},
		},
	})

	// Collect responses
	var responses []*pb.LLMStreamResponse
	for resp := range respChan {
		responses = append(responses, resp)
	}

	// Check for errors
	err := <-errChan
	require.NoError(t, err)

	// Verify responses
	require.Len(t, responses, 5)

	// First chunk should be "Hello"
	require.Equal(t, pb.ResponseType_TYPE_CONTENT, responses[0].Type)
	require.Equal(t, "Hello", responses[0].Content)

	// Second chunk should be " world"
	require.Equal(t, pb.ResponseType_TYPE_CONTENT, responses[1].Type)
	require.Equal(t, " world", responses[1].Content)

	// Third chunk should be "!"
	require.Equal(t, pb.ResponseType_TYPE_CONTENT, responses[2].Type)
	require.Equal(t, "!", responses[2].Content)

	// Fourth chunk should be finish reason
	require.Equal(t, pb.ResponseType_TYPE_FINISH_REASON, responses[3].Type)
	require.Equal(t, "stop", responses[3].FinishReason)

	// Fifth chunk should be usage info
	require.Equal(t, pb.ResponseType_TYPE_USAGE, responses[4].Type)
	require.NotNil(t, responses[4].Usage)
	require.Equal(t, int32(10), responses[4].Usage.PromptTokens)
	require.Equal(t, int32(20), responses[4].Usage.CompletionTokens)
	require.Equal(t, int32(30), responses[4].Usage.TotalTokens)
}

func TestInvokeStreamError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("test error"))
	}))
	defer server.Close()

	// Create provider with test server URL
	config := provider.NewConfig("test-key", defaultModel).WithBaseURL(server.URL)
	p := New(config)

	// Test error response
	respChan, errChan := p.InvokeStream(context.Background(), &pb.LLMRequest{
		Model: "test-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "test message",
			},
		},
	})

	// Should receive error
	err := <-errChan
	require.Error(t, err)
	require.Contains(t, err.Error(), "request failed with status 400")

	// Response channel should be closed
	_, ok := <-respChan
	require.False(t, ok)
}
