# Build stage
FROM golang:1.23.4-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o llm-service ./cmd/llmservice

# Final stage
FROM alpine:3.19

# Install CA certificates and grpc-health-probe
RUN apk add --no-cache ca-certificates curl && \
    GRPC_HEALTH_PROBE_VERSION=v0.4.25 && \
    curl -L -o /bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/llm-service .

# Use non-root user
USER appuser

# Expose gRPC port
EXPOSE 50051

# Set entrypoint
ENTRYPOINT ["/app/llm-service"] 