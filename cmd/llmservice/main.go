package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"llmservice/internal/provider"
	"llmservice/internal/provider/anthropic"
	"llmservice/internal/provider/gemini"
	"llmservice/internal/provider/openai"
	"llmservice/internal/provider/openrouter"
	"llmservice/internal/server"
	pb "llmservice/proto"
)

const (
	defaultPort = "50051"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Initialize providers
	providers := make(map[string]provider.LLMProvider)

	// OpenRouter provider
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		p := openrouter.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "google/gemini-flash-1.5-8b", // Exact model ID
		})
		providers["openrouter"] = p
		logger.Info("initialized OpenRouter provider")
	}

	// OpenAI provider
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p := openai.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "gpt-3.5-turbo", // Default model for OpenAI
		})
		providers["openai"] = p
		logger.Info("initialized OpenAI provider")
	}

	// Anthropic provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p := anthropic.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "claude-2", // Default model for Anthropic
		})
		providers["anthropic"] = p
		logger.Info("initialized Anthropic provider")
	}

	// Gemini provider
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		p, err := gemini.New(&provider.Config{
			APIKey:       key,
			DefaultModel: "gemini-1.5-flash-8b", // Updated to match your model
		})
		if err != nil {
			logger.Fatal("failed to initialize Gemini provider", zap.Error(err))
		}
		providers["gemini"] = p
		logger.Info("initialized Gemini provider")
	}

	if len(providers) == 0 {
		logger.Fatal("no providers initialized - please set at least one provider API key")
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()

	// Register LLM service
	llmServer := server.New(providers)
	pb.RegisterLLMServiceServer(grpcServer, llmServer)

	// Register health check service
	healthServer := server.NewHealthServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	// Enable reflection for development tools
	reflection.Register(grpcServer)

	// Print registered providers
	logger.Info("registered providers", zap.Strings("providers", func() []string {
		var names []string
		for name := range providers {
			names = append(names, name)
		}
		return names
	}()))

	// Start listening
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))

		// Gracefully stop the gRPC server
		grpcServer.GracefulStop()
	}()

	// Start serving
	logger.Info("starting gRPC server", zap.String("port", port))
	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatal("failed to serve", zap.Error(err))
	}
}
