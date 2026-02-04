# Multi-stage production Dockerfile for Backend
# Stage 1: Builder
FROM golang:1.25.6-alpine AS builder

# Install build dependencies
RUN echo "📦 Installing build dependencies..." && \
    for i in 1 2 3; do \
      apk add --no-cache git ca-certificates tzdata && break || \
      { echo "⚠ apk add attempt $i/3 failed"; [ $i -lt 3 ] && sleep 5; }; \
    done

WORKDIR /build

# Copy go mod files and download dependencies
COPY backend/go.mod backend/go.sum ./
RUN echo "📦 Downloading Go modules..." && \
    for i in 1 2 3; do \
      go mod download && echo "✓ Modules downloaded" && break || \
      { echo "⚠ Download attempt $i/3 failed"; [ $i -lt 3 ] && sleep 10; }; \
    done

# Copy config directory (needed for runtime)
COPY config/ ./config/

# Copy backend source code
COPY backend/ .

# Build the application
# CGO_ENABLED=0 for static binary
# -ldflags="-w -s" strips debug info for smaller binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o server \
    ./cmd/server

# Stage 2: Runtime (Debian Bookworm Slim - Minimal but stable)
FROM debian:bookworm-slim@sha256:98f4b71de414932439ac6ac690d7060df1f27161073c5036a7553723881bffbe

# Install runtime dependencies
RUN echo "📦 Installing runtime dependencies..." && \
    for i in 1 2 3; do \
      echo "Attempt $i/3..." && \
      apt-get update && \
      apt-get upgrade -y --only-upgrade libssl3 openssl && \
      apt-get install -y --no-install-recommends \
        ca-certificates curl tzdata postgresql-client && \
      rm -rf /var/lib/apt/lists/* && \
      echo "✓ Dependencies installed" && break || \
      { echo "⚠ apt-get attempt $i/3 failed"; \
        [ $i -lt 3 ] && sleep 5; }; \
    done

# Install golang-migrate for database migrations
# TARGETARCH is automatically set by Docker Buildx (amd64 or arm64)
ARG TARGETARCH
RUN echo "📦 Downloading golang-migrate for ${TARGETARCH}..." && \
    for i in 1 2 3 4 5; do \
      echo "Attempt $i/5..." && \
      curl -sSfL --retry 3 --retry-delay 5 --connect-timeout 10 \
        "https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-${TARGETARCH}.tar.gz" \
        | tar -xz -C /usr/local/bin && \
      chmod +x /usr/local/bin/migrate && \
      echo "✓ golang-migrate downloaded successfully" && break || \
      { echo "⚠ Download attempt $i/5 failed"; \
        [ $i -lt 5 ] && sleep 10; }; \
    done && \
    test -x /usr/local/bin/migrate || { echo "✗ golang-migrate installation failed"; exit 1; }

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/server /app/server

# Copy migrations directory (needed at runtime for golang-migrate)
COPY --from=builder /build/migrations /app/migrations

# Copy config directory (subscription tiers, etc.)
COPY --from=builder /build/config /app/config

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
