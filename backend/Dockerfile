# Multi-stage production Dockerfile for Backend
# Stage 1: Builder
# #1514: digest-pinned to golang:1.26.6-alpine (multi-arch manifest list digest, resolved
# from Docker Hub on 2026-08-13). If GO_VERSION is bumped, this digest must be re-resolved
# for the new tag or the pin will silently pull mismatched content — the digest wins over the
# tag, so bumping the version alone is a silent no-op that keeps building the old toolchain.
# Resolve with: docker buildx imagetools inspect golang:<version>-alpine  (take the top-level
# index Digest, not a per-platform manifest digest).
#
# Bumped 1.26.5 -> 1.26.6 to clear seven Go standard-library advisories reported by
# govulncheck: GO-2026-6218 (net/url), GO-2026-6091 (html/template), GO-2026-6090
# (crypto/tls), GO-2026-6089 (net/http), GO-2026-6088 (encoding/xml), GO-2026-5972
# (encoding/asn1) and GO-2026-5026 (net/http via x/net/idna). All are fixed in go1.26.6.
ARG GO_VERSION=1.26.6
FROM golang:${GO_VERSION}-alpine@sha256:af8d6740070b8906d12eae1c3e3ea0957fb63f492051ea05e354c38ef9fe88df AS builder

# Install build dependencies
RUN echo "Installing build dependencies..." && \
    echo "Using Go version: $(go version)" && \
    for i in 1 2 3; do \
      apk add --no-cache git ca-certificates tzdata && break || \
      { echo "apk add attempt $i/3 failed"; [ $i -lt 3 ] && sleep 5; }; \
    done

WORKDIR /build

# Copy go mod files and download dependencies
COPY backend/go.mod backend/go.sum ./
RUN echo "Downloading Go modules..." && \
    for i in 1 2 3; do \
      go mod download && echo "Modules downloaded" && break || \
      { echo "Download attempt $i/3 failed"; [ $i -lt 3 ] && sleep 10; }; \
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

# Build golang-migrate from source using this stage's pinned Go toolchain (see GO_VERSION
# and the digest-pinned builder FROM above), rather than downloading a prebuilt release binary.
# #1514: pinned to v4.19.1 (latest release as of 2026-08-12) instead of floating @latest,
# so the version baked into the production image is reproducible and reviewable.
# Integrity comes from Go's module checksum database, which authenticates the module
# source by default for public modules.
#
# What this does NOT provide: building from source cannot "ensure no CVEs" — an earlier
# version of this comment claimed exactly that, and also named a Go version (1.25.7) that
# had already drifted from the toolchain actually in use. Vulnerabilities may still exist in
# migrate v4.19.1 itself, in its transitive dependencies, or in the pinned toolchain. What it
# does buy is a reproducible, reviewable input and no dependence on a third-party release
# binary. Keep this claim narrow; do not restore a "no CVEs" guarantee.
RUN CGO_ENABLED=0 GOOS=linux go install \
    -tags 'postgres' \
    -ldflags="-w -s" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

# Stage 2: Runtime (Debian Bookworm Slim - Minimal but stable)
FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251

# Install runtime dependencies
RUN echo "Installing runtime dependencies..." && \
    for i in 1 2 3; do \
      echo "Attempt $i/3..." && \
      apt-get update && \
      apt-get upgrade -y --only-upgrade libssl3 openssl && \
      apt-get install -y --no-install-recommends \
        ca-certificates curl tzdata postgresql-client && \
      rm -rf /var/lib/apt/lists/* && \
      echo "Dependencies installed" && break || \
      { echo "apt-get attempt $i/3 failed"; \
        [ $i -lt 3 ] && sleep 5; }; \
    done

# Copy golang-migrate binary from builder stage (see toolchain provenance note above)
COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
RUN chmod +x /usr/local/bin/migrate && \
    migrate -version

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
