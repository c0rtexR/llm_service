package e2e

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"llmservice/internal/provider"
	"llmservice/internal/provider/openrouter"
	"llmservice/internal/server"
	pb "llmservice/proto"
)

type openrouterTestServer struct {
	server   *grpc.Server
	client   pb.LLMServiceClient
	provider provider.LLMProvider
	cleanup  func()
}

func setupOpenRouterTestServer(t *testing.T) *openrouterTestServer {
	// Check for OpenRouter API key
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		apiKey = "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" // Default test key
	}

	// Initialize OpenRouter provider
	p := openrouter.New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "google/gemini-flash-1.5-8b",
	})

	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.InitialWindowSize(1<<20),     // 1MB window size
		grpc.InitialConnWindowSize(1<<20), // 1MB connection window
		grpc.MaxConcurrentStreams(100),    // Allow more concurrent streams
		grpc.WriteBufferSize(64*1024),     // 64KB buffer
		grpc.ReadBufferSize(64*1024),      // 64KB buffer
		grpc.MaxRecvMsgSize(4*1024*1024),  // 4MB max message size
		grpc.MaxSendMsgSize(4*1024*1024),  // 4MB max message size
	)
	llmServer := server.New(map[string]provider.LLMProvider{
		"openrouter": p,
	})
	pb.RegisterLLMServiceServer(grpcServer, llmServer)
	reflection.Register(grpcServer)

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	// Start server
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			fmt.Printf("Failed to serve: %v\n", err)
		}
	}()

	// Create client connection
	conn, err := grpc.Dial(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(1<<20),     // 1MB window size
		grpc.WithInitialConnWindowSize(1<<20), // 1MB connection window
		grpc.WithWriteBufferSize(64*1024),     // 64KB buffer
		grpc.WithReadBufferSize(64*1024),      // 64KB buffer
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB max message size
			grpc.MaxCallSendMsgSize(4*1024*1024), // 4MB max message size
		),
	)
	require.NoError(t, err)

	client := pb.NewLLMServiceClient(conn)

	return &openrouterTestServer{
		server:   grpcServer,
		client:   client,
		provider: p,
		cleanup: func() {
			conn.Close()
			grpcServer.GracefulStop()
		},
	}
}
