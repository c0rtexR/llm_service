# Performance Testing Guide

## Overview

This guide describes how to run and interpret performance tests for our LLM service providers. Currently, we have detailed testing procedures for OpenRouter and Gemini providers.

## Prerequisites

- Go 1.23.4 or later
- Required API keys:
  - `OPENROUTER_API_KEY` for OpenRouter tests
  - `GEMINI_API_KEY` for Gemini tests

## Running Tests

### Basic Test Execution

```bash
# Run all performance tests
go test -v ./tests/e2e/...

# Run specific provider tests
go test -v -run Gemini ./tests/e2e/...
go test -v -run OpenRouter ./tests/e2e/...
```

### Test Categories

Each provider has three categories of performance tests:

1. **Latency Tests** (`Test<Provider>Latency`)
   - Tests response time for different prompt lengths
   - Verifies latency is within acceptable bounds
   ```bash
   go test -v -run TestGeminiLatency ./tests/e2e/...
   ```

2. **Throughput Tests** (`Test<Provider>Throughput`)
   - Measures requests/second and tokens/second
   - Verifies sustained throughput capabilities
   ```bash
   go test -v -run TestGeminiThroughput ./tests/e2e/...
   ```

3. **Concurrent Load Tests** (`Test<Provider>ConcurrentLoad`)
   - Tests behavior under parallel requests
   - Measures success rate and average latency
   ```bash
   go test -v -run TestGeminiConcurrentLoad ./tests/e2e/...
   ```

## Expected Performance

### Gemini Provider

1. **Latency Expectations**
   - Short prompts: <2s
   - Medium prompts: <8s
   - Long prompts: <10s

2. **Throughput Expectations**
   - Sustained: 0.5 requests/second
   - Peak: Up to 1 request/second
   - Note: Rate limits at >0.5 req/s

3. **Concurrent Load Expectations**
   - Maximum concurrent requests: 2
   - Minimum throughput: 0.1 req/s
   - Maximum average latency: 10s

### OpenRouter Provider

1. **Latency Expectations**
   - Short prompts: <3s
   - Medium prompts: <8s
   - Long prompts: <10s

2. **Throughput Expectations**
   - Sustained: 0.9 requests/second
   - Token throughput: ~35 tokens/second
   - Note: Rate limits vary by model

3. **Concurrent Load Expectations**
   - Maximum concurrent requests: 3
   - Minimum throughput: 0.3 req/s
   - Maximum average latency: 10s

## Interpreting Results

### Success Criteria

1. **Latency Tests**
   - All response times within specified bounds
   - No timeout errors
   - Content quality maintained

2. **Throughput Tests**
   - Achieved minimum throughput targets
   - Stable response times
   - No cascading failures

3. **Concurrent Load Tests**
   - Minimum success rate achieved
   - Average latency within bounds
   - Graceful handling of rate limits

### Common Issues

1. **Rate Limiting (429 Errors)**
   - Reduce concurrent requests
   - Increase delay between requests
   - Check if hitting API quotas

2. **High Latency**
   - Check network conditions
   - Monitor system resources
   - Verify prompt lengths

3. **Failed Requests**
   - Check error messages
   - Verify API keys
   - Review retry mechanism

## Monitoring Test Execution

### Test Output Format

```
=== RUN   Test<Provider>Latency
    test_file.go:XX: Latency for short_prompt: 890.481928ms
    test_file.go:XX: Latency for medium_prompt: 4.499651492s
    test_file.go:XX: Latency for long_prompt: 6.058040872s
--- PASS: Test<Provider>Latency (11.45s)
```

### Key Metrics to Watch

1. **Response Times**
   - Individual request latencies
   - Average latency under load
   - Maximum observed latency

2. **Success Rates**
   - Number of successful requests
   - Error distribution
   - Retry attempts

3. **Resource Usage**
   - Token consumption
   - Request throughput
   - Error rates

## Troubleshooting

### Rate Limit Issues
1. Verify API key quotas
2. Check rate limit headers
3. Adjust concurrent request parameters
4. Implement exponential backoff

### Performance Issues
1. Monitor system resources
2. Check network latency
3. Review request payload sizes
4. Analyze response times

### Test Failures
1. Check error messages
2. Verify test configuration
3. Review API documentation
4. Adjust test parameters

## Best Practices

1. **Test Preparation**
   - Clean test environment
   - Valid API keys
   - Sufficient system resources

2. **Test Execution**
   - Run tests in isolation
   - Monitor system metrics
   - Log detailed results

3. **Results Analysis**
   - Compare with baselines
   - Look for patterns
   - Document anomalies

## Future Improvements

1. **Test Coverage**
   - Add more providers
   - Test edge cases
   - Stress testing

2. **Monitoring**
   - Real-time metrics
   - Performance dashboards
   - Automated alerts

3. **Automation**
   - CI/CD integration
   - Scheduled testing
   - Automated reporting 