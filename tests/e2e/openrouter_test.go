package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/c0rtexR/llm_service/proto"
)

func TestOpenRouterBasicCall(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test basic request
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "What is 2+2?",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	require.Contains(t, strings.ToLower(resp.Content), "4")
	require.NotNil(t, resp.Usage)
	require.Greater(t, resp.Usage.PromptTokens, int32(0))
	require.Greater(t, resp.Usage.CompletionTokens, int32(0))
}

func TestOpenRouterStreamingCall(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Start streaming request
	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write a haiku about coding.",
			},
		},
	})
	require.NoError(t, err)

	var chunks []string
	var gotContent bool

	// Collect all chunks
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		switch resp.Type {
		case pb.ResponseType_TYPE_CONTENT:
			chunks = append(chunks, resp.Content)
			gotContent = true
		}
	}

	require.True(t, gotContent, "should have received content")
	require.NotEmpty(t, chunks)

	// Join chunks and verify it's not empty
	fullResponse := strings.Join(chunks, "")
	require.NotEmpty(t, fullResponse)
}

func TestOpenRouterChatHistory(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test chat history handling
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "My name is Alice.",
			},
			{
				Role:    "assistant",
				Content: "Hello Alice! How can I help you today?",
			},
			{
				Role:    "user",
				Content: "What's my name?",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	require.Contains(t, resp.Content, "Alice")
}

func TestOpenRouterParallelStreaming(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	const numStreams = 2 // Reduced from 3 to avoid rate limits

	var wg sync.WaitGroup
	errors := make(chan error, numStreams)

	// Launch parallel streaming requests
	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
				Provider: "openrouter",
				Model:    "google/gemini-flash-1.5-8b",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: fmt.Sprintf("Write a one-line story about number %d", idx+1),
					},
				},
			})
			if err != nil {
				errors <- fmt.Errorf("stream %d setup failed: %w", idx, err)
				return
			}

			var gotContent bool
			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					errors <- fmt.Errorf("stream %d receive failed: %w", idx, err)
					return
				}

				if resp.Type == pb.ResponseType_TYPE_CONTENT {
					gotContent = true
				}
			}

			if !gotContent {
				errors <- fmt.Errorf("stream %d did not receive any content", idx)
			}
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("parallel streaming test timed out")
	}

	// Check for errors
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestOpenRouterModelParameters(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test with different model parameters
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider:    "openrouter",
		Model:       "google/gemini-flash-1.5-8b",
		Temperature: 0.7,
		MaxTokens:   50,
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write a creative one-line story.",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	require.NotNil(t, resp.Usage)
	require.Greater(t, resp.Usage.TotalTokens, int32(0))
}

func TestOpenRouterInvalidModel(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test with invalid model name
	_, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "invalid-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid")
}
