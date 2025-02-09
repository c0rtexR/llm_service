package main

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"

	"github.com/c0rtexR/llm_service/internal/provider"
	"github.com/c0rtexR/llm_service/internal/server"
	pb "github.com/c0rtexR/llm_service/proto"
)

const (
	bufSize  = 1024 * 1024
	testPort = "50051" // Use actual port for testing
)

var lis *bufconn.Listener

func init() {
	lis = bufconn.Listen(bufSize)
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

// mockProvider implements the LLMProvider interface for testing
type mockProvider struct {
	mock.Mock
}

func (m *mockProvider) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	args := m.Called(ctx, req)
	if resp := args.Get(0); resp != nil {
		return resp.(*pb.LLMResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProvider) InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error) {
	args := m.Called(ctx, req)
	return args.Get(0).(<-chan *pb.LLMStreamResponse), args.Get(1).(<-chan error)
}

func TestMain(t *testing.T) {
	// Set up test environment
	os.Setenv("PORT", testPort)
	defer os.Unsetenv("PORT")

	// Create mock provider
	mockProv := &mockProvider{}
	mockProv.On("Invoke", mock.Anything, mock.MatchedBy(func(req *pb.LLMRequest) bool {
		return req.Provider == "mock"
	})).Return(&pb.LLMResponse{
		Content: "test response",
	}, nil)

	// Start server in background
	go func() {
		if err := startServer(mockProv); err != nil {
			t.Errorf("server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Set up client connection
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewLLMServiceClient(conn)

	// Test cases
	tests := []struct {
		name        string
		req         *pb.LLMRequest
		expectError bool
	}{
		{
			name: "valid request",
			req: &pb.LLMRequest{
				Provider: "mock",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid provider",
			req: &pb.LLMRequest{
				Provider: "invalid",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Invoke(ctx, tt.req)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, "test response", resp.Content)
			}
		})
	}

	mockProv.AssertExpectations(t)
}

func startServer(mockProv provider.LLMProvider) error {
	// Create a gRPC server
	grpcServer := grpc.NewServer()

	// Initialize providers
	providers := map[string]provider.LLMProvider{
		"mock": mockProv,
	}

	// Register LLM service
	llmServer := server.New(providers)
	pb.RegisterLLMServiceServer(grpcServer, llmServer)

	// Enable reflection
	reflection.Register(grpcServer)

	// Serve using our bufconn listener
	return grpcServer.Serve(lis)
}
