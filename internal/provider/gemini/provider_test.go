package gemini

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"llmservice/internal/provider"
	pb "llmservice/proto"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		config      *provider.Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &provider.Config{
				APIKey:       "test-key",
				DefaultModel: "gemini-pro",
			},
			expectError: false,
		},
		{
			name: "missing api key",
			config: &provider.Config{
				DefaultModel: "gemini-pro",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.config)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, p)
			} else {
				require.NoError(t, err)
				require.NotNil(t, p)
				require.Equal(t, tt.config.DefaultModel, p.defaultModel)
			}
		})
	}
}

func TestProvider_Invoke(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	p, err := New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "gemini-1.5-flash-8b",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		request     *pb.LLMRequest
		expectError bool
	}{
		{
			name: "basic request",
			request: &pb.LLMRequest{
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Say hello",
					},
				},
				Temperature: 0.7,
				TopP:        0.9,
			},
			expectError: false,
		},
		{
			name: "with system message",
			request: &pb.LLMRequest{
				Messages: []*pb.ChatMessage{
					{
						Role:    "system",
						Content: "You are a helpful assistant.",
					},
					{
						Role:    "user",
						Content: "Say hello",
					},
				},
			},
			expectError: false,
		},
		{
			name: "with chat history",
			request: &pb.LLMRequest{
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "What is 2+2?",
					},
					{
						Role:    "assistant",
						Content: "2+2 equals 4",
					},
					{
						Role:    "user",
						Content: "What did I ask you?",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.Invoke(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.Content)
			}
		})
	}
}

func TestProvider_InvokeStream(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	p, err := New(&provider.Config{
		APIKey:       apiKey,
		DefaultModel: "gemini-1.5-flash-8b",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		request     *pb.LLMRequest
		expectError bool
	}{
		{
			name: "basic stream request",
			request: &pb.LLMRequest{
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Count from 1 to 5",
					},
				},
				Temperature: 0.7,
				TopP:        0.9,
			},
			expectError: false,
		},
		{
			name: "stream with chat history",
			request: &pb.LLMRequest{
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Let's count numbers.",
					},
					{
						Role:    "assistant",
						Content: "Sure, I can help you count numbers!",
					},
					{
						Role:    "user",
						Content: "Count from 1 to 5 slowly",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respChan, errChan := p.InvokeStream(context.Background(), tt.request)

			var gotContent bool
			var gotFinish bool

			for resp := range respChan {
				switch resp.Type {
				case pb.ResponseType_TYPE_CONTENT:
					require.NotEmpty(t, resp.Content)
					gotContent = true
				case pb.ResponseType_TYPE_FINISH_REASON:
					require.Equal(t, "stop", resp.FinishReason)
					gotFinish = true
				}
			}

			// Check for any errors
			select {
			case err := <-errChan:
				if tt.expectError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			default:
				require.False(t, tt.expectError)
			}

			require.True(t, gotContent, "should have received content")
			require.True(t, gotFinish, "should have received finish reason")
		})
	}
}
