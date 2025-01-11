package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"llmservice/internal/provider"
	"llmservice/internal/provider/anthropic"
	"llmservice/internal/provider/openai"
	"llmservice/internal/provider/openrouter"
	"llmservice/internal/server"
	pb "llmservice/proto"
)

const (
	bufSize = 1024 * 1024
)

var (
	lis    *bufconn.Listener
	client pb.LLMServiceClient
)

func init() {
	lis = bufconn.Listen(bufSize)
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

func setupTestServer(t *testing.T) {
	// Create gRPC server
	grpcServer := grpc.NewServer()

	// Initialize providers with real API keys if available
	providers := make(map[string]provider.LLMProvider)

	// OpenRouter provider
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		p := openrouter.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "openai/gpt-3.5-turbo",
		})
		providers["openrouter"] = p
	}

	// OpenAI provider
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p := openai.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "gpt-3.5-turbo",
		})
		providers["openai"] = p
	}

	// Anthropic provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p := anthropic.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "claude-2",
		})
		providers["anthropic"] = p
	}

	// Register LLM service
	llmServer := server.New(providers)
	pb.RegisterLLMServiceServer(grpcServer, llmServer)

	// Start server
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Errorf("failed to serve: %v", err)
		}
	}()

	// Set up client connection
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client = pb.NewLLMServiceClient(conn)
}

func TestInvoke(t *testing.T) {
	setupTestServer(t)

	tests := []struct {
		name        string
		provider    string
		messages    []*pb.ChatMessage
		expectError bool
	}{
		{
			name:     "openrouter basic request",
			provider: "openrouter",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Say hello",
				},
			},
			expectError: os.Getenv("OPENROUTER_API_KEY") == "",
		},
		{
			name:     "openai basic request",
			provider: "openai",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Say hello",
				},
			},
			expectError: os.Getenv("OPENAI_API_KEY") == "",
		},
		{
			name:     "anthropic basic request",
			provider: "anthropic",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Say hello",
				},
			},
			expectError: os.Getenv("ANTHROPIC_API_KEY") == "",
		},
		{
			name:     "invalid provider",
			provider: "invalid",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Say hello",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Invoke(context.Background(), &pb.LLMRequest{
				Provider: tt.provider,
				Messages: tt.messages,
			})

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.Content)
			}
		})
	}
}

func TestInvokeStream(t *testing.T) {
	setupTestServer(t)

	tests := []struct {
		name        string
		provider    string
		messages    []*pb.ChatMessage
		expectError bool
	}{
		{
			name:     "openai stream request",
			provider: "openai",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Count from 1 to 5",
				},
			},
			expectError: os.Getenv("OPENAI_API_KEY") == "",
		},
		{
			name:     "anthropic stream request",
			provider: "anthropic",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Count from 1 to 5",
				},
			},
			expectError: os.Getenv("ANTHROPIC_API_KEY") == "",
		},
		{
			name:     "invalid provider",
			provider: "invalid",
			messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Count from 1 to 5",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream, err := client.InvokeStream(context.Background(), &pb.LLMRequest{
				Provider: tt.provider,
				Messages: tt.messages,
			})
			require.NoError(t, err)

			var gotContent bool
			var gotFinish bool

			for {
				resp, err := stream.Recv()
				if err != nil {
					if tt.expectError {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					break
				}

				switch resp.Type {
				case pb.ResponseType_TYPE_CONTENT:
					require.NotEmpty(t, resp.Content)
					gotContent = true
				case pb.ResponseType_TYPE_FINISH_REASON:
					require.Equal(t, "stop", resp.FinishReason)
					gotFinish = true
				}
			}

			if !tt.expectError {
				require.True(t, gotContent, "should have received content")
				require.True(t, gotFinish, "should have received finish reason")
			}
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	setupTestServer(t)

	// Skip if no API keys available
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	const numRequests = 5
	var wg sync.WaitGroup
	errChan := make(chan error, numRequests)

	// Launch concurrent requests
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			resp, err := client.Invoke(context.Background(), &pb.LLMRequest{
				Provider: "openai",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: fmt.Sprintf("Count to %d", i+1),
					},
				},
			})

			if err != nil {
				errChan <- fmt.Errorf("request %d failed: %w", i, err)
				return
			}

			if resp == nil || resp.Content == "" {
				errChan <- fmt.Errorf("request %d got empty response", i)
				return
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
		// Check for any errors
		close(errChan)
		for err := range errChan {
			t.Error(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for concurrent requests")
	}
}

func TestAnthropicCaching(t *testing.T) {
	setupTestServer(t)

	// Skip if no Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create a request with ephemeral caching
	req := &pb.LLMRequest{
		Provider: "anthropic",
		Messages: []*pb.ChatMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant that provides concise responses.",
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
	resp1, err := client.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.NotEmpty(t, resp1.Content)

	// Second request (should hit cache)
	resp2, err := client.Invoke(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.NotEmpty(t, resp2.Content)

	// Responses should be identical since it's cached
	require.Equal(t, resp1.Content, resp2.Content)

	// Check usage info (if available)
	if resp2.Usage != nil && resp1.Usage != nil {
		// Second request should have lower prompt tokens due to cache hit
		require.Less(t, resp2.Usage.PromptTokens, resp1.Usage.PromptTokens)
	}
}
