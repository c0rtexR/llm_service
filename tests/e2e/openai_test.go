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
	"llmservice/internal/provider/openai"
	"llmservice/internal/server"
	pb "llmservice/proto"
)

type openaiTestServer struct {
	server     *grpc.Server
	client     pb.LLMServiceClient
	provider   provider.LLMProvider
	grpcServer *grpc.Server
	cleanup    func()
}

func setupOpenAITestServer(t *testing.T) *openaiTestServer {
	// Check for OpenAI API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	// Initialize OpenAI provider
	p := openai.New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "gpt-4o-mini",
	})

	providers := map[string]provider.LLMProvider{
		"openai": p,
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

	return &openaiTestServer{
		server:     grpcServer,
		client:     pb.NewLLMServiceClient(conn),
		provider:   p,
		grpcServer: grpcServer,
		cleanup:    cleanup,
	}
}

func TestOpenAIBasicCall(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Test basic request
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Say something",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
}

func TestOpenAIStreamingCall(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Start streaming request
	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write a short story",
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
}

func TestOpenAIChatHistory(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Test chat history handling
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "First message",
			},
			{
				Role:    "assistant",
				Content: "First response",
			},
			{
				Role:    "user",
				Content: "Second message",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
}

func TestOpenAIParallelStreaming(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	const numStreams = 3

	var wg sync.WaitGroup
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
						Content: fmt.Sprintf("Stream %d", idx+1),
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
		// Check for any errors
		close(errors)
		for err := range errors {
			t.Error(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for parallel streams")
	}
}

func TestOpenAILargePrompt(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Create a large prompt (~100KB)
	largePrompt := strings.Repeat("This is a test prompt. ", 5000)

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
	var gotError bool
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Accept any error for large prompts
			gotError = true
			break
		}

		if resp.Type == pb.ResponseType_TYPE_CONTENT {
			gotContent = true
		}
	}

	require.True(t, gotContent || gotError, "should have either received content or an error")
}

func TestOpenAIModelParameters(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Test with different model parameters
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider:    "openai",
		Model:       "gpt-4o-mini",
		Temperature: 1.0,
		TopP:        0.9,
		MaxTokens:   100,
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write something",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
}

func TestOpenAIInvalidModel(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	// Test with invalid model name
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "invalid-model",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	})

	require.Error(t, err)
	require.Nil(t, resp)
}

func TestOpenAIUsageInfo(t *testing.T) {
	ts := setupOpenAITestServer(t)
	defer ts.cleanup()

	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Test message",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Usage, "should have usage information")
	require.Greater(t, resp.Usage.PromptTokens, int32(0), "should have prompt tokens")
	require.Greater(t, resp.Usage.CompletionTokens, int32(0), "should have completion tokens")
	require.Greater(t, resp.Usage.TotalTokens, int32(0), "should have total tokens")
}
