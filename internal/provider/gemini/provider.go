package gemini

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"llmservice/internal/provider"
	pb "llmservice/proto"
)

type Provider struct {
	client       *genai.Client
	config       *provider.Config
	defaultModel string
}

// New creates a new Gemini provider
func New(config *provider.Config) (*Provider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("gemini: API key is required")
	}

	client, err := genai.NewClient(context.Background(), option.WithAPIKey(config.APIKey))
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to create client: %w", err)
	}

	defaultModel := config.DefaultModel
	if defaultModel == "" {
		defaultModel = "gemini-1.5-flash-8b" // Default to gemini-1.5-flash-8b
	}

	return &Provider{
		client:       client,
		config:       config,
		defaultModel: defaultModel,
	}, nil
}

// Invoke implements the LLMProvider interface
func (p *Provider) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	model := p.client.GenerativeModel(p.getModelName(req))

	// Configure model parameters
	if req.Temperature != 0 {
		model.SetTemperature(float32(req.Temperature))
	}
	if req.TopP != 0 {
		model.SetTopP(float32(req.TopP))
	}
	if req.TopK != 0 {
		model.SetTopK(int32(req.TopK))
	}
	if req.MaxTokens != 0 {
		model.SetMaxOutputTokens(int32(req.MaxTokens))
	}
	model.ResponseMIMEType = "text/plain"

	// Convert messages to Gemini format
	prompt := ""
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// Gemini doesn't support system messages directly, prepend to user message
			prompt += msg.Content + "\n"
		} else if msg.Role == "user" {
			prompt += msg.Content
		}
		// Skip assistant messages as they're not needed for the prompt
	}

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("gemini: generate failed: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: no response generated")
	}

	// Extract the response text
	content := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			content += string(text)
		}
	}

	return &pb.LLMResponse{
		Content: content,
		Usage: &pb.UsageInfo{
			// Gemini doesn't provide token counts directly
			TotalTokens: 0,
		},
	}, nil
}

// InvokeStream implements the LLMProvider interface
func (p *Provider) InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error) {
	responseChan := make(chan *pb.LLMStreamResponse)
	errorChan := make(chan error, 1)

	go func() {
		defer close(responseChan)
		defer close(errorChan)

		model := p.client.GenerativeModel(p.getModelName(req))

		// Configure model parameters
		if req.Temperature != 0 {
			model.SetTemperature(float32(req.Temperature))
		}
		if req.TopP != 0 {
			model.SetTopP(float32(req.TopP))
		}
		if req.TopK != 0 {
			model.SetTopK(int32(req.TopK))
		}
		if req.MaxTokens != 0 {
			model.SetMaxOutputTokens(int32(req.MaxTokens))
		}
		model.ResponseMIMEType = "text/plain"

		// Convert messages to Gemini format
		prompt := ""
		for _, msg := range req.Messages {
			if msg.Role == "system" {
				// Gemini doesn't support system messages directly, prepend to user message
				prompt += msg.Content + "\n"
			} else if msg.Role == "user" {
				prompt += msg.Content
			}
			// Skip assistant messages as they're not needed for the prompt
		}

		// Start the streaming session
		iter := model.GenerateContentStream(ctx, genai.Text(prompt))

		// Process the stream
		for {
			resp, err := iter.Next()
			if err == iterator.Done {
				// End of stream
				responseChan <- &pb.LLMStreamResponse{
					Type:         pb.ResponseType_TYPE_FINISH_REASON,
					FinishReason: "stop",
				}
				return
			}
			if err != nil {
				errorChan <- fmt.Errorf("gemini: stream failed: %w", err)
				return
			}

			// Process each candidate's content parts
			for _, candidate := range resp.Candidates {
				for _, part := range candidate.Content.Parts {
					if text, ok := part.(genai.Text); ok && len(text) > 0 {
						responseChan <- &pb.LLMStreamResponse{
							Type:    pb.ResponseType_TYPE_CONTENT,
							Content: string(text),
						}
					}
				}
			}
		}
	}()

	return responseChan, errorChan
}

// getModelName returns the model name to use, falling back to default if not specified
func (p *Provider) getModelName(req *pb.LLMRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.defaultModel
}
