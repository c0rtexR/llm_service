package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "llmservice/proto"
)

func setupClient(t *testing.T) pb.LLMServiceClient {
	// Connect to the gRPC server
	conn, err := grpc.Dial("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return pb.NewLLMServiceClient(conn)
}

func TestBasicSingleCall(t *testing.T) {
	// Skip if no OpenRouter API key
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	client := setupClient(t)

	// Test basic request
	resp, err := client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Say hello",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	require.Contains(t, strings.ToLower(resp.Content), "hello")
}

func TestSimpleStreamedCall(t *testing.T) {
	// Skip if no OpenAI API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	client := setupClient(t)

	// Start streaming request
	stream, err := client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Count from 1 to 5 slowly",
			},
		},
	})
	require.NoError(t, err)

	var chunks []string
	var gotContent, gotFinish bool

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
		case pb.ResponseType_TYPE_FINISH_REASON:
			require.Equal(t, "stop", resp.FinishReason)
			gotFinish = true
		}
	}

	require.True(t, gotContent, "should have received content")
	require.True(t, gotFinish, "should have received finish reason")
	require.NotEmpty(t, chunks)

	// Join all chunks and verify content
	fullResponse := strings.Join(chunks, "")
	for i := 1; i <= 5; i++ {
		require.Contains(t, fullResponse, fmt.Sprintf("%d", i))
	}
}

func TestAnthropicCachingSingleBlock(t *testing.T) {
	// Skip if no Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	client := setupClient(t)

	// Create a request with ephemeral caching
	req := &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
		Messages: []*pb.ChatMessage{
			{
				Role:    "system",
				Content: strings.Repeat("You are a helpful assistant that provides concise responses. ", 100), // Large system message
			},
			{
				Role:    "user",
				Content: "What is 2+2?",
			},
		},
		CacheControl: &pb.CacheControl{
			UseCache: true,
			Ttl:      300, // 5 minutes
		},
	}

	// First request
	start1 := time.Now()
	resp1, err := client.Invoke(context.Background(), req)
	duration1 := time.Since(start1)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.NotEmpty(t, resp1.Content)
	require.Contains(t, resp1.Content, "4")

	// Second request (should hit cache)
	start2 := time.Now()
	resp2, err := client.Invoke(context.Background(), req)
	duration2 := time.Since(start2)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.NotEmpty(t, resp2.Content)

	// Responses should be identical since it's cached
	require.Equal(t, resp1.Content, resp2.Content)

	// Second request should be faster due to cache hit
	require.Less(t, duration2, duration1)

	// Check usage info (if available)
	if resp2.Usage != nil && resp1.Usage != nil {
		require.Less(t, resp2.Usage.PromptTokens, resp1.Usage.PromptTokens)
	}
}

func TestAnthropicCachingMultipleBlocks(t *testing.T) {
	// Skip if no Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	client := setupClient(t)

	// Create a request with multiple ephemeral blocks
	req := &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
		Messages: []*pb.ChatMessage{
			{
				Role:    "system",
				Content: strings.Repeat("You are a helpful assistant. ", 50),
			},
			{
				Role:    "user",
				Content: strings.Repeat("Consider the following context: The sky is blue. ", 50),
			},
			{
				Role:    "user",
				Content: "What color is the sky?",
			},
		},
		CacheControl: &pb.CacheControl{
			UseCache: true,
			Ttl:      300, // 5 minutes
		},
	}

	// First request
	start1 := time.Now()
	resp1, err := client.Invoke(context.Background(), req)
	duration1 := time.Since(start1)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.NotEmpty(t, resp1.Content)
	require.Contains(t, strings.ToLower(resp1.Content), "blue")

	// Second request (should hit cache)
	start2 := time.Now()
	resp2, err := client.Invoke(context.Background(), req)
	duration2 := time.Since(start2)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.NotEmpty(t, resp2.Content)

	// Responses should be identical since it's cached
	require.Equal(t, resp1.Content, resp2.Content)

	// Second request should be faster due to cache hit
	require.Less(t, duration2, duration1)
}

func TestParallelStreaming(t *testing.T) {
	// Skip if no OpenAI API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	client := setupClient(t)
	const numStreams = 5

	var wg sync.WaitGroup
	responses := make([][]string, numStreams)
	errors := make(chan error, numStreams)

	// Launch parallel streaming requests
	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			stream, err := client.InvokeStream(context.Background(), &pb.LLMRequest{
				Provider: "openai",
				Model:    "gpt-4o-mini",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: fmt.Sprintf("Count from 1 to %d", idx+1),
					},
				},
			})
			if err != nil {
				errors <- fmt.Errorf("stream %d setup failed: %w", idx, err)
				return
			}

			var chunks []string
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
					chunks = append(chunks, resp.Content)
				}
			}

			responses[idx] = chunks
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
		// Check for any errors
		close(errors)
		for err := range errors {
			t.Error(err)
		}

		// Verify each response
		for i, chunks := range responses {
			require.NotEmpty(t, chunks)
			fullResponse := strings.Join(chunks, "")
			for j := 1; j <= i+1; j++ {
				require.Contains(t, fullResponse, fmt.Sprintf("%d", j))
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for parallel streams")
	}
}

func TestInvalidProvider(t *testing.T) {
	client := setupClient(t)

	// Test invalid provider
	resp, err := client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "invalid-provider",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	})

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "provider")
}

func TestLargePrompt(t *testing.T) {
	// Skip if no OpenAI API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	client := setupClient(t)

	// Create a large prompt (~1MB)
	largePrompt := strings.Repeat("This is a test prompt. ", 50000)

	stream, err := client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: largePrompt,
			},
		},
	})
	require.NoError(t, err)

	var gotContent bool
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// OpenAI might reject the request due to token limit
			require.Contains(t, err.Error(), "token")
			return
		}

		if resp.Type == pb.ResponseType_TYPE_CONTENT {
			gotContent = true
		}
	}

	require.True(t, gotContent, "should have received some content")
}

func TestMissingAPIKey(t *testing.T) {
	client := setupClient(t)

	resp, err := client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "unsupported-provider",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	})

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, strings.ToLower(err.Error()), "unsupported provider")
}

func TestOpenRouterStreaming(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	client := setupClient(t)
	stream, err := client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "openrouter",
		Model:    "google/gemini-flash-1.5-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Count from 1 to 5 slowly.",
			},
		},
	})
	require.NoError(t, err)

	var fullResponse string
	var finishReason string
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if resp.FinishReason != "" {
			finishReason = resp.FinishReason
		}
		fullResponse += resp.Content
	}

	require.Equal(t, "STOP", finishReason)

	// Check that the response contains either numeric or text numbers
	require.True(t,
		strings.Contains(fullResponse, "1") || strings.Contains(fullResponse, "one") ||
			strings.Contains(fullResponse, "One"),
		"Response should contain '1' or 'one'")
	require.True(t,
		strings.Contains(fullResponse, "2") || strings.Contains(fullResponse, "two") ||
			strings.Contains(fullResponse, "Two"),
		"Response should contain '2' or 'two'")
	require.True(t,
		strings.Contains(fullResponse, "3") || strings.Contains(fullResponse, "three") ||
			strings.Contains(fullResponse, "Three"),
		"Response should contain '3' or 'three'")
	require.True(t,
		strings.Contains(fullResponse, "4") || strings.Contains(fullResponse, "four") ||
			strings.Contains(fullResponse, "Four"),
		"Response should contain '4' or 'four'")
	require.True(t,
		strings.Contains(fullResponse, "5") || strings.Contains(fullResponse, "five") ||
			strings.Contains(fullResponse, "Five"),
		"Response should contain '5' or 'five'")
}
