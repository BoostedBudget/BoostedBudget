# Multi-stage production Dockerfile for Frontend
# Stage 1: Builder
FROM node:25-alpine AS builder

WORKDIR /build

# Copy package files
COPY frontend/package*.json ./

# Install ALL dependencies (including devDependencies for build)
RUN echo "📦 Installing production dependencies..." && \
    for i in 1 2 3 4; do \
      echo "npm ci attempt $i/4..." && \
      npm ci --prefer-offline --no-audit && \
      echo "✓ Dependencies installed" && break || \
      { echo "⚠ npm ci attempt $i/4 failed"; \
        npm cache clean --force; \
        [ $i -lt 4 ] && sleep 10; }; \
    done

# Copy config directory (needed for type generation)
COPY config/ /config/

# Copy frontend source code
COPY frontend/ .

# Build the application
RUN npm run build

# Stage 2: Runtime with Nginx (Debian-based)
FROM nginx:stable

# Install runtime dependencies
RUN echo "📦 Installing runtime dependencies..." && \
    for i in 1 2 3; do \
      echo "Attempt $i/3..." && \
      apt-get update && apt-get install -y --no-install-recommends curl && \
      rm -rf /var/lib/apt/lists/* && \
      echo "✓ Dependencies installed" && break || \
      { echo "⚠ apt-get attempt $i/3 failed"; \
        [ $i -lt 3 ] && sleep 5; }; \
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

# Expose port
EXPOSE 80

# Use entrypoint script for runtime config injection
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
