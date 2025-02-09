package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/c0rtexR/llm_service/internal/provider"
	pb "github.com/c0rtexR/llm_service/proto"
)

// mockProvider implements the LLMProvider interface for testing
type mockProvider struct {
	mock.Mock
}

func (m *mockProvider) Invoke(ctx context.Context, req *pb.LLMRequest) (*pb.LLMResponse, error) {
	args := m.Called(ctx, req)
	if resp := args.Get(0); resp != nil {
		return resp.(*pb.LLMResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProvider) InvokeStream(ctx context.Context, req *pb.LLMRequest) (<-chan *pb.LLMStreamResponse, <-chan error) {
	args := m.Called(ctx, req)
	return args.Get(0).(<-chan *pb.LLMStreamResponse), args.Get(1).(<-chan error)
}

// mockStream implements pb.LLMService_InvokeStreamServer for testing
type mockStream struct {
	mock.Mock
	ctx context.Context
}

func (m *mockStream) Send(resp *pb.LLMStreamResponse) error {
	args := m.Called(resp)
	return args.Error(0)
}

func (m *mockStream) Context() context.Context {
	return m.ctx
}

func (m *mockStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *mockStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *mockStream) SetTrailer(metadata.MD) {
}

func (m *mockStream) SendMsg(msg interface{}) error {
	return m.Send(msg.(*pb.LLMStreamResponse))
}

func (m *mockStream) RecvMsg(msg interface{}) error {
	return nil
}

func TestLLMServer_Invoke(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		setupMock   func(*mockProvider)
		expectError bool
	}{
		{
			name:     "successful call",
			provider: "test",
			setupMock: func(m *mockProvider) {
				m.On("Invoke", mock.Anything, mock.MatchedBy(func(req *pb.LLMRequest) bool {
					return req.Provider == "test"
				})).Return(&pb.LLMResponse{
					Content: "test response",
				}, nil)
			},
			expectError: false,
		},
		{
			name:     "provider error",
			provider: "test",
			setupMock: func(m *mockProvider) {
				m.On("Invoke", mock.Anything, mock.MatchedBy(func(req *pb.LLMRequest) bool {
					return req.Provider == "test"
				})).Return(nil, errors.New("provider error"))
			},
			expectError: true,
		},
		{
			name:        "unknown provider",
			provider:    "unknown",
			setupMock:   func(m *mockProvider) {},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockProvider{}
			tt.setupMock(mock)

			server := New(map[string]provider.LLMProvider{
				"test": mock,
			})

			resp, err := server.Invoke(context.Background(), &pb.LLMRequest{
				Provider: tt.provider,
			})

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, "test response", resp.Content)
			}

			mock.AssertExpectations(t)
		})
	}
}

func TestLLMServer_InvokeStream(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		setupMock   func(*mockProvider, chan *pb.LLMStreamResponse, chan error)
		setupStream func(*mockStream)
		expectError bool
	}{
		{
			name:     "successful stream",
			provider: "test",
			setupMock: func(m *mockProvider, respChan chan *pb.LLMStreamResponse, errChan chan error) {
				m.On("InvokeStream", mock.Anything, mock.MatchedBy(func(req *pb.LLMRequest) bool {
					return req.Provider == "test"
				})).Run(func(args mock.Arguments) {
					respChan <- &pb.LLMStreamResponse{
						Type:    pb.ResponseType_TYPE_CONTENT,
						Content: "chunk 1",
					}
					respChan <- &pb.LLMStreamResponse{
						Type:         pb.ResponseType_TYPE_FINISH_REASON,
						FinishReason: "stop",
					}
					close(respChan)
					close(errChan)
				}).Return((<-chan *pb.LLMStreamResponse)(respChan), (<-chan error)(errChan))
			},
			setupStream: func(m *mockStream) {
				m.On("Send", mock.MatchedBy(func(resp *pb.LLMStreamResponse) bool {
					return resp.Type == pb.ResponseType_TYPE_CONTENT && resp.Content == "chunk 1"
				})).Return(nil)
				m.On("Send", mock.MatchedBy(func(resp *pb.LLMStreamResponse) bool {
					return resp.Type == pb.ResponseType_TYPE_FINISH_REASON && resp.FinishReason == "stop"
				})).Return(nil)
			},
			expectError: false,
		},
		{
			name:     "provider error",
			provider: "test",
			setupMock: func(m *mockProvider, respChan chan *pb.LLMStreamResponse, errChan chan error) {
				m.On("InvokeStream", mock.Anything, mock.MatchedBy(func(req *pb.LLMRequest) bool {
					return req.Provider == "test"
				})).Run(func(args mock.Arguments) {
					errChan <- errors.New("provider error")
					close(respChan)
					close(errChan)
				}).Return((<-chan *pb.LLMStreamResponse)(respChan), (<-chan error)(errChan))
			},
			setupStream: func(m *mockStream) {},
			expectError: true,
		},
		{
			name:        "unknown provider",
			provider:    "unknown",
			setupMock:   func(m *mockProvider, respChan chan *pb.LLMStreamResponse, errChan chan error) {},
			setupStream: func(m *mockStream) {},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockProvider{}
			stream := &mockStream{ctx: context.Background()}
			respChan := make(chan *pb.LLMStreamResponse)
			errChan := make(chan error)

			tt.setupMock(mock, respChan, errChan)
			tt.setupStream(stream)

			server := New(map[string]provider.LLMProvider{
				"test": mock,
			})

			err := server.InvokeStream(&pb.LLMRequest{
				Provider: tt.provider,
			}, stream)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			mock.AssertExpectations(t)
			stream.AssertExpectations(t)
		})
	}
}
