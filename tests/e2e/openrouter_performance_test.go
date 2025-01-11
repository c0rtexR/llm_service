package e2e

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "llmservice/proto"
)

func TestOpenRouterLatency(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
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
			maxLatency: 3 * time.Second,
		},
		{
			name:       "long_prompt",
			prompt:     "Write a detailed technical analysis of the differences between REST and GraphQL APIs, including their pros and cons.",
			maxLatency: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
				Provider: "openrouter",
				Model:    "google/gemini-flash-1.5-8b",
				Messages: []*pb.ChatMessage{
					{
						Role:    "user",
						Content: tt.prompt,
					},
				},
			})
			latency := time.Since(start)

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotEmpty(t, resp.Content)
			require.Less(t, latency, tt.maxLatency, "Response took too long")
			t.Logf("Latency for %s: %v", tt.name, latency)
		})
	}
}

func TestOpenRouterThroughput(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test parameters
	const (
		numRequests      = 10
		targetThroughput = 2 // requests per second
		testDuration     = 10 * time.Second
	)

	// Create a ticker to control request rate
	ticker := time.NewTicker(time.Second / time.Duration(targetThroughput))
	defer ticker.Stop()

	start := time.Now()
	var completedRequests int
	var totalTokens int32

	for completedRequests < numRequests && time.Since(start) < testDuration {
		<-ticker.C // Wait for ticker

		resp, err := ts.client.Invoke(context.Background(), &pb.LLMRequest{
			Provider: "openrouter",
			Model:    "google/gemini-flash-1.5-8b",
			Messages: []*pb.ChatMessage{
				{
					Role:    "user",
					Content: "Write a one-sentence story.",
				},
			},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.Content)
		require.NotNil(t, resp.Usage)

		completedRequests++
		totalTokens += resp.Usage.TotalTokens
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

func TestOpenRouterConcurrentLoad(t *testing.T) {
	ts := setupOpenRouterTestServer(t)
	defer ts.cleanup()

	// Test parameters
	const (
		numConcurrent = 5
		numRequests   = 3 // requests per goroutine
		maxDuration   = 30 * time.Second
	)

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		latencies    []time.Duration
		totalTokens  int32
		successCount int
		rateLimit    = time.NewTicker(500 * time.Millisecond) // 2 requests per second
		testStart    = time.Now()
		errors       = make(chan error, numConcurrent*numRequests)
	)
	defer rateLimit.Stop()

	// Launch concurrent workers
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numRequests; j++ {
				<-rateLimit.C // Rate limiting

				start := time.Now()
				stream, err := ts.client.InvokeStream(context.Background(), &pb.LLMRequest{
					Provider: "openrouter",
					Model:    "google/gemini-flash-1.5-8b",
					Messages: []*pb.ChatMessage{
						{
							Role:    "user",
							Content: fmt.Sprintf("Write a one-line story about worker %d request %d.", workerID, j),
						},
					},
				})

				if err != nil {
					errors <- fmt.Errorf("worker %d request %d setup failed: %w", workerID, j, err)
					continue
				}

				var gotContent bool
				var usage *pb.UsageInfo
				for {
					resp, err := stream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						errors <- fmt.Errorf("worker %d request %d stream failed: %w", workerID, j, err)
						break
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
			}
		}(i)
	}

	// Wait for completion or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed normally
	case <-time.After(maxDuration):
		t.Fatal("Test timed out")
	}

	// Check for errors
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	// Calculate statistics
	totalDuration := time.Since(testStart)
	mu.Lock()
	var totalLatency time.Duration
	maxLatency := time.Duration(0)
	minLatency := time.Duration(1<<63 - 1)
	for _, lat := range latencies {
		totalLatency += lat
		if lat > maxLatency {
			maxLatency = lat
		}
		if lat < minLatency {
			minLatency = lat
		}
	}
	avgLatency := totalLatency / time.Duration(len(latencies))
	mu.Unlock()

	// Log performance metrics
	t.Logf("Test completed in: %v", totalDuration)
	t.Logf("Successful requests: %d/%d", successCount, numConcurrent*numRequests)
	t.Logf("Average latency: %v", avgLatency)
	t.Logf("Min latency: %v", minLatency)
	t.Logf("Max latency: %v", maxLatency)
	t.Logf("Total tokens processed: %d", totalTokens)
	t.Logf("Token throughput: %.2f tokens/second", float64(totalTokens)/totalDuration.Seconds())
	t.Logf("Request throughput: %.2f requests/second", float64(successCount)/totalDuration.Seconds())

	// Verify performance requirements
	require.Greater(t, successCount, 0, "No successful requests")
	require.Less(t, avgLatency, 5*time.Second, "Average latency too high")
	require.Greater(t, float64(successCount)/totalDuration.Seconds(), 1.0,
		"Throughput below 1 request per second")
}
