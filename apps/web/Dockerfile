# Multi-stage production Dockerfile for Frontend (Turborepo monorepo)
# Stage 1: Builder - Use Node.js Alpine (QEMU compatible, no AVX required)
# Note: Bun crashes under QEMU x86 emulation due to AVX instruction requirements
FROM node:22-alpine AS builder

WORKDIR /build

# Copy package files
COPY package.json ./
COPY apps/web/package.json ./apps/web/
COPY packages/api-client/package.json ./packages/api-client/
COPY packages/shared-types/package.json ./packages/shared-types/

# Remove workspace:* dependencies from apps/web/package.json
# (npm doesn't support workspace: protocol, Vite resolve.alias handles these imports)
RUN sed -i '/@boostedbudget/d' apps/web/package.json

# Install dependencies with npm (doesn't enforce packageManager field)
RUN npm install --legacy-peer-deps

# Copy workspace packages
COPY packages/ ./packages/

# Copy config directory (needed for type generation)
COPY config/ /config/

# Copy root tsconfig.json (apps/web/tsconfig.json extends it)
COPY tsconfig.json ./

# Copy web app source
COPY apps/web/ ./apps/web/

# Build the web application
WORKDIR /build/apps/web
RUN npm run build

# Stage 2: Runtime with Nginx (Debian-based)
FROM nginx:stable@sha256:d9911e5ac94c1905ffd34a582d01430224aed6954cb4a788b7877114bf0c1cb1

# Install runtime dependencies
RUN for i in 1 2 3; do \
      apt-get update && \
      apt-get upgrade -y --only-upgrade libssl3 openssl && \
      apt-get install -y --no-install-recommends curl && \
      rm -rf /var/lib/apt/lists/* && break || \
      { [ $i -lt 3 ] && sleep 5; }; \
    done

# Copy custom nginx configuration
COPY apps/web/nginx.conf /etc/nginx/nginx.conf

# Copy built assets from builder
COPY --from=builder /build/apps/web/dist /usr/share/nginx/html

# Copy runtime config injection script
COPY apps/web/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost/ || exit 1

# Expose ports (HTTP and HTTPS)
EXPOSE 80 443

# Use entrypoint script for runtime config injection
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
