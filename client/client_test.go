package client

import (
	"context"
	"testing"

	"github.com/c0rtexR/llm_service/internal/provider"
	"github.com/c0rtexR/llm_service/proto"
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

func TestClient_InvokeSimple(t *testing.T) {
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

	providers := map[Provider]provider.LLMProvider{
		OpenAI: mock,
	}

	client := New(providers)

	// Test simple invocation
	resp, err := client.InvokeSimple(context.Background(), OpenAI, "Hello", WithTemperature(0.7))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp != mockResp {
		t.Errorf("Expected response %v, got %v", mockResp, resp)
	}

	// Verify request
	req := mock.lastRequest
	if req.Provider != string(OpenAI) {
		t.Errorf("Expected provider 'openai', got %s", req.Provider)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != string(RoleUser) {
		t.Errorf("Expected role 'user', got %s", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "Hello" {
		t.Errorf("Expected content 'Hello', got %s", req.Messages[0].Content)
	}
	if req.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %f", req.Temperature)
	}
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

	providers := map[Provider]provider.LLMProvider{
		OpenAI: mock,
	}

	client := New(providers)

	// Test chat conversation
	messages := []Message{
		{
			Role:    RoleSystem,
			Content: "You are a helpful assistant",
		},
		{
			Role:    RoleUser,
			Content: "Hello",
		},
		{
			Role:    RoleAssistant,
			Content: "Hi! How can I help you?",
		},
		{
			Role:    RoleUser,
			Content: "Tell me more",
		},
	}

	resp, err := client.Invoke(context.Background(), OpenAI, messages, WithTemperature(0.7))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp != mockResp {
		t.Errorf("Expected response %v, got %v", mockResp, resp)
	}

	// Verify request
	req := mock.lastRequest
	if req.Provider != string(OpenAI) {
		t.Errorf("Expected provider 'openai', got %s", req.Provider)
	}
	if len(req.Messages) != len(messages) {
		t.Fatalf("Expected %d messages, got %d", len(messages), len(req.Messages))
	}
	for i, msg := range req.Messages {
		if msg.Role != string(messages[i].Role) {
			t.Errorf("Message %d: expected role %s, got %s", i, messages[i].Role, msg.Role)
		}
		if msg.Content != messages[i].Content {
			t.Errorf("Message %d: expected content %s, got %s", i, messages[i].Content, msg.Content)
		}
	}
}

func TestClient_InvokeStreamSimple(t *testing.T) {
	mockResp := &proto.LLMStreamResponse{
		Type:    proto.ResponseType_TYPE_CONTENT,
		Content: "Test response",
	}

	mock := &MockProvider{
		streamResp: mockResp,
	}

	providers := map[Provider]provider.LLMProvider{
		OpenAI: mock,
	}

	client := New(providers)

	// Test streaming
	respCh, errCh := client.InvokeStreamSimple(context.Background(), OpenAI, "Hello", WithModel("test-model"))

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
	if req.Provider != string(OpenAI) {
		t.Errorf("Expected provider 'openai', got %s", req.Provider)
	}
	if req.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got %s", req.Model)
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

	providers := map[Provider]provider.LLMProvider{
		OpenAI: mock,
	}

	client := New(providers)

	// Test chat conversation streaming
	messages := []Message{
		{
			Role:    RoleSystem,
			Content: "You are a helpful assistant",
		},
		{
			Role:    RoleUser,
			Content: "Hello",
		},
		{
			Role:    RoleAssistant,
			Content: "Hi! How can I help you?",
		},
		{
			Role:    RoleUser,
			Content: "Tell me more",
		},
	}

	respCh, errCh := client.InvokeStream(context.Background(), OpenAI, messages, WithModel("test-model"))

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
	if req.Provider != string(OpenAI) {
		t.Errorf("Expected provider 'openai', got %s", req.Provider)
	}
	if req.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got %s", req.Model)
	}
	if len(req.Messages) != len(messages) {
		t.Fatalf("Expected %d messages, got %d", len(messages), len(req.Messages))
	}
	for i, msg := range req.Messages {
		if msg.Role != string(messages[i].Role) {
			t.Errorf("Message %d: expected role %s, got %s", i, messages[i].Role, msg.Role)
		}
		if msg.Content != messages[i].Content {
			t.Errorf("Message %d: expected content %s, got %s", i, messages[i].Content, msg.Content)
		}
	}
}

func TestClient_InvalidProvider(t *testing.T) {
	client := New(map[Provider]provider.LLMProvider{})

	// Test invalid provider with Invoke
	_, err := client.Invoke(context.Background(), "invalid", []Message{})
	if err == nil {
		t.Error("Expected error for invalid provider, got nil")
	}

	// Test invalid provider with InvokeSimple
	_, err = client.InvokeSimple(context.Background(), "invalid", "test")
	if err == nil {
		t.Error("Expected error for invalid provider, got nil")
	}

	// Test invalid provider with InvokeStream
	_, errCh := client.InvokeStream(context.Background(), "invalid", []Message{})
	if errCh == nil {
		t.Error("Expected error channel for invalid provider, got nil")
	}
	for err := range errCh {
		if err == nil {
			t.Error("Expected error for invalid provider in stream, got nil")
		}
	}

	// Test invalid provider with InvokeStreamSimple
	_, errCh = client.InvokeStreamSimple(context.Background(), "invalid", "test")
	if errCh == nil {
		t.Error("Expected error channel for invalid provider, got nil")
	}
	for err := range errCh {
		if err == nil {
			t.Error("Expected error for invalid provider in stream, got nil")
		}
	}
}
