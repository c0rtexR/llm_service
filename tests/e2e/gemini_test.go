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

	"github.com/c0rtexR/llm_service/internal/provider"
	"github.com/c0rtexR/llm_service/internal/provider/gemini"
	"github.com/c0rtexR/llm_service/internal/server"
	pb "github.com/c0rtexR/llm_service/proto"
)

type geminiTestServer struct {
	server     *grpc.Server
	client     pb.LLMServiceClient
	provider   provider.LLMProvider
	grpcServer *grpc.Server
	cleanup    func()
}

func setupGeminiTestServer(t *testing.T) *geminiTestServer {
	// Check for Gemini API key
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Initialize Gemini provider
	p, err := gemini.New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "gemini-1.5-flash-8b",
	})
	require.NoError(t, err)

	providers := map[string]provider.LLMProvider{
		"gemini": p,
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

	return &geminiTestServer{
		server:     grpcServer,
		client:     pb.NewLLMServiceClient(conn),
		provider:   p,
		grpcServer: grpcServer,
		cleanup:    cleanup,
	}
}

func TestGeminiBasicCall(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test basic request
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "gemini",
		Model:    "gemini-1.5-flash-8b",
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

func TestGeminiStreamingCall(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Start streaming request
	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "gemini",
		Model:    "gemini-1.5-flash-8b",
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write a short story",
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
			gotFinish = true
		}
	}

	require.True(t, gotContent, "should have received content")
	require.True(t, gotFinish, "should have received finish reason")
	require.NotEmpty(t, chunks)
}

func TestGeminiChatHistory(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test chat history handling
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "gemini",
		Model:    "gemini-1.5-flash-8b",
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

func TestGeminiParallelStreaming(t *testing.T) {
	ts := setupGeminiTestServer(t)
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
				Provider: "gemini",
				Model:    "gemini-1.5-flash-8b",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: fmt.Sprintf("Write story number %d", idx+1),
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

func TestGeminiLargePrompt(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Create a large prompt (~100KB)
	largePrompt := strings.Repeat("This is a test prompt. ", 5000)

	stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
		Provider: "gemini",
		Model:    "gemini-1.5-flash-8b",
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

func TestGeminiModelParameters(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test with different model parameters
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider:    "gemini",
		Model:       "gemini-1.5-flash-8b",
		Temperature: 1.0,
		TopP:        0.9,
		TopK:        40,
		MaxTokens:   100,
		Messages: []*pb.ChatMessage{
			{
				Role:    "user",
				Content: "Write something creative",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
}

func TestGeminiInvalidModel(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test with invalid model name
	resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
		Provider: "gemini",
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
