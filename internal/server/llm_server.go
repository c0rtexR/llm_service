package server

import (
	"context"
	"fmt"

	"llmservice/internal/provider"
	pb "llmservice/proto"
)

// LLMServer implements the LLMServiceServer interface
type LLMServer struct {
	pb.UnimplementedLLMServiceServer
	providers map[string]provider.LLMProvider
}

// New creates a new LLM server with the given providers
func New(providers map[string]provider.LLMProvider) *LLMServer {
	return &LLMServer{
		providers: providers,
	}
}

// Invoke implements the unary LLM call
func (s *LLMServer) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	p, err := s.getProvider(req.Provider)
	if err != nil {
		return nil, err
	}

	return p.Invoke(ctx, req)
}

// InvokeStream implements the streaming LLM call
func (s *LLMServer) InvokeStream(req *pb.LLMRequest, stream pb.LLMService_InvokeStreamServer) error {
	p, err := s.getProvider(req.Provider)
	if err != nil {
		return err
	}

	respChan, errChan := p.InvokeStream(stream.Context(), req)

	// Forward response chunks to the gRPC stream
	for {
		select {
		case resp, ok := <-respChan:
			if !ok {
				// Response channel closed, we're done
				return nil
			}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("failed to send response: %w", err)
			}
		case err, ok := <-errChan:
			if ok && err != nil {
				return fmt.Errorf("provider error: %w", err)
			}
			if !ok {
				// Error channel closed without error
				return nil
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// getProvider returns the provider for the given name
func (s *LLMServer) getProvider(name string) (provider.LLMProvider, error) {
	p, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
	return p, nil
}
