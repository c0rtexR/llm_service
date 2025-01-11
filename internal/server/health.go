package server

import (
	"context"

	"google.golang.org/grpc/health/grpc_health_v1"
)

// healthServer implements the gRPC health check service
type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
}

// Check implements the gRPC health check service
func (s *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

// Watch implements the gRPC health check service
func (s *healthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	return stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	})
}

// NewHealthServer creates a new health check server
func NewHealthServer() *healthServer {
	return &healthServer{}
}
