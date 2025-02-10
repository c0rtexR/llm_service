package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/c0rtexR/llm_service/client"
	"github.com/c0rtexR/llm_service/pkg/provider"
	"github.com/c0rtexR/llm_service/proto"
)

func main() {
	// Initialize providers with your API keys
	providers := make(map[client.Provider]provider.LLMProvider)

	// Initialize OpenAI provider
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers[client.OpenAI] = provider.NewOpenAI(&provider.Config{
			APIKey:       key,
			DefaultModel: "gpt-3.5-turbo",
		})
	}

	// Initialize Anthropic provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers[client.Anthropic] = provider.NewAnthropic(&provider.Config{
			APIKey:       key,
			DefaultModel: "claude-2",
		})
	}

	// Create the client
	llm := client.New(providers)

	// Example 1: Simple completion using the convenience method
	fmt.Println("\nExample 1: Simple completion")
	resp, err := llm.InvokeSimple(context.Background(), client.OpenAI, "What is the capital of France?")
	if err != nil {
		log.Fatalf("Failed to get response: %v", err)
	}
	fmt.Printf("Response: %s\n", resp.Content)

	// Example 2: Interactive chat conversation
	fmt.Println("\nExample 2: Interactive chat conversation")
	conversation := []client.Message{
		{
			Role:    client.RoleSystem,
			Content: "You are a helpful AI teacher. Keep explanations simple and use analogies.",
		},
	}

	// First question
	conversation = append(conversation, client.Message{
		Role:    client.RoleUser,
		Content: "What is a neural network?",
	})

	resp, err = llm.Invoke(context.Background(), client.OpenAI, conversation, client.WithTemperature(0.7))
	if err != nil {
		log.Fatalf("Failed to get response: %v", err)
	}

	// Add assistant's response to conversation
	conversation = append(conversation, client.Message{
		Role:    client.RoleAssistant,
		Content: resp.Content,
	})

	fmt.Printf("User: What is a neural network?\n")
	fmt.Printf("Assistant: %s\n", resp.Content)

	// Follow-up question
	conversation = append(conversation, client.Message{
		Role:    client.RoleUser,
		Content: "Can you explain how they learn using a simple analogy?",
	})

	resp, err = llm.Invoke(context.Background(), client.OpenAI, conversation, client.WithTemperature(0.7))
	if err != nil {
		log.Fatalf("Failed to get response: %v", err)
	}

	fmt.Printf("\nUser: Can you explain how they learn using a simple analogy?\n")
	fmt.Printf("Assistant: %s\n", resp.Content)

	// Example 3: Creative writing with streaming
	fmt.Println("\nExample 3: Creative writing with streaming")
	storyPrompt := []client.Message{
		{
			Role:    client.RoleSystem,
			Content: "You are a creative storyteller who specializes in short, engaging stories.",
		},
		{
			Role:    client.RoleUser,
			Content: "Write a short story about a robot discovering art. Make it heartwarming.",
		},
	}

	respCh, errCh := llm.InvokeStream(
		context.Background(),
		client.Anthropic,
		storyPrompt,
		client.WithModel("claude-2"),
		client.WithTemperature(0.9),
	)

	// Handle streaming responses
	go func() {
		for err := range errCh {
			if err != nil {
				log.Printf("Stream error: %v", err)
			}
		}
	}()

	fmt.Println("\nGenerating story...")
	for resp := range respCh {
		switch resp.Type {
		case proto.ResponseType_TYPE_CONTENT:
			fmt.Print(resp.Content)
		case proto.ResponseType_TYPE_FINISH_REASON:
			fmt.Printf("\n\nFinish reason: %s\n", resp.FinishReason)
		case proto.ResponseType_TYPE_USAGE:
			fmt.Printf("Token usage: %+v\n", resp.Usage)
		}
	}
}

// Example of maintaining conversation state in a struct
type Conversation struct {
	messages []client.Message
	llm      *client.Client
}

func NewConversation(llm *client.Client, systemPrompt string) *Conversation {
	return &Conversation{
		llm: llm,
		messages: []client.Message{
			{
				Role:    client.RoleSystem,
				Content: systemPrompt,
			},
		},
	}
}

func (c *Conversation) AddUserMessage(content string) {
	c.messages = append(c.messages, client.Message{
		Role:    client.RoleUser,
		Content: content,
	})
}

func (c *Conversation) AddAssistantMessage(content string) {
	c.messages = append(c.messages, client.Message{
		Role:    client.RoleAssistant,
		Content: content,
	})
}

func (c *Conversation) GetResponse(ctx context.Context, options ...client.Option) (string, error) {
	resp, err := c.llm.Invoke(ctx, client.OpenAI, c.messages, options...)
	if err != nil {
		return "", err
	}

	// Add the response to the conversation history
	c.AddAssistantMessage(resp.Content)
	return resp.Content, nil
}

// Example of using the Conversation struct
func exampleConversation() error {
	llm := createLLMClient()

	conv := NewConversation(llm, "You are a helpful coding assistant specializing in Go.")

	// First question
	conv.AddUserMessage("What's the best way to handle errors in Go?")
	resp, err := conv.GetResponse(context.Background(),
		client.WithTemperature(0.7),
		client.WithModel("gpt-4"),
	)
	if err != nil {
		return err
	}
	fmt.Printf("Response: %s\n", resp)

	// Follow-up question
	conv.AddUserMessage("Can you show an example of error wrapping?")
	resp, err = conv.GetResponse(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("Response: %s\n", resp)

	return nil
}

// Example of generating product descriptions
func generateProductDescription(productName string) (string, error) {
	llm := createLLMClient()

	messages := []client.Message{
		{
			Role:    client.RoleSystem,
			Content: "You are a professional copywriter specializing in e-commerce product descriptions.",
		},
		{
			Role:    client.RoleUser,
			Content: fmt.Sprintf("Write a compelling product description for: %s", productName),
		},
	}

	resp, err := llm.Invoke(
		context.Background(),
		client.OpenAI,
		messages,
		client.WithTemperature(0.7),
		client.WithMaxTokens(200),
	)

	if err != nil {
		return "", fmt.Errorf("failed to generate description: %w", err)
	}

	return resp.Content, nil
}

func createLLMClient() *client.Client {
	providers := make(map[client.Provider]provider.LLMProvider)

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers[client.OpenAI] = provider.NewOpenAI(&provider.Config{
			APIKey:       key,
			DefaultModel: "gpt-3.5-turbo",
		})
	}

	return client.New(providers)
}
