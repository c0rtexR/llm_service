package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// healthServer implements the gRPC health checking protocol
type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
}

func (s *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func TestServerStartup(t *testing.T) {
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()

	// Register health service for testing
	grpc_health_v1.RegisterHealthServer(server, &healthServer{})

	// Start server in background
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("Server stopped serving: %v", err)
		}
	}()

	// Create a buffer connection for testing
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(t, err)
	defer conn.Close()

	// Wait for connection to be ready with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connected := false
	for !connected {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for connection to be ready")
		default:
			state := conn.GetState()
			if state == connectivity.Ready {
				connected = true
			} else {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	// Verify server is running by making a health check call
	healthClient := grpc_health_v1.NewHealthClient(conn)
	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err, "Health check should succeed while server is running")
	require.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.Status)

	// Gracefully stop server
	server.GracefulStop()

	// Close the listener
	lis.Close()

	// Try to make another health check - should fail
	_, err = healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.Error(t, err, "Health check should fail after server shutdown")
	require.Equal(t, codes.Unavailable, status.Code(err), "Expected Unavailable error after shutdown")
}
