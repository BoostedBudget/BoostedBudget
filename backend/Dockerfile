# Multi-stage production Dockerfile for Backend
# Stage 1: Builder
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static binary
# -ldflags="-w -s" strips debug info for smaller binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o server \
    ./cmd/server

# Stage 2: Runtime (Debian Bookworm Slim - Minimal but stable)
FROM debian:bookworm-slim

# Install runtime dependencies including golang-migrate
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Install golang-migrate
RUN MIGRATE_VERSION=v4.18.1 && \
    MIGRATE_ARCH=linux-amd64 && \
    curl -L "https://github.com/golang-migrate/migrate/releases/download/${MIGRATE_VERSION}/migrate.${MIGRATE_ARCH}.tar.gz" \
    | tar xvz -C /usr/local/bin && \
    chmod +x /usr/local/bin/migrate

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/server /app/server

# Copy migrations directory (if needed at runtime)
COPY --from=builder /build/migrations /app/migrations

# Copy startup script
COPY --from=builder /build/scripts/docker-entrypoint.sh /app/docker-entrypoint.sh

# Create non-root user for security
# Note: We need curl access for the startup script, so install it before switching users
RUN chmod +x /app/docker-entrypoint.sh && \
    groupadd -g 1000 appuser && \
    useradd -r -u 1000 -g appuser appuser && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application via startup script (handles migrations automatically)
CMD ["/app/docker-entrypoint.sh"]
