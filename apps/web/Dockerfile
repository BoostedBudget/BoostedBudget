# Multi-stage production Dockerfile for Frontend (Turborepo monorepo)
# Stage 1: Builder - Use Node.js Alpine (QEMU compatible, no AVX required)
# Note: Bun crashes under QEMU x86 emulation due to AVX instruction requirements
# #1514: digest-pinned to node:22-alpine (multi-arch manifest list digest, resolved from
# Docker Hub on 2026-08-12). Re-resolve this digest if the node major/tag is bumped.
FROM node:22-alpine@sha256:b6f26b36c8ff49624cfdac716b8ea1138d606df02586a77d364bb5536a634f85 AS builder

WORKDIR /build

# Copy package files
COPY package.json ./
COPY apps/web/package.json ./apps/web/
COPY packages/api-client/package.json ./packages/api-client/
COPY packages/design-tokens/package.json ./packages/design-tokens/
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

# #1514 (C2): vite.config.ts uses sourcemap: 'hidden' (G6), so dist/**/*.map is always
# produced but never referenced by the built .js — the same maps prod/staging upload to
# Sentry then delete before deploy (see deploy-production.yml's "Delete source maps
# before deploy" step). This image has no separate delete-before-deploy step of its own
# (deploy-qa.yml builds straight from this Dockerfile and COPYs the built dist into
# nginx), so unlike prod/staging, the deletion must happen in the image build itself or
# every consumer of this image ships the maps. Fails the build loudly if any survive,
# mirroring the same self-verifying check.
RUN find dist -type f -name '*.map' -delete && \
    remaining=$(find dist -type f -name '*.map' | wc -l | tr -d ' ') && \
    if [ "$remaining" -ne 0 ]; then \
      echo "ERROR: ${remaining} .map file(s) still present in dist/ after deletion." >&2; \
      exit 1; \
    fi && \
    echo "Confirmed: no .map files remain in dist/."

# Stage 2: Runtime with Nginx (Debian-based)
FROM nginx:stable@sha256:46ccc48fbb1f5a43167f2ee2c279c122b96eec5d976e7f4e1e0780f59a51b4d6

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
