package client

import (
	"context"
	"fmt"

	"llmservice/internal/provider"
	"llmservice/proto"
)

// Client provides direct access to LLM functionality without gRPC
type Client struct {
	providers map[string]provider.LLMProvider
}

// New creates a new LLM client with the given providers
func New(providers map[string]provider.LLMProvider) *Client {
	return &Client{
		providers: providers,
	}
}

// Invoke sends a request to an LLM and returns a single response
func (c *Client) Invoke(ctx context.Context, providerName string, prompt string, options ...Option) (*proto.LLMResponse, error) {
	p, err := c.getProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &proto.LLMRequest{
		Provider: providerName,
		Messages: []*proto.ChatMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	// Apply options
	for _, opt := range options {
		opt(req)
	}

	return p.Invoke(ctx, req)
}

// InvokeStream sends a request to an LLM and returns a stream of responses
func (c *Client) InvokeStream(ctx context.Context, providerName string, prompt string, options ...Option) (<-chan *proto.LLMStreamResponse, <-chan error) {
	p, err := c.getProvider(providerName)
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- err
		close(errCh)
		return nil, errCh
	}

	req := &proto.LLMRequest{
		Provider: providerName,
		Messages: []*proto.ChatMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	// Apply options
	for _, opt := range options {
		opt(req)
	}

	return p.InvokeStream(ctx, req)
}

// Option is a function that modifies the LLM request
type Option func(*proto.LLMRequest)

// WithModel sets the model to use for the request
func WithModel(model string) Option {
	return func(req *proto.LLMRequest) {
		req.Model = model
	}
}

// WithTemperature sets the temperature for the request
func WithTemperature(temperature float32) Option {
	return func(req *proto.LLMRequest) {
		req.Temperature = temperature
	}
}

// WithMaxTokens sets the maximum number of tokens for the request
func WithMaxTokens(maxTokens int32) Option {
	return func(req *proto.LLMRequest) {
		req.MaxTokens = maxTokens
	}
}

// WithSystemPrompt sets a system prompt for the request
func WithSystemPrompt(systemPrompt string) Option {
	return func(req *proto.LLMRequest) {
		// Insert system message at the beginning
		req.Messages = append([]*proto.ChatMessage{
			{
				Role:    "system",
				Content: systemPrompt,
			},
		}, req.Messages...)
	}
}

func (c *Client) getProvider(name string) (provider.LLMProvider, error) {
	p, ok := c.providers[name]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
	return p, nil
}
