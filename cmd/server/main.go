package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/c0rtexR/llm_service/internal/provider"
	"github.com/c0rtexR/llm_service/internal/provider/anthropic"
	"github.com/c0rtexR/llm_service/internal/provider/gemini"
	"github.com/c0rtexR/llm_service/internal/provider/openai"
	"github.com/c0rtexR/llm_service/internal/provider/openrouter"
	"github.com/c0rtexR/llm_service/internal/server"
	pb "github.com/c0rtexR/llm_service/proto"

	"google.golang.org/grpc"
)

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	providers := make(map[string]provider.LLMProvider)

	// Initialize OpenRouter provider if API key is set
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		providers["openrouter"] = openrouter.New(&provider.Config{
			APIKey: key,
		})
	}

	// Initialize OpenAI provider if API key is set
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers["openai"] = openai.New(&provider.Config{
			APIKey: key,
		})
	}

	// Initialize Anthropic provider if API key is set
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers["anthropic"] = anthropic.New(&provider.Config{
			APIKey: key,
		})
	}

	// Initialize Gemini provider if API key is set
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		p, err := gemini.New(&provider.Config{
			APIKey: key,
		})
		if err != nil {
			log.Printf("failed to initialize Gemini provider: %v", err)
		} else {
			providers["gemini"] = p
		}
	}

	s := grpc.NewServer()
	pb.RegisterLLMServiceServer(s, server.New(providers))

	fmt.Printf("Server listening at %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
