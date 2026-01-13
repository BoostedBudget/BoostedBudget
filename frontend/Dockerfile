# Multi-stage production Dockerfile for Frontend
# Stage 1: Builder
FROM oven/bun:1 AS builder

WORKDIR /build

# Copy package files
COPY frontend/package.json frontend/bun.lock ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy config directory (needed for type generation)
COPY config/ /config/

# Copy frontend source code
COPY frontend/ .

# Build the application
RUN bun run build

# Stage 2: Runtime with Nginx (Debian-based)
FROM nginx:stable

# Install runtime dependencies
RUN for i in 1 2 3; do \
      apt-get update && apt-get install -y --no-install-recommends curl && \
      rm -rf /var/lib/apt/lists/* && break || \
      { [ $i -lt 3 ] && sleep 5; }; \
    done

# Copy custom nginx configuration
COPY frontend/nginx.conf /etc/nginx/nginx.conf

# Copy built assets from builder
COPY --from=builder /build/dist /usr/share/nginx/html

# Copy runtime config injection script
COPY frontend/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/ || exit 1

# Expose ports (HTTP and HTTPS)
EXPOSE 80 443

# Use entrypoint script for runtime config injection
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
