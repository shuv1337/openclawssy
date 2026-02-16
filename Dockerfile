# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN make build

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates and openssl for secret management
RUN apk add --no-cache ca-certificates openssl

# Copy binary from builder
COPY --from=builder /app/bin/openclawssy /usr/local/bin/openclawssy

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Create workspace directory
RUN mkdir -p /app/workspace

# Expose the default port
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# Default command (can be overridden)
CMD ["serve", "--token", "change-me", "--addr", "0.0.0.0:8080"]
