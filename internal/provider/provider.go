package provider

import (
	"context"

	pb "github.com/c0rtexR/llm_service/proto"
)

// LLMProvider defines the interface that all LLM providers must implement
type LLMProvider interface {
	// Invoke sends a request to an LLM and returns a single response
	Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error)

	// InvokeStream sends a request to an LLM and returns a stream of responses
	InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error)
}

// Config holds common configuration for LLM providers
type Config struct {
	// APIKey is the authentication key for the provider
	APIKey string

	// DefaultModel is the model to use if none is specified in the request
	DefaultModel string

	// BaseURL is the base URL for API requests (optional, for testing)
	BaseURL string
}

// NewConfig creates a new provider configuration
func NewConfig(apiKey string, defaultModel string) *Config {
	return &Config{
		APIKey:       apiKey,
		DefaultModel: defaultModel,
	}
}

// WithBaseURL sets a custom base URL for the provider
func (c *Config) WithBaseURL(url string) *Config {
	c.BaseURL = url
	return c
}
