# Multi-stage production Dockerfile for Backend
# Stage 1: Builder
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files and download dependencies
COPY backend/go.mod backend/go.sum ./
RUN go mod download

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
FROM debian:bookworm-slim

# Install runtime dependencies including Atlas
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Install Atlas for database migrations
# Using -latest to get the most recent stable version
# TARGETARCH is automatically set by Docker Buildx (amd64 or arm64)
ARG TARGETARCH
RUN curl -sSfL "https://release.ariga.io/atlas/atlas-linux-${TARGETARCH}-latest" -o /usr/local/bin/atlas && \
    chmod +x /usr/local/bin/atlas

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/server /app/server

# Copy migrations directory and Atlas config (needed at runtime)
COPY --from=builder /build/migrations /app/migrations
COPY --from=builder /build/atlas.hcl /app/atlas.hcl

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
