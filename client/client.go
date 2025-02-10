package client

import (
	"context"
	"fmt"

	"github.com/c0rtexR/llm_service/pkg/provider"
	"github.com/c0rtexR/llm_service/proto"
)

// Provider represents supported LLM providers
type Provider string

const (
	OpenAI     Provider = "openai"
	Anthropic  Provider = "anthropic"
	Gemini     Provider = "gemini"
	OpenRouter Provider = "openrouter"
)

// Role represents the role of a message sender
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message represents a chat message
type Message struct {
	Role    Role
	Content string
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}

// IsValid checks if the provider is valid
func (p Provider) IsValid() bool {
	switch p {
	case OpenAI, Anthropic, Gemini, OpenRouter:
		return true
	default:
		return false
	}
}

// Client provides direct access to LLM functionality without gRPC
type Client struct {
	providers map[Provider]provider.LLMProvider
}

// New creates a new LLM client with the given providers
func New(providers map[Provider]provider.LLMProvider) *Client {
	return &Client{
		providers: providers,
	}
}

// Invoke sends a request to an LLM and returns a single response
func (c *Client) Invoke(ctx context.Context, provider Provider, messages []Message, options ...Option) (*proto.LLMResponse, error) {
	if !provider.IsValid() {
		return nil, fmt.Errorf("invalid provider: %s", provider)
	}

	p, err := c.getProvider(provider)
	if err != nil {
		return nil, err
	}

	// Convert messages to proto format
	protoMessages := make([]*proto.ChatMessage, len(messages))
	for i, msg := range messages {
		protoMessages[i] = &proto.ChatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	req := &proto.LLMRequest{
		Provider: provider.String(),
		Messages: protoMessages,
	}

	// Apply options
	for _, opt := range options {
		opt(req)
	}

	return p.Invoke(ctx, req)
}

// InvokeSimple is a convenience method for simple single-prompt requests
func (c *Client) InvokeSimple(ctx context.Context, provider Provider, prompt string, options ...Option) (*proto.LLMResponse, error) {
	messages := []Message{
		{
			Role:    RoleUser,
			Content: prompt,
		},
	}
	return c.Invoke(ctx, provider, messages, options...)
}

// InvokeStream sends a request to an LLM and returns a stream of responses
func (c *Client) InvokeStream(ctx context.Context, provider Provider, messages []Message, options ...Option) (<-chan *proto.LLMStreamResponse, <-chan error) {
	if !provider.IsValid() {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("invalid provider: %s", provider)
		close(errCh)
		return nil, errCh
	}

	p, err := c.getProvider(provider)
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- err
		close(errCh)
		return nil, errCh
	}

	// Convert messages to proto format
	protoMessages := make([]*proto.ChatMessage, len(messages))
	for i, msg := range messages {
		protoMessages[i] = &proto.ChatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	req := &proto.LLMRequest{
		Provider: provider.String(),
		Messages: protoMessages,
	}

	// Apply options
	for _, opt := range options {
		opt(req)
	}

	return p.InvokeStream(ctx, req)
}

// InvokeStreamSimple is a convenience method for simple single-prompt streaming requests
func (c *Client) InvokeStreamSimple(ctx context.Context, provider Provider, prompt string, options ...Option) (<-chan *proto.LLMStreamResponse, <-chan error) {
	messages := []Message{
		{
			Role:    RoleUser,
			Content: prompt,
		},
	}
	return c.InvokeStream(ctx, provider, messages, options...)
}

// Option is a function that modifies the LLM request
type Option func(*proto.LLMRequest)

// WithModel sets the model to use for the request
func WithModel(model string) Option {
	return func(req *proto.LLMRequest) {
		req.Model = model
	}
}

// WithTemperature sets the temperature for the request (0.0 to 1.0)
func WithTemperature(temperature float32) Option {
	return func(req *proto.LLMRequest) {
		req.Temperature = temperature
	}
}

// WithMaxTokens sets the maximum number of tokens for the response
func WithMaxTokens(maxTokens int32) Option {
	return func(req *proto.LLMRequest) {
		req.MaxTokens = maxTokens
	}
}

// WithTopP controls diversity via nucleus sampling (0.0 to 1.0)
func WithTopP(topP float32) Option {
	return func(req *proto.LLMRequest) {
		req.TopP = topP
	}
}

// WithTopK controls diversity by limiting to k most likely tokens
func WithTopK(topK int32) Option {
	return func(req *proto.LLMRequest) {
		req.TopK = topK
	}
}

// WithCacheControl sets the caching behavior for the request
func WithCacheControl(useCache bool, ttl int32) Option {
	return func(req *proto.LLMRequest) {
		req.CacheControl = &proto.CacheControl{
			UseCache: useCache,
			Ttl:      ttl,
		}
	}
}

// WithSystemPrompt sets a system prompt for the request
func WithSystemPrompt(systemPrompt string) Option {
	return func(req *proto.LLMRequest) {
		// Insert system message at the beginning
		req.Messages = append([]*proto.ChatMessage{
			{
				Role:    string(RoleSystem),
				Content: systemPrompt,
			},
		}, req.Messages...)
	}
}

func (c *Client) getProvider(name Provider) (provider.LLMProvider, error) {
	p, ok := c.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not initialized: %s", name)
	}
	return p, nil
}
