# BoostedBudget - Docker-First Fly.io Deployment Strategy

## Core Deployment Philosophy
Build once, deploy anywhere. Both frontend and backend are compiled into Docker images that can be deployed to Fly.io, AWS ECS, Google Cloud Run, or any container platform.

**Initial Deployment**: USA-only (us-east-1/iad region) for MVP
**Future Expansion**: Multi-region deployment path documented in "Future: Multi-Region Migration Plan" section

## Docker Build Strategy

### Backend Multi-Stage Dockerfile

```dockerfile
# backend/Dockerfile
# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with all optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=$(git describe --tags --always --dirty) -X main.BuildTime=$(date -u +%Y%m%d-%H%M%S)" \
    -a -installsuffix cgo \
    -o boostedbudget-api ./cmd/server

# Run tests as part of build
RUN go test -v ./...

# Stage 2: Create the minimal runtime image
FROM alpine:3.18

# Install necessary runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl postgresql-client

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/boostedbudget-api /app/
COPY --from=builder /build/migrations /app/migrations/

# Install migrate tool for database migrations
ADD https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz /tmp/
RUN tar -xzf /tmp/migrate.linux-amd64.tar.gz -C /usr/local/bin/ && \
    chmod +x /usr/local/bin/migrate && \
    rm /tmp/migrate.linux-amd64.tar.gz

# Create non-root user
RUN addgroup -g 1001 -S boostedbudget && \
    adduser -u 1001 -S boostedbudget -G boostedbudget
USER boostedbudget

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:${PORT:-8080}/health || exit 1

EXPOSE 8080

ENTRYPOINT ["/app/boostedbudget-api"]
```

### Frontend Multi-Stage Dockerfile

```dockerfile
# frontend/Dockerfile
# Stage 1: Build the React application
FROM node:18-alpine AS builder

WORKDIR /build

# Copy package files
COPY package*.json ./
RUN npm ci --only=production && \
    npm cache clean --force

# Copy all source files
COPY . .

# Build arguments for compile-time environment variables
ARG VITE_API_URL=https://api.boostedbudget.com/v1
ARG VITE_AUTH0_DOMAIN
ARG VITE_AUTH0_CLIENT_ID
ARG VITE_AUTH0_AUDIENCE
ARG VITE_GOOGLE_ADSENSE_CLIENT
ARG VITE_PLAID_PUBLIC_KEY

# Build the application
RUN npm run build

# Verify build output
RUN ls -la dist/

# Stage 2: Nginx server with built files
FROM nginx:alpine-slim

# Install runtime dependencies
RUN apk add --no-cache curl

# Copy custom nginx configuration
COPY nginx.conf /etc/nginx/nginx.conf

# Copy built application from builder
COPY --from=builder /build/dist /usr/share/nginx/html

# Create runtime environment injection script
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/health || exit 1

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
```

### Frontend Runtime Config Injection

```bash
#!/bin/sh
# frontend/docker-entrypoint.sh
# This script allows runtime environment variable injection

# Create runtime config
cat > /usr/share/nginx/html/config.js << EOF
window.__RUNTIME_CONFIG__ = {
  API_URL: "${API_URL:-https://api.boostedbudget.com/v1}",
  AUTH0_DOMAIN: "${AUTH0_DOMAIN}",
  AUTH0_CLIENT_ID: "${AUTH0_CLIENT_ID}",
  ENVIRONMENT: "${ENVIRONMENT:-production}"
};
EOF

# Start nginx
exec "$@"
```

## GitHub Actions CI/CD Pipeline

### Complete Build and Deploy Pipeline

```yaml
# .github/workflows/build-deploy.yml
name: Build and Deploy to Fly.io

on:
  push:
    branches: [main, staging, develop]
  pull_request:
    types: [opened, synchronize]

env:
  REGISTRY: ghcr.io
  BACKEND_IMAGE_NAME: ${{ github.repository }}-backend
  FRONTEND_IMAGE_NAME: ${{ github.repository }}-frontend

jobs:
  # Step 1: Run all tests
  test:
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Run backend tests
      working-directory: ./backend
      run: |
        go test -v -race -coverprofile=coverage.out ./...
        go tool cover -html=coverage.out -o coverage.html

    - name: Upload backend coverage
      uses: actions/upload-artifact@v3
      with:
        name: backend-coverage
        path: backend/coverage.html

    - name: Set up Node
      uses: actions/setup-node@v4
      with:
        node-version: '18'
        cache: 'npm'
        cache-dependency-path: frontend/package-lock.json

    - name: Run frontend tests
      working-directory: ./frontend
      run: |
        npm ci
        npm run lint
        npm run test:ci
        npm run test:e2e

    - name: SonarCloud Scan
      uses: SonarSource/sonarcloud-github-action@master
      env:
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        SONAR_TOKEN: ${{ secrets.SONAR_TOKEN }}

  # Step 2: Build Docker images
  build-images:
    needs: test
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    outputs:
      backend-image: ${{ steps.image.outputs.backend-image }}
      frontend-image: ${{ steps.image.outputs.frontend-image }}
    steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3

    - name: Log in to GitHub Container Registry
      uses: docker/login-action@v3
      with:
        registry: ${{ env.REGISTRY }}
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    - name: Extract metadata
      id: meta
      uses: docker/metadata-action@v5
      with:
        images: |
          ${{ env.REGISTRY }}/${{ env.BACKEND_IMAGE_NAME }}
          ${{ env.REGISTRY }}/${{ env.FRONTEND_IMAGE_NAME }}
        tags: |
          type=ref,event=branch
          type=ref,event=pr
          type=semver,pattern={{version}}
          type=semver,pattern={{major}}.{{minor}}
          type=sha,prefix={{branch}}-
          type=raw,value=latest,enable={{is_default_branch}}

    - name: Build and push backend Docker image
      uses: docker/build-push-action@v5
      with:
        context: ./backend
        file: ./backend/Dockerfile
        push: true
        tags: ${{ env.REGISTRY }}/${{ env.BACKEND_IMAGE_NAME }}:${{ github.sha }}
        labels: ${{ steps.meta.outputs.labels }}
        cache-from: type=gha
        cache-to: type=gha,mode=max
        platforms: linux/amd64,linux/arm64

    - name: Build and push frontend Docker image
      uses: docker/build-push-action@v5
      with:
        context: ./frontend
        file: ./frontend/Dockerfile
        push: true
        tags: ${{ env.REGISTRY }}/${{ env.FRONTEND_IMAGE_NAME }}:${{ github.sha }}
        labels: ${{ steps.meta.outputs.labels }}
        cache-from: type=gha
        cache-to: type=gha,mode=max
        platforms: linux/amd64,linux/arm64
        build-args: |
          VITE_AUTH0_DOMAIN=${{ secrets.AUTH0_DOMAIN }}
          VITE_AUTH0_CLIENT_ID=${{ secrets.AUTH0_CLIENT_ID }}
          VITE_AUTH0_AUDIENCE=${{ secrets.AUTH0_AUDIENCE }}
          VITE_GOOGLE_ADSENSE_CLIENT=${{ secrets.GOOGLE_ADSENSE_CLIENT }}
          VITE_PLAID_PUBLIC_KEY=${{ secrets.PLAID_PUBLIC_KEY }}

    - name: Output image names
      id: image
      run: |
        echo "backend-image=${{ env.REGISTRY }}/${{ env.BACKEND_IMAGE_NAME }}:${{ github.sha }}" >> $GITHUB_OUTPUT
        echo "frontend-image=${{ env.REGISTRY }}/${{ env.FRONTEND_IMAGE_NAME }}:${{ github.sha }}" >> $GITHUB_OUTPUT

  # Step 3: Security scanning
  security-scan:
    needs: build-images
    runs-on: ubuntu-latest
    steps:
    - name: Run Trivy vulnerability scanner - Backend
      uses: aquasecurity/trivy-action@master
      with:
        image-ref: ${{ needs.build-images.outputs.backend-image }}
        format: 'sarif'
        output: 'trivy-backend-results.sarif'

    - name: Upload Trivy results to GitHub Security
      uses: github/codeql-action/upload-sarif@v2
      with:
        sarif_file: 'trivy-backend-results.sarif'

  # Step 4: Deploy to staging (automatic for develop branch)
  deploy-staging:
    needs: [build-images, security-scan]
    if: github.ref == 'refs/heads/develop'
    runs-on: ubuntu-latest
    environment: staging
    steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Setup Fly.io
      uses: superfly/flyctl-actions/setup-flyctl@master

    - name: Deploy backend to staging
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN_STAGING }}
      run: |
        flyctl deploy \
          --app boostedbudget-staging-api \
          --image ${{ needs.build-images.outputs.backend-image }} \
          --config ./fly.staging.toml \
          --strategy immediate

    - name: Deploy frontend to staging
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN_STAGING }}
      run: |
        flyctl deploy \
          --app boostedbudget-staging-web \
          --image ${{ needs.build-images.outputs.frontend-image }} \
          --config ./frontend/fly.staging.toml \
          --strategy immediate

    - name: Run smoke tests
      run: |
        npm run test:smoke -- --env=staging

  # Step 5: Deploy to production (manual approval required)
  deploy-production:
    needs: [build-images, security-scan]
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    environment: 
      name: production
      url: https://boostedbudget.com
    steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Setup Fly.io
      uses: superfly/flyctl-actions/setup-flyctl@master

    - name: Deploy backend to production
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
      run: |
        # Deploy with blue-green strategy
        flyctl deploy \
          --app boostedbudget-api \
          --image ${{ needs.build-images.outputs.backend-image }} \
          --config ./fly.toml \
          --strategy bluegreen

    - name: Run database migrations
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
      run: |
        flyctl ssh console -a boostedbudget-api -C \
          "migrate -path /app/migrations -database \$DATABASE_URL up"

    - name: Deploy frontend to production
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
      run: |
        flyctl deploy \
          --app boostedbudget-web \
          --image ${{ needs.build-images.outputs.frontend-image }} \
          --config ./frontend/fly.toml \
          --strategy bluegreen

    - name: Verify deployment
      run: |
        ./scripts/verify-deployment.sh production

    - name: Notify deployment
      uses: 8398a7/action-slack@v3
      with:
        status: ${{ job.status }}
        text: 'Production deployment completed for version ${{ github.sha }}'
      env:
        SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK }}

  # Note: Multi-region deployment (Step 6) removed for MVP
  # See "Future: Multi-Region Migration Plan" section for expansion strategy
```

## Fly.io Application Configuration

### Backend Fly Configuration

```toml
# fly.toml
app = "boostedbudget-api"
primary_region = "iad"  # Single region for MVP: USA East (Virginia)
kill_signal = "SIGTERM"
kill_timeout = 30

[build]
  # Use pre-built Docker image from GitHub Container Registry
  image = "ghcr.io/yourusername/boostedbudget-backend:latest"

[env]
  PORT = "8080"
  PRIMARY_REGION = "iad"
  AUTH0_DOMAIN = "boostedbudget.auth0.com"
  AUTH0_AUDIENCE = "https://api.boostedbudget.com"
  PLAID_ENV = "production"
  ENVIRONMENT = "production"
  LOG_LEVEL = "info"
  ENABLE_METRICS = "true"
  # Note: DATABASE_URL set via secrets (single database)

[experimental]
  allowed_public_ports = []
  auto_rollback = true
  enable_consul = true

[metrics]
  port = 9091
  path = "/metrics"

[[services]]
  http_checks = []
  internal_port = 8080
  processes = ["app"]
  protocol = "tcp"
  script_checks = []
  
  [services.concurrency]
    hard_limit = 250
    soft_limit = 200
    type = "connections"

  [[services.ports]]
    force_https = true
    handlers = ["http"]
    port = 80

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 443

  [[services.tcp_checks]]
    grace_period = "1s"
    interval = "15s"
    restart_limit = 0
    timeout = "2s"

  [[services.http_checks]]
    interval = "10s"
    grace_period = "5s"
    method = "get"
    path = "/health"
    protocol = "http"
    restart_limit = 0
    timeout = "2s"
    tls_skip_verify = false
    headers = {}

[deploy]
  # Run migrations before starting the app
  release_command = "migrate -path /app/migrations -database $DATABASE_URL up"

[[vm]]
  cpu_kind = "shared"
  cpus = 1
  memory_mb = 512

[processes]
  app = "/app/boostedbudget-api"
  worker = "/app/boostedbudget-api --worker-mode"
```

### Frontend Fly Configuration

```toml
# frontend/fly.toml
app = "boostedbudget-web"
primary_region = "iad"

[build]
  # Use pre-built Docker image
  image = "ghcr.io/yourusername/boostedbudget-frontend:latest"

[env]
  PORT = "80"
  API_URL = "https://api.boostedbudget.com/v1"
  AUTH0_DOMAIN = "boostedbudget.auth0.com"
  ENVIRONMENT = "production"

[[services]]
  http_checks = []
  internal_port = 80
  processes = ["app"]
  protocol = "tcp"
  script_checks = []
  
  [services.concurrency]
    hard_limit = 1000
    soft_limit = 800
    type = "connections"

  [[services.ports]]
    force_https = true
    handlers = ["http"]
    port = 80

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 443

  [[services.http_checks]]
    interval = "30s"
    grace_period = "5s"
    method = "get"
    path = "/health"
    protocol = "http"
    timeout = "2s"

[[vm]]
  cpu_kind = "shared"
  cpus = 1
  memory_mb = 256

[mounts]
  source = "nginx_cache"
  destination = "/var/cache/nginx"
```

## Local Development with Production Images

### Docker Compose for Local Development

```yaml
# docker-compose.yml
version: '3.9'

services:
  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
      target: builder  # Use builder stage for hot reload
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://devuser:devpass@postgres:5432/boostedbudget?sslmode=disable
      REDIS_URL: redis://redis:6379
      PRIMARY_REGION: us-east-1
      AUTH0_DOMAIN: ${AUTH0_DOMAIN}
      AUTH0_AUDIENCE: ${AUTH0_AUDIENCE}
      AUTH0_CLIENT_ID: ${AUTH0_CLIENT_ID}
      AUTH0_CLIENT_SECRET: ${AUTH0_CLIENT_SECRET}
      ENVIRONMENT: development
      LOG_LEVEL: debug
    volumes:
      - ./backend:/build
    command: go run cmd/server/main.go
    depends_on:
      - postgres
      - redis

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
      target: builder  # Use builder stage for hot reload
    ports:
      - "3000:3000"
    environment:
      VITE_API_URL: http://localhost:8080/api/v1
      VITE_AUTH0_DOMAIN: ${AUTH0_DOMAIN}
      VITE_AUTH0_CLIENT_ID: ${AUTH0_CLIENT_ID}
    volumes:
      - ./frontend:/build
    command: npm run dev

  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: devuser
      POSTGRES_PASSWORD: devpass
      POSTGRES_DB: boostedbudget
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  # Optional: Local image registry for testing
  registry:
    image: registry:2
    ports:
      - "5000:5000"
    volumes:
      - registry_data:/var/lib/registry

volumes:
  postgres_data:
  registry_data:
```

### Build and Push Script

```bash
#!/bin/bash
# scripts/build-and-push.sh

set -e

# Configuration
REGISTRY="${REGISTRY:-ghcr.io}"
NAMESPACE="${NAMESPACE:-yourusername}"
VERSION="${VERSION:-$(git describe --tags --always --dirty)}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Building BoostedBudget Docker images...${NC}"

# Build backend
echo -e "${GREEN}Building backend image...${NC}"
docker build \
  -t ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:${VERSION} \
  -t ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:latest \
  -f backend/Dockerfile \
  backend/

# Build frontend
echo -e "${GREEN}Building frontend image...${NC}"
docker build \
  -t ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:${VERSION} \
  -t ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:latest \
  --build-arg VITE_AUTH0_DOMAIN=${AUTH0_DOMAIN} \
  --build-arg VITE_AUTH0_CLIENT_ID=${AUTH0_CLIENT_ID} \
  -f frontend/Dockerfile \
  frontend/

# Push images
if [ "$1" == "--push" ]; then
  echo -e "${YELLOW}Pushing images to registry...${NC}"
  
  docker push ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:${VERSION}
  docker push ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:latest
  
  docker push ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:${VERSION}
  docker push ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:latest
  
  echo -e "${GREEN}Images successfully pushed!${NC}"
else
  echo -e "${YELLOW}Images built locally. Use --push to push to registry.${NC}"
fi

# Output image names
echo -e "${GREEN}Built images:${NC}"
echo "  Backend:  ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:${VERSION}"
echo "  Frontend: ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:${VERSION}"
```

### Deploy Script

```bash
#!/bin/bash
# scripts/deploy-to-fly.sh

set -e

# Parse arguments
ENVIRONMENT="${1:-staging}"
VERSION="${2:-latest}"

# Configuration
REGISTRY="ghcr.io"
NAMESPACE="yourusername"

echo "Deploying BoostedBudget to ${ENVIRONMENT} (version: ${VERSION})"

# Set the correct Fly app names based on environment
if [ "$ENVIRONMENT" == "production" ]; then
  BACKEND_APP="boostedbudget-api"
  FRONTEND_APP="boostedbudget-web"
  FLY_CONFIG="fly.toml"
elif [ "$ENVIRONMENT" == "staging" ]; then
  BACKEND_APP="boostedbudget-staging-api"
  FRONTEND_APP="boostedbudget-staging-web"
  FLY_CONFIG="fly.staging.toml"
else
  echo "Unknown environment: $ENVIRONMENT"
  exit 1
fi

# Deploy backend
echo "Deploying backend..."
flyctl deploy \
  --app ${BACKEND_APP} \
  --image ${REGISTRY}/${NAMESPACE}/boostedbudget-backend:${VERSION} \
  --config ${FLY_CONFIG} \
  --strategy bluegreen

# Run migrations
echo "Running database migrations..."
flyctl ssh console -a ${BACKEND_APP} -C \
  "migrate -path /app/migrations -database \$DATABASE_URL up"

# Deploy frontend
echo "Deploying frontend..."
flyctl deploy \
  --app ${FRONTEND_APP} \
  --image ${REGISTRY}/${NAMESPACE}/boostedbudget-frontend:${VERSION} \
  --config frontend/${FLY_CONFIG} \
  --strategy bluegreen

# Verify deployment
echo "Verifying deployment..."
./scripts/verify-deployment.sh ${ENVIRONMENT}

echo "Deployment complete!"
```

## Secrets and Configuration Management

### GitHub Secrets Required

```yaml
# Required GitHub Secrets (Single Region):
FLY_API_TOKEN                # Fly.io API token for production
FLY_API_TOKEN_STAGING        # Fly.io API token for staging
GITHUB_TOKEN                 # Auto-generated for packages
AUTH0_DOMAIN                 # Auth0 domain
AUTH0_CLIENT_ID             # Auth0 client ID
AUTH0_CLIENT_SECRET         # Auth0 client secret
AUTH0_AUDIENCE              # Auth0 API audience
PLAID_CLIENT_ID             # Plaid client ID
PLAID_SECRET                # Plaid secret (sandbox/production)
PLAID_PUBLIC_KEY            # Plaid public key
STRIPE_SECRET_KEY           # Stripe secret key
STRIPE_WEBHOOK_SECRET       # Stripe webhook secret
GOOGLE_ADSENSE_CLIENT       # AdSense client ID
SLACK_WEBHOOK              # Slack notifications
SONAR_TOKEN                # SonarCloud token
CF_ZONE_ID                 # CloudFlare zone ID
CF_API_TOKEN               # CloudFlare API token

# Note: No EU_DATABASE_URL needed for single-region MVP
# Multi-region secrets documented in "Future: Multi-Region Migration Plan"
```

### Fly.io Secrets Management

```bash
# Set secrets in Fly.io (Single Region)
fly secrets set \
  DATABASE_URL="postgres://..." \
  REDIS_URL="redis://..." \
  AUTH0_DOMAIN="boostedbudget.auth0.com" \
  AUTH0_CLIENT_ID="..." \
  AUTH0_CLIENT_SECRET="..." \
  PLAID_CLIENT_ID="..." \
  PLAID_SECRET="..." \
  STRIPE_SECRET_KEY="..." \
  STRIPE_WEBHOOK_SECRET="..." \
  --app boostedbudget-api

# List secrets
fly secrets list --app boostedbudget-api

# Remove a secret
fly secrets unset KEY_NAME --app boostedbudget-api

# Note: Only single DATABASE_URL needed for MVP
# Multi-region database secrets added in Phase 7+
```

## Monitoring and Observability

### Health Check Endpoints

```go
// backend/internal/handlers/health.go
package handlers

type HealthResponse struct {
    Status    string            `json:"status"`
    Version   string            `json:"version"`
    Timestamp time.Time         `json:"timestamp"`
    Checks    map[string]string `json:"checks"`
}

func (h *HealthHandler) GetHealth(c echo.Context) error {
    checks := make(map[string]string)
    
    // Check database
    if err := h.db.Ping(); err != nil {
        checks["database"] = "unhealthy"
    } else {
        checks["database"] = "healthy"
    }
    
    // Check Redis
    if err := h.redis.Ping(c.Request().Context()).Err(); err != nil {
        checks["redis"] = "unhealthy"
    } else {
        checks["redis"] = "healthy"
    }
    
    // Check Auth0
    if err := h.checkAuth0(); err != nil {
        checks["auth0"] = "unhealthy"
    } else {
        checks["auth0"] = "healthy"
    }
    
    status := "healthy"
    for _, check := range checks {
        if check != "healthy" {
            status = "degraded"
            break
        }
    }
    
    return c.JSON(http.StatusOK, HealthResponse{
        Status:    status,
        Version:   version.Version,
        Timestamp: time.Now(),
        Checks:    checks,
    })
}
```

### Deployment Verification Script

```bash
#!/bin/bash
# scripts/verify-deployment.sh

ENVIRONMENT=$1
MAX_RETRIES=30
RETRY_INTERVAL=10

if [ "$ENVIRONMENT" == "production" ]; then
  API_URL="https://api.boostedbudget.com"
  WEB_URL="https://boostedbudget.com"
else
  API_URL="https://staging-api.boostedbudget.com"
  WEB_URL="https://staging.boostedbudget.com"
fi

echo "Verifying deployment for $ENVIRONMENT..."

# Check API health
for i in $(seq 1 $MAX_RETRIES); do
  response=$(curl -s -o /dev/null -w "%{http_code}" $API_URL/health)
  if [ $response -eq 200 ]; then
    echo "✓ API is healthy"
    break
  fi
  echo "Waiting for API... (attempt $i/$MAX_RETRIES)"
  sleep $RETRY_INTERVAL
done

# Check frontend
for i in $(seq 1 $MAX_RETRIES); do
  response=$(curl -s -o /dev/null -w "%{http_code}" $WEB_URL)
  if [ $response -eq 200 ]; then
    echo "✓ Frontend is accessible"
    break
  fi
  echo "Waiting for frontend... (attempt $i/$MAX_RETRIES)"
  sleep $RETRY_INTERVAL
done

# Run smoke tests
npm run test:smoke -- --env=$ENVIRONMENT

echo "Deployment verification complete!"
```

## Benefits of This Approach

1. **Portability**: Docker images can be deployed anywhere (Fly.io, AWS, GCP, self-hosted)
2. **Consistency**: Same image tested in CI is deployed to production
3. **Rollback**: Easy rollback to previous image versions
4. **Multi-Region**: Fly.io handles global distribution automatically
5. **Cost-Effective**: Fly.io's pricing is developer-friendly for startups
6. **GDPR Compliance**: Regional data isolation with EU database
7. **Zero-Downtime**: Blue-green deployments ensure no downtime
8. **Security**: Images scanned for vulnerabilities before deployment

## Key Commands Summary

```bash
# Build images locally
./scripts/build-and-push.sh

# Build and push to registry
./scripts/build-and-push.sh --push

# Deploy to staging
./scripts/deploy-to-fly.sh staging

# Deploy to production
./scripts/deploy-to-fly.sh production v1.2.3

# Check logs
fly logs --app boostedbudget-api

# SSH into container
fly ssh console --app boostedbudget-api

# Scale regions
fly regions add ams --app boostedbudget-api
fly scale count 2 --region ams --app boostedbudget-api

# Monitor metrics
fly dashboard metrics --app boostedbudget-api
```

---

## Future: Multi-Region Migration Plan

This section documents the path to expand from single-region (us-east-1) to multi-region deployment. Implement when user base and revenue justify the additional complexity and cost.

### Phase 1: Enable Fly.io Edge Proxy (Quick Win)

**Timeline**: 1-2 days
**Cost**: Minimal (included in Fly.io pricing)
**Complexity**: Low

#### Benefits
- 40-60% latency reduction for international users
- No code changes required
- Database remains in USA
- Simple rollback if issues arise

#### Implementation

```bash
# Enable automatic edge caching for frontend
fly regions add ams lhr fra syd nrt sin --app boostedbudget-web

# Frontend gets automatically cached at edge locations
# Backend API still runs only in iad (single region)
# Fly.io proxy routes requests to nearest edge, then to iad backend
```

#### Verification

```bash
# Check active regions
fly regions list --app boostedbudget-web

# Expected output:
# Regions [boostedbudget-web]: iad(primary), ams, lhr, fra, syd, nrt, sin
```

#### Monitoring

```bash
# Monitor latency improvements
fly metrics --app boostedbudget-web

# Check for any edge caching issues
fly logs --app boostedbudget-web
```

#### Rollback (if needed)

```bash
# Remove edge regions, keep only primary
fly regions set iad --app boostedbudget-web
```

---

### Phase 2: Add Read Replicas (Medium Effort)

**Timeline**: 1-2 weeks
**Cost**: ~$30-50/month per region
**Complexity**: Medium

#### Benefits
- Faster reads for international users
- Improved disaster recovery
- Data sovereignty preparation
- Write operations still centralized

#### Implementation

**Step 1: Create Replica Databases**

```bash
# Create EU read replica
fly postgres create \
  --name boostedbudget-db-eu \
  --region lhr \
  --initial-cluster-size 1

# Create APAC read replica
fly postgres create \
  --name boostedbudget-db-apac \
  --region nrt \
  --initial-cluster-size 1
```

**Step 2: Set Up Replication**

```bash
# Attach replicas to primary database
fly postgres attach boostedbudget-db \
  --replica boostedbudget-db-eu

fly postgres attach boostedbudget-db \
  --replica boostedbudget-db-apac

# Verify replication status
fly postgres connect -a boostedbudget-db-eu
# Run: SELECT pg_is_in_recovery(); -- Should return 't' for replica
```

**Step 3: Configure Backend for Read Replicas**

```go
// backend/internal/infrastructure/db_config.go
type DatabaseConfig struct {
    PrimaryURL  string // us-east-1 (writes)
    ReplicaURLs map[string]string // Read replicas
}

func (cfg *DatabaseConfig) GetReadDB(region string) *sql.DB {
    // Route reads to nearest replica
    if replicaURL, exists := cfg.ReplicaURLs[region]; exists {
        return openDB(replicaURL)
    }
    // Fallback to primary
    return openDB(cfg.PrimaryURL)
}

func (cfg *DatabaseConfig) GetWriteDB() *sql.DB {
    // All writes go to primary
    return openDB(cfg.PrimaryURL)
}
```

**Step 4: Update Fly.io Secrets**

```bash
fly secrets set \
  DATABASE_URL="postgres://primary..." \
  DATABASE_REPLICA_EU="postgres://replica-eu..." \
  DATABASE_REPLICA_APAC="postgres://replica-apac..." \
  --app boostedbudget-api
```

#### Monitoring Replication Lag

```sql
-- Run on primary database
SELECT
    client_addr,
    state,
    sent_lsn,
    write_lsn,
    flush_lsn,
    replay_lsn,
    sync_state,
    pg_wal_lsn_diff(sent_lsn, replay_lsn) AS replication_lag_bytes
FROM pg_stat_replication;
```

#### Rollback

```bash
# Remove read replicas
fly postgres detach boostedbudget-db-eu
fly postgres detach boostedbudget-db-apac

# Delete replica databases
fly postgres destroy boostedbudget-db-eu
fly postgres destroy boostedbudget-db-apac

# Remove replica URLs from secrets
fly secrets unset DATABASE_REPLICA_EU DATABASE_REPLICA_APAC
```

---

### Phase 3: Full Multi-Region (Major Feature)

**Timeline**: 6-10 weeks
**Cost**: ~$100-200/month additional
**Complexity**: High

This is the complete multi-region implementation as documented in `boosted-budget-complete-plan.md` Phase 7+.

#### Key Components

1. **Regional Databases** (Week 1-2)
   - Independent database per region
   - No replication (data isolated by region)
   - Users explicitly choose data region

2. **Code Changes** (Week 3-4)
   - Implement `RegionRouter` pattern
   - Activate existing `*InRegion` repository methods
   - Add region selection UI
   - Update Auth0 rules for geo-suggestion

3. **Data Migration** (Week 5-6)
   - User-initiated region migration tool
   - Data export/import pipeline
   - 7-day verification period
   - Rollback capability

4. **Deployment** (Week 7-8)
   - Deploy backend to multiple regions
   - Configure Fly.io for multi-region routing
   - Set up regional secrets
   - Scale instances per region

5. **Testing & Verification** (Week 9-10)
   - Region-specific test suites
   - Cross-region consistency tests
   - Load testing
   - Compliance verification

#### Fly.io Multi-Region Configuration

```toml
# fly.toml (Multi-region version)
app = "boostedbudget-api"
primary_region = "iad"

[env]
  PORT = "8080"
  ENABLE_MULTI_REGION = "true"
  PRIMARY_REGION = "iad"

# Deploy to multiple regions
[[services]]
  internal_port = 8080
  protocol = "tcp"

  [[services.ports]]
    handlers = ["http"]
    port = 80
    force_https = true

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 443
```

Deploy script:
```bash
#!/bin/bash
# scripts/deploy-multi-region.sh

# Add regions
fly regions add iad lhr nrt --app boostedbudget-api

# Scale instances per region
fly scale count 2 --region iad --app boostedbudget-api    # USA
fly scale count 2 --region lhr --app boostedbudget-api    # EU
fly scale count 1 --region nrt --app boostedbudget-api    # APAC

# Deploy with rolling strategy
fly deploy --strategy rolling

# Verify all regions
fly status --app boostedbudget-api
fly regions list --app boostedbudget-api
```

#### Database Secrets Per Region

```bash
# Set regional database URLs
fly secrets set \
  DATABASE_URL_US="postgres://us..." \
  DATABASE_URL_EU="postgres://eu..." \
  DATABASE_URL_APAC="postgres://apac..." \
  --app boostedbudget-api
```

#### Region Router Implementation

```go
// backend/internal/infrastructure/region_router.go
package infrastructure

import (
    "database/sql"
    "fmt"
)

type RegionRouter struct {
    databases map[string]*sql.DB
}

func NewRegionRouter(config DatabaseConfig) (*RegionRouter, error) {
    router := &RegionRouter{
        databases: make(map[string]*sql.DB),
    }

    // Initialize database connections for each region
    for region, url := range config.RegionalURLs {
        db, err := sql.Open("postgres", url)
        if err != nil {
            return nil, fmt.Errorf("failed to connect to %s: %w", region, err)
        }
        router.databases[region] = db
    }

    return router, nil
}

func (r *RegionRouter) GetDB(region string) (*sql.DB, error) {
    db, exists := r.databases[region]
    if !exists {
        // Fallback to primary region
        return r.databases["us-east-1"], nil
    }
    return db, nil
}

func (r *RegionRouter) Close() error {
    for _, db := range r.databases {
        if err := db.Close(); err != nil {
            return err
        }
    }
    return nil
}
```

#### Frontend Region Selection

```typescript
// frontend/src/features/onboarding/RegionSelector.tsx
import { useState } from 'react';
import { useAuth0 } from '@auth0/auth0-react';

interface Region {
  code: string;
  name: string;
  flag: string;
  description: string;
}

const regions: Region[] = [
  {
    code: 'us-east-1',
    name: 'United States',
    flag: '🇺🇸',
    description: 'Data stored in Virginia, USA'
  },
  {
    code: 'eu-west-1',
    name: 'European Union',
    flag: '🇪🇺',
    description: 'Data stored in London, UK'
  },
  {
    code: 'ap-southeast-1',
    name: 'Asia Pacific',
    flag: '🌏',
    description: 'Data stored in Tokyo, Japan'
  }
];

export const RegionSelector: React.FC = () => {
  const [selectedRegion, setSelectedRegion] = useState<string>('');
  const { user } = useAuth0();

  const handleSelectRegion = async (regionCode: string) => {
    setSelectedRegion(regionCode);

    // Store region preference
    await api.updateUserRegion(regionCode);

    // Proceed with onboarding
    navigate('/dashboard');
  };

  return (
    <div className="region-selector">
      <h2>Choose Your Data Region</h2>
      <p>Your data will be stored in the region you select for compliance and performance.</p>

      <div className="regions-grid">
        {regions.map(region => (
          <button
            key={region.code}
            className={`region-card ${selectedRegion === region.code ? 'selected' : ''}`}
            onClick={() => handleSelectRegion(region.code)}
          >
            <span className="flag">{region.flag}</span>
            <h3>{region.name}</h3>
            <p>{region.description}</p>
          </button>
        ))}
      </div>

      <p className="note">
        You can change your data region later in settings. Migration takes 1-2 hours.
      </p>
    </div>
  );
};
```

#### Testing Multi-Region Setup

```bash
# Test regional deployment
./scripts/test-multi-region.sh

# Load test from different regions
k6 run --env REGION=us tests/load/regional-test.js
k6 run --env REGION=eu tests/load/regional-test.js
k6 run --env REGION=apac tests/load/regional-test.js

# Verify data isolation
./scripts/verify-region-isolation.sh
```

#### Monitoring Multi-Region

```bash
# View metrics per region
fly metrics --app boostedbudget-api --region iad
fly metrics --app boostedbudget-api --region lhr
fly metrics --app boostedbudget-api --region nrt

# Check logs per region
fly logs --app boostedbudget-api --region iad
fly logs --app boostedbudget-api --region lhr

# Monitor regional health
fly status --all-regions --app boostedbudget-api
```

---

### Cost Comparison

| Deployment Strategy | Monthly Cost | Latency (Intl) | Complexity |
|---------------------|--------------|----------------|------------|
| **Current (Single Region)** | $50-100 | 200-500ms | Low |
| **Phase 1 (Edge Proxy)** | $50-100 | 100-200ms | Low |
| **Phase 2 (Read Replicas)** | $100-150 | 50-150ms | Medium |
| **Phase 3 (Multi-Region)** | $200-300 | 20-50ms | High |

### Decision Framework

**Deploy Phase 1 (Edge Proxy) when:**
- ✅ International traffic > 10%
- ✅ MVP validated
- ✅ No additional cost

**Deploy Phase 2 (Read Replicas) when:**
- ✅ International users complaining about speed
- ✅ Revenue > $1,000/month
- ✅ 2-3 weeks of dev time available

**Deploy Phase 3 (Full Multi-Region) when:**
- ✅ User base > 10,000
- ✅ Revenue > $10,000/month
- ✅ Enterprise customers requesting data residency
- ✅ 6-10 weeks of dev time available
- ✅ Legal review completed

### Rollback Strategy

All phases include rollback procedures to safely return to single-region deployment if issues arise. See individual phase sections for specific rollback commands.

---

This approach gives you complete control over your Docker images while leveraging Fly.io's excellent global infrastructure for deployment. The single-region MVP provides a stable foundation, with clear expansion paths when justified by business growth.