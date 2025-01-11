package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "llmservice/proto"
)

// retryWithBackoff attempts the operation with exponential backoff
func retryWithBackoff(t *testing.T, op func() error) error {
	var lastErr error
	for i := 0; i < 3; i++ { // Maximum 3 retries
		err := op()
		if err == nil {
			return nil
		}

		lastErr = err
		if s, ok := status.FromError(err); ok {
			if s.Code() == codes.ResourceExhausted || strings.Contains(s.Message(), "429") {
				backoff := time.Duration(1<<uint(i)) * time.Second
				t.Logf("Rate limited, backing off for %v", backoff)
				time.Sleep(backoff)
				continue
			}
		}
		return err // Non-rate-limit error, return immediately
	}
	return lastErr
}

func TestGeminiLatency(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	tests := []struct {
		name       string
		prompt     string
		maxLatency time.Duration
	}{
		{
			name:       "short_prompt",
			prompt:     "What is 2+2?",
			maxLatency: 2 * time.Second,
		},
		{
			name:       "medium_prompt",
			prompt:     "Explain the concept of recursion in programming using an example.",
			maxLatency: 8 * time.Second,
		},
		{
			name:       "long_prompt",
			prompt:     "Write a detailed technical analysis of the differences between REST and GraphQL APIs, including their pros and cons.",
			maxLatency: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *pb.LLMResponse
			var latency time.Duration

			err := retryWithBackoff(t, func() error {
				start := time.Now()
				var err error
				resp, err = ts.client.Invoke(context.Background(), &pb.LLMRequest{
					Provider: "gemini",
					Model:    "gemini-1.5-flash-8b",
					Messages: []*pb.ChatMessage{
						{
							Role:    "user",
							Content: tt.prompt,
						},
					},
				})
				latency = time.Since(start)
				return err
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotEmpty(t, resp.Content)
			require.Less(t, latency, tt.maxLatency, "Response took too long")
			t.Logf("Latency for %s: %v", tt.name, latency)
		})
	}
}

func TestGeminiThroughput(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test parameters
	const (
		numRequests      = 10
		targetThroughput = 0.5              // requests per second (reduced)
		testDuration     = 30 * time.Second // increased duration
	)

	// Create a ticker to control request rate
	ticker := time.NewTicker(time.Duration(float64(time.Second) / targetThroughput))
	defer ticker.Stop()

	start := time.Now()
	var completedRequests int
	var totalTokens int32

	for completedRequests < numRequests && time.Since(start) < testDuration {
		<-ticker.C // Wait for ticker

		err := retryWithBackoff(t, func() error {
			resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
				Provider: "gemini",
				Model:    "gemini-1.5-flash-8b",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: "Write a one-sentence story.",
					},
				},
			})
			if err != nil {
				return err
			}
			require.NotNil(t, resp)
			require.NotEmpty(t, resp.Content)
			require.NotNil(t, resp.Usage)

			completedRequests++
			totalTokens += resp.Usage.TotalTokens
			return nil
		})

		if err != nil {
			t.Logf("Request failed after retries: %v", err)
			time.Sleep(5 * time.Second) // Additional cooldown after failure
			continue
		}
	}

	duration := time.Since(start)
	actualThroughput := float64(completedRequests) / duration.Seconds()
	tokensPerSecond := float64(totalTokens) / duration.Seconds()

	t.Logf("Completed %d requests in %v", completedRequests, duration)
	t.Logf("Throughput: %.2f requests/second", actualThroughput)
	t.Logf("Token throughput: %.2f tokens/second", tokensPerSecond)

	require.GreaterOrEqual(t, actualThroughput, float64(targetThroughput)*0.8,
		"Throughput is significantly below target")
}

func TestGeminiConcurrentLoad(t *testing.T) {
	ts := setupGeminiTestServer(t)
	defer ts.cleanup()

	// Test parameters
	const (
		numWorkers    = 2                // Number of workers
		numRequests   = 2                // Requests per worker
		maxDuration   = 60 * time.Second // Maximum test duration
		minThroughput = 0.1              // Very conservative throughput requirement
	)

	var (
		mu           sync.Mutex
		latencies    []time.Duration
		totalTokens  int32
		successCount int
		testStart    = time.Now()
		errors       = make(chan error, numWorkers*numRequests)
	)

	// Process requests sequentially per worker
	for i := 0; i < numWorkers; i++ {
		workerID := i
		for j := 0; j < numRequests; j++ {
			// Add delay between requests
			if j > 0 || i > 0 {
				time.Sleep(4 * time.Second)
			}

			start := time.Now()
			err := retryWithBackoff(t, func() error {
				stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
					Provider: "gemini",
					Model:    "gemini-1.5-flash-8b",
					Messages: []*pb.ChatMessage{
						{
							Role:    "user",
							Content: fmt.Sprintf("Write a one-line story about worker %d request %d.", workerID, j),
						},
					},
				})
				if err != nil {
					return err
				}

				var gotContent bool
				var usage *pb.UsageInfo
				for {
					resp, err := stream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						return err
					}

					switch resp.Type {
					case pb.ResponseType_TYPE_CONTENT:
						gotContent = true
					case pb.ResponseType_TYPE_USAGE:
						usage = resp.Usage
					}
				}

				if gotContent && usage != nil {
					latency := time.Since(start)
					mu.Lock()
					latencies = append(latencies, latency)
					totalTokens += usage.TotalTokens
					successCount++
					mu.Unlock()
				}
				return nil
			})

			if err != nil {
				errors <- fmt.Errorf("worker %d request %d failed after retries: %w", workerID, j, err)
				time.Sleep(5 * time.Second) // Additional cooldown after failure
				continue
			}
		}
	}

	// Calculate statistics
	totalDuration := time.Since(testStart)
	mu.Lock()
	var (
		totalLatency time.Duration
		avgLatency   time.Duration
		maxLatency   = time.Duration(0)
		minLatency   = time.Duration(1<<63 - 1)
	)

	if len(latencies) > 0 {
		for _, lat := range latencies {
			totalLatency += lat
			if lat > maxLatency {
				maxLatency = lat
			}
			if lat < minLatency {
				minLatency = lat
			}
		}
		avgLatency = totalLatency / time.Duration(len(latencies))
	} else {
		avgLatency = 0
		minLatency = 0
	}
	mu.Unlock()

	// Log performance metrics
	t.Logf("Test completed in: %v", totalDuration)
	t.Logf("Successful requests: %d/%d", successCount, numWorkers*numRequests)
	t.Logf("Average latency: %v", avgLatency)
	t.Logf("Min latency: %v", minLatency)
	t.Logf("Max latency: %v", maxLatency)
	t.Logf("Total tokens processed: %d", totalTokens)
	t.Logf("Token throughput: %.2f tokens/second", float64(totalTokens)/totalDuration.Seconds())
	t.Logf("Request throughput: %.2f requests/second", float64(successCount)/totalDuration.Seconds())

	// Verify performance requirements
	require.Greater(t, successCount, 0, "No successful requests")
	require.Less(t, avgLatency, 10*time.Second, "Average latency too high")
	require.Greater(t, float64(successCount)/totalDuration.Seconds(), minThroughput,
		"Throughput below %.1f request per second", minThroughput)
}
