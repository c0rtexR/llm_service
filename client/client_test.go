package client

import (
	"context"
	"testing"

	"llmservice/internal/provider"
	"llmservice/proto"
)

// MockProvider implements provider.LLMProvider for testing
type MockProvider struct {
	response    *proto.LLMResponse
	streamResp  *proto.LLMStreamResponse
	err         error
	lastRequest *proto.LLMRequest
}

func (m *MockProvider) Invoke(ctx context.Context, req *proto.LLMRequest) (*proto.LLMResponse, error) {
	m.lastRequest = req
	return m.response, m.err
}

func (m *MockProvider) InvokeStream(ctx context.Context, req *proto.LLMRequest) (<-chan *proto.LLMStreamResponse, <-chan error) {
	m.lastRequest = req
	respCh := make(chan *proto.LLMStreamResponse, 1)
	errCh := make(chan error, 1)

	if m.err != nil {
		errCh <- m.err
	} else if m.streamResp != nil {
		respCh <- m.streamResp
	}

	close(respCh)
	close(errCh)
	return respCh, errCh
}

func TestClient_Invoke(t *testing.T) {
	mockResp := &proto.LLMResponse{
		Content: "Test response",
		Usage: &proto.UsageInfo{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	mock := &MockProvider{
		response: mockResp,
	}

	providers := map[string]provider.LLMProvider{
		"test": mock,
	}

	client := New(providers)

	// Test basic invocation
	resp, err := client.Invoke(context.Background(), "test", "Hello", WithTemperature(0.7))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp != mockResp {
		t.Errorf("Expected response %v, got %v", mockResp, resp)
	}

	// Verify request
	req := mock.lastRequest
	if req.Provider != "test" {
		t.Errorf("Expected provider 'test', got %s", req.Provider)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("Expected role 'user', got %s", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "Hello" {
		t.Errorf("Expected content 'Hello', got %s", req.Messages[0].Content)
	}
	if req.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %f", req.Temperature)
	}

	// Test with system prompt
	resp, err = client.Invoke(context.Background(), "test", "Hello", WithSystemPrompt("You are a helpful assistant"))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req = mock.lastRequest
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("Expected first message role 'system', got %s", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "You are a helpful assistant" {
		t.Errorf("Expected system content 'You are a helpful assistant', got %s", req.Messages[0].Content)
	}
}

func TestClient_InvokeStream(t *testing.T) {
	mockResp := &proto.LLMStreamResponse{
		Type:    proto.ResponseType_TYPE_CONTENT,
		Content: "Test response",
	}

	mock := &MockProvider{
		streamResp: mockResp,
	}

	providers := map[string]provider.LLMProvider{
		"test": mock,
	}

	client := New(providers)

	// Test streaming
	respCh, errCh := client.InvokeStream(context.Background(), "test", "Hello", WithModel("test-model"))

	// Check for responses
	for resp := range respCh {
		if resp != mockResp {
			t.Errorf("Expected response %v, got %v", mockResp, resp)
		}
	}

	// Check for errors
	for err := range errCh {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify request
	req := mock.lastRequest
	if req.Provider != "test" {
		t.Errorf("Expected provider 'test', got %s", req.Provider)
	}
	if req.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got %s", req.Model)
	}
}
