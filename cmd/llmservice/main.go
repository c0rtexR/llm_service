package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultPort = "50051"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Create listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.Fatal("Failed to listen",
			zap.String("port", port),
			zap.Error(err),
		)
	}

	// Create gRPC server
	server := grpc.NewServer()

	// Enable reflection for debugging
	reflection.Register(server)

	// Initialize signal handler for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server
	go func() {
		logger.Info("Starting gRPC server",
			zap.String("port", port),
		)
		if err := server.Serve(lis); err != nil {
			logger.Fatal("Failed to serve",
				zap.Error(err),
			)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("Received shutdown signal",
		zap.String("signal", sig.String()),
	)

	// Gracefully stop the server
	server.GracefulStop()
	logger.Info("Server stopped gracefully")
}
