# BoostedBudget - Docker-First Fly.io Deployment Strategy

## Core Deployment Philosophy
Build once, deploy anywhere. Both frontend and backend are compiled into Docker images that can be deployed to Fly.io, AWS ECS, Google Cloud Run, or any container platform.

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

  # Step 6: Multi-region deployment
  deploy-regions:
    needs: deploy-production
    runs-on: ubuntu-latest
    strategy:
      matrix:
        region: [ams, lhr, fra, syd, nrt, sin]
    steps:
    - name: Setup Fly.io
      uses: superfly/flyctl-actions/setup-flyctl@master

    - name: Scale in ${{ matrix.region }}
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
      run: |
        # Scale backend in region
        flyctl regions add ${{ matrix.region }} --app boostedbudget-api
        flyctl scale count 2 --region ${{ matrix.region }} --app boostedbudget-api
        
        # Scale frontend in region
        flyctl regions add ${{ matrix.region }} --app boostedbudget-web
        flyctl scale count 2 --region ${{ matrix.region }} --app boostedbudget-web
```

## Fly.io Application Configuration

### Backend Fly Configuration

```toml
# fly.toml
app = "boostedbudget-api"
primary_region = "iad"
kill_signal = "SIGTERM"
kill_timeout = 30

[build]
  # Use pre-built Docker image from GitHub Container Registry
  image = "ghcr.io/yourusername/boostedbudget-backend:latest"

[env]
  PORT = "8080"
  AUTH0_DOMAIN = "boostedbudget.auth0.com"
  AUTH0_AUDIENCE = "https://api.boostedbudget.com"
  PLAID_ENV = "production"
  ENVIRONMENT = "production"
  LOG_LEVEL = "info"
  ENABLE_METRICS = "true"

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
# Required GitHub Secrets:
FLY_API_TOKEN                # Fly.io API token for production
FLY_API_TOKEN_STAGING        # Fly.io API token for staging
GITHUB_TOKEN                 # Auto-generated for packages
AUTH0_DOMAIN                 # Auth0 domain
AUTH0_CLIENT_ID             # Auth0 client ID
AUTH0_CLIENT_SECRET         # Auth0 client secret
AUTH0_AUDIENCE              # Auth0 API audience
PLAID_CLIENT_ID             # Plaid client ID
PLAID_SECRET                # Plaid secret
PLAID_PUBLIC_KEY            # Plaid public key
STRIPE_SECRET_KEY           # Stripe secret key
STRIPE_WEBHOOK_SECRET       # Stripe webhook secret
GOOGLE_ADSENSE_CLIENT       # AdSense client ID
SLACK_WEBHOOK              # Slack notifications
SONAR_TOKEN                # SonarCloud token
CF_ZONE_ID                 # CloudFlare zone ID
CF_API_TOKEN               # CloudFlare API token
```

### Fly.io Secrets Management

```bash
# Set secrets in Fly.io
fly secrets set \
  DATABASE_URL="postgres://..." \
  EU_DATABASE_URL="postgres://..." \
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

This approach gives you complete control over your Docker images while leveraging Fly.io's excellent global infrastructure for deployment.