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
  - [x] `message LLMRequest` (fields: `provider`, `model`, `messages[]`, tuning params, `enable_stream`, `enable_cache`)
  - [x] `message ChatMessage` (fields: `role`, `content`, `cache_control`)
  - [x] `message CacheControl` (fields: `type`, e.g. `"ephemeral"`)
  - [x] `message LLMResponse` (final output)
  - [x] `message LLMStreamResponse` (stream chunks, `is_final`)
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

---

## **Domain Layer (Optional)**
*(Only if you plan to store usage logs or specialized domain logic. Otherwise, skip.)*

- [ ] Define domain objects (e.g., `LLMCall` for logging calls or usage metrics)
- [ ] Add validation methods (if you have custom domain constraints)
- [ ] Create domain-specific errors
- [ ] Write unit tests for domain objects
- [ ] Verify domain test coverage

---

## **Database (Optional)**
*(Only if you need to persist logs, usage, or any metadata. Otherwise, skip.)*

- [ ] Create migrations for `llm_calls` table
  - [ ] Define columns: `id`, `provider`, `model`, `prompt`, `response`, `tokens_used`, etc.
- [ ] Define indexes if you need fast searching or analytics
- [ ] Setup migration runner (Flyway, golang-migrate, or similar)
- [ ] Verify rollback handling
- [ ] Docker Compose or Kubernetes config for migrations

---

## **Repository Layer (Optional)**
*(Only if storing logs/usage. Otherwise, skip.)*

- [ ] Implement a PostgreSQL or other DB repository:
  - [ ] Database connection handling
  - [ ] CRUD or insertion methods (`InsertLLMCall`, etc.)
  - [ ] Transaction support if needed
- [ ] Integration tests with testcontainers:
  - [ ] Setup test DB in Docker
  - [ ] Insert/fetch logs
  - [ ] Check performance with large logs

---

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
- [x] Ignore `cache_control` if not used by OpenRouter
- [x] Write unit tests with mock HTTP

### **Anthropic Provider**
- [x] Implement `Invoke()`:
  - [x] Create JSON structure with `system[]` and `messages[]`
  - [x] Attach `"cache_control": {"type":"ephemeral"}` if `ChatMessage.cache_control.type=="ephemeral"`
  - [x] Set `"x-api-key": "$ANTHROPIC_API_KEY"` and `"anthropic-version"`
  - [x] Parse completion and usage fields
- [ ] Implement `InvokeStream()` (SSE-based):
  - [ ] If `enable_stream` is true, add `"stream": true`
  - [ ] Parse SSE for partial tokens
  - [ ] Map each chunk to `LLMStreamResponse`
- [x] Ephemeral caching logic:
  - [x] For large blocks with `cache_control`, forward unchanged text to get a cache hit
  - [x] Check usage fields for `cache_creation_input_tokens` or `cache_read_input_tokens`
- [x] Unit tests with mocked Anthropic SSE

### **OpenAI Provider**
- [ ] Implement `Invoke()`:
  - [ ] `POST https://api.openai.com/v1/chat/completions`
  - [ ] Map `messages` to OpenAI's format
  - [ ] Use `"Authorization": "Bearer $OPENAI_API_KEY"`
  - [ ] Parse `choices[0].message.content`
- [ ] Implement `InvokeStream()`:
  - [ ] Set `"stream": true`
  - [ ] Parse SSE or chunked data from OpenAI
  - [ ] Convert to `LLMStreamResponse`
- [ ] Ignore `cache_control` (not supported)
- [ ] Unit tests with mock HTTP/OpenAI responses

### **Gemini Provider**
- [ ] Implement using `github.com/google/generative-ai-go/genai`
- [ ] `Invoke()` calls `SendMessage()`
  - [ ] Map `temperature`, `top_k`, `top_p`, etc.
  - [ ] Parse final content
- [ ] Check if streaming is supported (if not, you might only do unary)
- [ ] Ignore `cache_control`
- [ ] Unit tests (mock the Gemini client or calls)

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
- [ ] `LLMServer` struct with references to each `LLMProvider` 
- [ ] `Invoke(ctx context.Context, req *LLMRequest)`:
  - [ ] Switch on `req.Provider`, call the correct provider's `Invoke()`
  - [ ] Return or handle error if unknown provider
- [ ] `InvokeStream(req *LLMRequest, stream LLMService_InvokeStreamServer)`:
  - [ ] Switch on `req.Provider`, call `InvokeStream()`
  - [ ] Receive chunk channel, do `stream.Send(chunk)` for each
  - [ ] On final chunk or error, terminate
- [ ] Error handling & structured logging

### **Server Entry Point**
- [ ] Confirm `main.go` registers `LLMServiceServer`
- [ ] Confirm reflection is enabled (if desired)
- [ ] Health check implementation (optional)

---

## **Deployment & Configuration**

### **Docker & Docker Compose**
- [ ] Create a multi-stage **Dockerfile**:
  - [ ] Build stage with Go 1.23.4
  - [ ] Final minimal stage
- [ ] Optional `docker-compose.yaml`:
  - [ ] Define `llmservice` container
  - [ ] Possibly define a local Postgres for usage logs
  - [ ] Check health checks
- [ ] Verify container runs and gRPC is accessible

### **Environment Configuration**
- [ ] Create a config struct for timeouts, concurrency, default models
- [ ] Load from env or config file
- [ ] Validate mandatory keys at startup
- [ ] Add logging for missing optional keys

### **Logging Setup**
- [ ] Integrate structured logger (Zap or Logrus)
- [ ] Configure log levels via env
- [ ] Log request/response metadata (provider, model, etc.)
- [ ] Add correlation IDs or request IDs if needed
- [ ] Check error logs for stack traces

### **Metrics & Observability**
- [ ] Add Prometheus metrics (requests per provider, success/fail counts)
- [ ] Track streaming concurrency
- [ ] Potential distributed tracing (OpenTelemetry)
- [ ] gRPC reflection for debugging

### **CI/CD Pipeline**
- [ ] GitHub Actions or equivalent:
  - [ ] Build step
  - [ ] Run unit tests
  - [ ] Run integration tests
  - [ ] Publish Docker image
  - [ ] Deploy to staging environment

---

## **Testing Infrastructure**

### **Unit Tests**
- [ ] Provider-level mocks (OpenRouter, Anthropic, OpenAI, Gemini)
- [ ] Test `Invoke()` with valid/invalid prompts
- [ ] Test `Invoke()` with missing env keys
- [ ] Test error handling paths
- [ ] High coverage for provider logic

### **Integration Tests**
- [ ] Spin up the gRPC server via `main.go`
- [ ] Make real gRPC calls to `Invoke` or `InvokeStream`
- [ ] Optionally call external providers with small test prompts (watch cost)
- [ ] If using DB, test DB logs insertion/retrieval
- [ ] Concurrency checks (multiple calls at once)

### **E2E Tests**  
*(Nothing is implemented yet; these are your final thorough scenarios.)*

1. **Basic Single Call**
   - [ ] Setup: Start the gRPC server
   - [ ] Execution: `Invoke` with a small prompt on `openrouter`
   - [ ] Verification: Confirm a valid `LLMResponse.content` is returned

2. **Simple Streamed Call**
   - [ ] Setup: `InvokeStream` with a short prompt on `openai`
   - [ ] Execution: Read partial tokens until the final chunk
   - [ ] Verification: Ensure correct ordering of chunks; `is_final == true` at end

3. **Anthropic Ephemeral Caching – Single Block**
   - [ ] Setup: Create a large system message with `cache_control.type = "ephemeral"`
   - [ ] Execution 1: Call `Invoke` with that block
   - [ ] Execution 2: Repeat the exact block within 5 minutes
   - [ ] Verification: Confirm usage or logs show a cache hit (faster or cheaper)

4. **Anthropic Ephemeral Caching – Multiple Blocks**
   - [ ] Setup: Mark multiple `ChatMessage`s with ephemeral cache
   - [ ] Execution: Re-send them identically
   - [ ] Verification: Confirm each ephemeral block is recognized; partial changes cause new caches

5. **Parallel Streaming**
   - [ ] Setup: Send `InvokeStream` to Anthropic or OpenAI from multiple goroutines
   - [ ] Execution: Monitor concurrency
   - [ ] Verification: Ensure no cross-data contamination; all partial streams deliver correct content

6. **Invalid Provider / Model**
   - [ ] Setup: `LLMRequest.provider = "unrecognized"`
   - [ ] Execution: `Invoke` or `InvokeStream`
   - [ ] Verification: Expect a clear error message in gRPC status

7. **Large Prompt Handling**
   - [ ] Setup: ~1MB prompt in `messages[]`
   - [ ] Execution: Attempt `InvokeStream` with large content
   - [ ] Verification: No OOM or timeouts; partial chunks still flow

8. **Missing or Invalid API Key**
   - [ ] Setup: Purposely omit `OPENAI_API_KEY`
   - [ ] Execution: `Invoke` for `openai`
   - [ ] Verification: Service returns an error, logs indicate missing key

9. **Concurrency & Rate Limiting** (If implemented)
   - [ ] Setup: Simulate 50 concurrent calls to each provider
   - [ ] Execution: Check if service enforces rate limits or queues
   - [ ] Verification: No meltdown or crashes; correct error if limit is exceeded

10. **Performance Load Test** (Optional)
    - [ ] Setup: Use a load testing tool (Locust, Vegeta)
    - [ ] Execution: ~100 RPS over 2 minutes
    - [ ] Verification: Service maintains stable latency; no memory leaks

11. **Security & Auth** (If required)
    - [ ] Setup: gRPC with token/mTLS
    - [ ] Execution: Attempt calls with invalid token
    - [ ] Verification: 401 or 403 error

12. **E2E Edge Cases**
    - [ ] SSE interruption mid-stream
    - [ ] Retries on network errors (if implemented)
    - [ ] Cache expiration after 5 minutes for Anthropic ephemeral blocks

---

## **Next Steps**
1. **Implement** each provider logic (OpenRouter, Anthropic, OpenAI, Gemini).  
2. **Finish** SSE streaming and ephemeral caching support in `AnthropicProvider`.  
3. **Complete** the E2E test suite (items 1–12 above).  
4. **Set up** Docker + CI/CD pipeline for automated builds & tests.  
5. **Add** metrics, logs, tracing for production observability.

---

**Use this checklist as your central "to-do" board**, marking `[x]` next to tasks as you complete them. By following each step, you'll ensure the LLM Service meets the core requirements (multiple providers, gRPC streaming, ephemeral caching) and is robustly tested at all levels (unit, integration, E2E).