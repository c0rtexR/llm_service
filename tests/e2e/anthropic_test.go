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
	"llmservice/internal/server"
	pb "llmservice/proto"
)

type anthropicTestServer struct {
	server     *grpc.Server
	client     pb.LLMServiceClient
	provider   provider.LLMProvider
	grpcServer *grpc.Server
	cleanup    func()
}

func setupAnthropicTestServer(t *testing.T) *anthropicTestServer {
	// Check for Anthropic API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Initialize Anthropic provider
	p := anthropic.New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "claude-3-5-haiku-latest",
	})

	providers := map[string]provider.LLMProvider{
		"anthropic": p,
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

	return &anthropicTestServer{
		server:     grpcServer,
		client:     pb.NewLLMServiceClient(conn),
		provider:   p,
		grpcServer: grpcServer,
		cleanup:    cleanup,
	}
}

func TestAnthropicBasicCall(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Test basic request
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
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
	require.Contains(t, resp.Content, "4")
	require.NotNil(t, resp.Usage)
	require.Greater(t, resp.Usage.PromptTokens, int32(0))
	require.Greater(t, resp.Usage.CompletionTokens, int32(0))
}

func TestAnthropicStreamingCall(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Start streaming request
	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Count from 1 to 5.",
			},
		},
	})
	require.NoError(t, err)

	var chunks []string
	var gotContent, gotUsage bool

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
		case pb.ResponseType_TYPE_USAGE:
			gotUsage = true
			require.NotNil(t, resp.Usage)
			require.Greater(t, resp.Usage.PromptTokens, int32(0))
			require.Greater(t, resp.Usage.CompletionTokens, int32(0))
		}
	}

	require.True(t, gotContent, "should have received content")
	require.True(t, gotUsage, "should have received usage info")
	require.NotEmpty(t, chunks)

	// Join chunks and verify numbers are present
	fullResponse := strings.Join(chunks, "")
	for i := 1; i <= 5; i++ {
		require.Contains(t, fullResponse, fmt.Sprintf("%d", i))
	}
}

func TestAnthropicChatHistory(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Test chat history handling
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
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

func TestAnthropicSystemMessage(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Test with system message
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "anthropic",
		Model:    "claude-3-5-haiku-latest",
		Messages: []*pb.ChatMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant that always responds with exactly one word.",
			},
			{
				Role:    "user",
				Content: "What is the capital of France?",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	require.Contains(t, strings.ToLower(resp.Content), "paris")
}

func TestAnthropicParallelStreaming(t *testing.T) {
	ts := setupAnthropicTestServer(t)
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
				Provider: "anthropic",
				Model:    "claude-3-5-haiku-latest",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: fmt.Sprintf("Write a one-sentence story about number %d.", idx+1),
					},
				},
			})
			if err != nil {
				errors <- fmt.Errorf("stream %d setup failed: %w", idx, err)
				return
			}

			var gotContent, gotUsage bool
			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					errors <- fmt.Errorf("stream %d receive failed: %w", idx, err)
					return
				}

				switch resp.Type {
				case pb.ResponseType_TYPE_CONTENT:
					gotContent = true
				case pb.ResponseType_TYPE_USAGE:
					gotUsage = true
				}
			}

			if !gotContent {
				errors <- fmt.Errorf("stream %d did not receive any content", idx)
			}
			if !gotUsage {
				errors <- fmt.Errorf("stream %d did not receive usage info", idx)
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

func TestAnthropicModelParameters(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Test with different model parameters
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider:    "anthropic",
		Model:       "claude-3-5-haiku-latest",
		Temperature: 1.0,
		TopP:        0.9,
		MaxTokens:   100,
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Generate a random number between 1 and 10.",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
}

func TestAnthropicInvalidModel(t *testing.T) {
	ts := setupAnthropicTestServer(t)
	defer ts.cleanup()

	// Test with invalid model name
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "anthropic",
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
