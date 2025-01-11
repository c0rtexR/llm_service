Below is a **comprehensive, very specific task list** for implementing the **LLM Service**. Nothing is completed yet—every item is unchecked (`[ ]`). The structure mirrors the style of your **Context Management Service** example, but adapted to **LLM** functionality, including **SSE streaming** and **Anthropic ephemeral caching**. It also includes **detailed E2E test scenarios** as a final section with their own tasks.

---

# LLM Service – Implementation Progress

## **Foundation Layer**

### **Project Structure** 
- [x] Initialize Go module (`go mod init llmservice`)
- [x] Create directory structure:
  - [x] `cmd/llmservice` (entry point)
  - [x] `internal/provider` (provider-specific logic)
  - [x] `internal/server` (gRPC server implementation)
  - [x] `internal/service` (business logic, caching, etc. if needed)
  - [x] `internal/repository` (optional DB logs)
  - [x] `proto` (Protobuf files)
  - [x] `tests` (integration, E2E)
- [x] Add essential dependencies:
  - [x] `google.golang.org/grpc`
  - [x] `github.com/golang/protobuf`
  - [x] Logging library (Zap)
  - [x] Test frameworks (`testify`)
- [x] Set up basic configuration approach (env vars, config struct)
- [x] Confirm Go version (e.g., 1.23.4) in `go.mod` and Dockerfile

### **Proto Definition**
- [x] Create `proto/llm_service.proto` with:
  - [x] `message LLMRequest` (fields: `provider`, `model`, `messages[]`, tuning params)
  - [x] `message ChatMessage` (fields: `role`, `content`)
  - [x] `message CacheControl` (fields: `use_cache`, `ttl`)
  - [x] `message LLMResponse` (final output)
  - [x] `message LLMStreamResponse` (stream chunks with type, content, finish_reason, usage)
  - [x] `service LLMService` with:
    - [x] `rpc Invoke(LLMRequest) returns (LLMResponse);`
    - [x] `rpc InvokeStream(LLMRequest) returns (stream LLMStreamResponse);`
- [x] Generate Go code (`protoc --go_out` and `--go-grpc_out`)
- [x] Confirm gRPC reflection, health checks, or other necessary proto options

### **Main Entry Point (`cmd/llmservice/main.go`)**
- [x] Load environment variables for each provider key:
  - [x] `OPENROUTER_API_KEY`
  - [x] `OPENAI_API_KEY`
  - [x] `ANTHROPIC_API_KEY`
  - [x] `GEMINI_API_KEY`
- [x] Create a `grpc.Server`
- [x] Register `LLMServiceServer` implementation
- [x] Listen on `:50051` (or config-based port)
- [x] Implement graceful shutdown (handle `SIGINT`, `SIGTERM`)

## **Provider Layer**

### **Common Provider Interface**
- [x] Define `LLMProvider` interface with:
  - [x] `Invoke(ctx context.Context, req *LLMRequest) (*LLMResponse, error)`
  - [x] `InvokeStream(ctx context.Context, req *LLMRequest) (<-chan *LLMStreamResponse, <-chan error)`
- [x] Create provider configuration structure
- [x] Add tests for provider configuration

### **OpenRouter Provider**
- [x] Implement `Invoke()`:
  - [x] Build JSON payload (`model`, `messages`, etc.)
  - [x] Set `"Authorization": "Bearer $OPENROUTER_API_KEY"`
  - [x] Parse response for `content`
- [x] Implement `InvokeStream()`:
  - [x] Send `"stream": true` if needed
  - [x] Parse SSE line-by-line
  - [x] Convert partial messages to `LLMStreamResponse`
- [x] Handle optional fields (temperature, top_p, etc.) if supported
- [x] Write unit tests with mock HTTP

### **Anthropic Provider**
- [x] Implement `Invoke()`:
  - [x] Create JSON structure with `system[]` and `messages[]`
  - [x] Set `"x-api-key": "$ANTHROPIC_API_KEY"` and `"anthropic-version"`
  - [x] Parse completion and usage fields
- [x] Implement `InvokeStream()` (SSE-based):
  - [x] Add `"stream": true`
  - [x] Parse SSE for partial tokens
  - [x] Map each chunk to `LLMStreamResponse`
- [x] Write unit tests with mocked Anthropic SSE

### **OpenAI Provider**
- [x] Implement `Invoke()`:
  - [x] `POST https://api.openai.com/v1/chat/completions`
  - [x] Map `messages` to OpenAI's format
  - [x] Use `"Authorization": "Bearer $OPENAI_API_KEY"`
  - [x] Parse `choices[0].message.content`
- [x] Implement `InvokeStream()`:
  - [x] Set `"stream": true`
  - [x] Parse SSE or chunked data from OpenAI
  - [x] Convert to `LLMStreamResponse`
- [x] Write unit tests with mock HTTP/OpenAI responses

### **Gemini Provider**
- [x] Implement using `github.com/google/generative-ai-go/genai`
- [x] `Invoke()` calls `SendMessage()`
  - [x] Map `temperature`, `top_p`, etc.
  - [x] Parse final content
- [x] Implement streaming support
- [x] Write unit tests (mock the Gemini client or calls)

---

## **Service Layer**

*(Optional if the server logic is simple. Otherwise, place caching, concurrency, or rate-limiting here.)*

- [ ] Implement optional business logic, e.g.:
  - [ ] Local caching for repeated identical prompts
  - [ ] Rate limiting or concurrency checks
  - [ ] Logging usage to a repository
- [ ] Mock service in unit tests to confirm logic

---

## **API Layer (gRPC Server)**

### **Server Implementation**
- [x] `LLMServer` struct with references to each `LLMProvider` 
- [x] `Invoke(ctx context.Context, req *LLMRequest)`:
  - [x] Switch on `req.Provider`, call the correct provider's `Invoke()`
  - [x] Return or handle error if unknown provider
- [x] `InvokeStream(req *LLMRequest, stream LLMService_InvokeStreamServer)`:
  - [x] Switch on `req.Provider`, call `InvokeStream()`
  - [x] Receive chunk channel, do `stream.Send(chunk)` for each
  - [x] On final chunk or error, terminate
- [x] Error handling & structured logging

### **Server Entry Point**
- [x] Confirm `main.go` registers `LLMServiceServer`
- [x] Confirm reflection is enabled (if desired)
- [x] Health check implementation (optional)

---

## **Deployment & Configuration**

### **Docker & Docker Compose**
- [x] Create a multi-stage **Dockerfile**:
  - [x] Build stage with Go 1.23.4
  - [x] Final minimal stage
- [x] Optional `docker-compose.yaml`:
  - [x] Define `llmservice` container
  - [x] Possibly define a local Postgres for usage logs
  - [x] Check health checks
- [x] Verify container runs and gRPC is accessible

### **Environment Configuration**
- [x] Create a config struct for timeouts, concurrency, default models
- [x] Load from env or config file
- [x] Validate mandatory keys at startup
- [x] Add logging for missing optional keys

### **Logging Setup**
- [x] Integrate structured logger (Zap or Logrus)
- [x] Configure log levels via env
- [x] Log request/response metadata (provider, model, etc.)
- [x] Add correlation IDs or request IDs if needed
- [x] Check error logs for stack traces

### **Metrics & Observability**
- [x] Add Prometheus metrics (requests per provider, success/fail counts)
- [x] Track streaming concurrency
- [x] Potential distributed tracing (OpenTelemetry)
- [x] gRPC reflection for debugging

### **CI/CD Pipeline**
- [x] GitHub Actions or equivalent:
  - [x] Build step
  - [x] Run unit tests
  - [x] Run integration tests
  - [x] Publish Docker image
  - [x] Deploy to staging environment

---

## **Testing Infrastructure**

### **Unit Tests**
- [x] Provider-level mocks (OpenRouter, Anthropic, OpenAI, Gemini)
- [x] Test `Invoke()` with valid/invalid prompts
- [x] Test `Invoke()` with missing env keys
- [x] Test error handling paths
- [x] High coverage for provider logic

### **Integration Tests**
- [x] Spin up the gRPC server via `main.go`
- [x] Make real gRPC calls to `Invoke` or `InvokeStream`
- [x] Optionally call external providers with small test prompts (watch cost)
- [x] If using DB, test DB logs insertion/retrieval
- [x] Concurrency checks (multiple calls at once)

### **E2E Tests**  
*(Gemini, OpenRouter, and OpenAI providers implemented, Anthropic pending)*

1. **Basic Single Call**
   - [x] Setup: Start the gRPC server
   - [x] Execution: `Invoke` with a small prompt on Gemini/OpenRouter/OpenAI
   - [x] Verification: Confirm a valid response is returned

2. **Simple Streamed Call**
   - [x] Setup: `InvokeStream` with a short prompt
   - [x] Execution: Read partial tokens until the final chunk
   - [x] Verification: Ensure correct streaming behavior

3. **Chat History**
   - [x] Setup: Create a conversation with multiple messages
   - [x] Execution: Send messages and verify context retention
   - [x] Verification: Confirm chat history is maintained

4. **Parallel Streaming**
   - [x] Setup: Multiple concurrent streaming requests
   - [x] Execution: Monitor parallel streams
   - [x] Verification: All streams complete successfully

5. **Large Prompt Handling**
   - [x] Setup: Send large prompts
   - [x] Execution: Test with ~100KB of text
   - [x] Verification: Proper handling of large inputs

6. **Model Parameters**
   - [x] Setup: Test different model parameters
   - [x] Execution: Vary temperature, top_p, etc.
   - [x] Verification: Parameters are properly applied

7. **Error Handling**
   - [x] Setup: Test invalid configurations
   - [x] Execution: Send invalid model names
   - [x] Verification: Proper error responses 

8. **OpenAI Integration** (Completed)
   - [x] Setup: Configure OpenAI provider
   - [x] Basic and streaming tests
   - [x] Error handling verification
   - [x] Usage information tests

9. **Anthropic Integration** (Pending)
   - [ ] Setup: Configure Anthropic provider
   - [ ] Basic and streaming tests
   - [ ] Ephemeral caching tests
   - [ ] Error handling verification

10. **Performance Load Test** (Pending)
    - [ ] Setup: Use a load testing tool
    - [ ] Execution: ~100 RPS over 2 minutes
    - [ ] Verification: Service stability

11. **Security & Auth** (If required)
    - [ ] Setup: gRPC with token/mTLS
    - [ ] Execution: Attempt calls with invalid token
    - [ ] Verification: 401 or 403 error

---

## **Next Steps**
1. **Implement** remaining provider logic (OpenAI, Anthropic).  
2. **Finish** SSE streaming and ephemeral caching support in `AnthropicProvider`.  
3. **Complete** the E2E test suite (items 8–11 above).  
4. **Set up** Docker + CI/CD pipeline for automated builds & tests.  
5. **Add** metrics, logs, tracing for production observability.

---

**Use this checklist as your central "to-do" board**, marking `[x]` next to tasks as you complete them. By following each step, you'll ensure the LLM Service meets the core requirements (multiple providers, gRPC streaming, ephemeral caching) and is robustly tested at all levels (unit, integration, E2E).