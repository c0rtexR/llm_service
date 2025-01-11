package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"llmservice/internal/provider"
	"llmservice/internal/provider/anthropic"
	"llmservice/internal/provider/gemini"
	"llmservice/internal/provider/openai"
	"llmservice/internal/provider/openrouter"
	"llmservice/internal/server"
	pb "llmservice/proto"
)

type testServer struct {
	server     *grpc.Server
	client     pb.LLMServiceClient
	providers  map[string]provider.LLMProvider
	grpcServer *grpc.Server
	cleanup    func()
}

func setupTestServer(t *testing.T) *testServer {
	// Initialize providers
	providers := make(map[string]provider.LLMProvider)

	// OpenRouter provider
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" && !strings.HasPrefix(key, "sk-test") {
		p := openrouter.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "openai/gpt-3.5-turbo",
		})
		providers["openrouter"] = p
	}

	// OpenAI provider
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && !strings.HasPrefix(key, "sk-test") {
		p := openai.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "gpt-3.5-turbo",
		})
		providers["openai"] = p
	}

	// Anthropic provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && !strings.HasPrefix(key, "sk-test") {
		p := anthropic.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "claude-2",
		})
		providers["anthropic"] = p
	}

	// Gemini provider
	if key := os.Getenv("GEMINI_API_KEY"); key != "" && !strings.HasPrefix(key, "sk-test") {
		p, err := gemini.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "gemini-1.5-flash-8b",
		})
		if err == nil {
			providers["gemini"] = p
		}
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()

	// Register LLM service
	llmServer := server.New(providers)
	pb.RegisterLLMServiceServer(grpcServer, llmServer)

	// Enable reflection for development tools
	reflection.Register(grpcServer)

	// Create a listener on a random port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	// Connect to the server
	conn, err := grpc.Dial(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	cleanup := func() {
		conn.Close()
		grpcServer.GracefulStop()
	}

	return &testServer{
		server:     grpcServer,
		client:     pb.NewLLMServiceClient(conn),
		providers:  providers,
		grpcServer: grpcServer,
		cleanup:    cleanup,
	}
}

func TestBasicSingleCall(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["openrouter"]; !ok {
		t.Skip("OpenRouter provider not available")
	}

	// Test basic request
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["openai"]; !ok {
		t.Skip("OpenAI provider not available")
	}

	// Start streaming request
	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["anthropic"]; !ok {
		t.Skip("Anthropic provider not available")
	}

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
	resp1, err := ts.client.Invoke(context.Background(), req)
	duration1 := time.Since(start1)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.NotEmpty(t, resp1.Content)
	require.Contains(t, resp1.Content, "4")

	// Second request (should hit cache)
	start2 := time.Now()
	resp2, err := ts.client.Invoke(context.Background(), req)
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["anthropic"]; !ok {
		t.Skip("Anthropic provider not available")
	}

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
	resp1, err := ts.client.Invoke(context.Background(), req)
	duration1 := time.Since(start1)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.NotEmpty(t, resp1.Content)
	require.Contains(t, strings.ToLower(resp1.Content), "blue")

	// Second request (should hit cache)
	start2 := time.Now()
	resp2, err := ts.client.Invoke(context.Background(), req)
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["openai"]; !ok {
		t.Skip("OpenAI provider not available")
	}

	const numStreams = 5

	var wg sync.WaitGroup
	responses := make([][]string, numStreams)
	errors := make(chan error, numStreams)

	// Launch parallel streaming requests
	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Test invalid provider
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["openai"]; !ok {
		t.Skip("OpenAI provider not available")
	}

	// Create a large prompt (~1MB)
	largePrompt := strings.Repeat("This is a test prompt. ", 50000)

	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
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
	ts := setupTestServer(t)
	defer ts.cleanup()

	if _, ok := ts.providers["openrouter"]; !ok {
		t.Skip("OpenRouter provider not available")
	}

	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
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
