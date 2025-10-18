# BoostedBudget Deployment Scripts

This directory contains deployment and utility scripts for building and deploying BoostedBudget to Fly.io.

## Overview

The deployment process follows this workflow:

1. **Build** Docker images and push to GitHub Container Registry (GHCR)
2. **Deploy** pre-built images to Fly.io
3. **Verify** deployment health
4. **Rollback** if needed

## Prerequisites

- Docker installed and running
- Fly CLI installed (`brew install flyctl` or `curl -L https://fly.io/install.sh | sh`)
- Authenticated with Fly.io (`flyctl auth login`)
- Authenticated with GHCR (`echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin`)
- `jq` installed for JSON parsing (`brew install jq` or `apt-get install jq`)

## Scripts

### 1. build-and-push.sh

Builds Docker images for backend and frontend, tags them, and optionally pushes to GHCR.

**Usage:**
```bash
# Build only (local testing)
./scripts/build-and-push.sh

# Build and push with git SHA tag
./scripts/build-and-push.sh --push

# Build and push with custom version
./scripts/build-and-push.sh --push v1.2.3
```

**What it does:**
- Checks Docker is running
- Verifies GHCR authentication (if pushing)
- Builds backend image from `backend/Dockerfile`
- Builds frontend image from `frontend/Dockerfile`
- Tags images with specified version and `latest`
- Pushes to `ghcr.io/maxnussbaum/boostedbudget-backend` and `boostedbudget-frontend`

**Example output:**
```
╔═══════════════════════════════════════════════════════════════╗
║           BoostedBudget Docker Build Script                   ║
╚═══════════════════════════════════════════════════════════════╝

Version: a618a77
Push to registry: true

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Building ghcr.io/maxnussbaum/boostedbudget-backend:a618a77
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Successfully built ghcr.io/maxnussbaum/boostedbudget-backend:a618a77
```

### 2. deploy-to-fly.sh

Deploys pre-built Docker images from GHCR to Fly.io.

**Usage:**
```bash
# Deploy latest to staging
./scripts/deploy-to-fly.sh staging

# Deploy specific version to production
./scripts/deploy-to-fly.sh production --image v1.2.3
```

**What it does:**
- Validates environment (staging or production)
- Checks Fly CLI is installed and authenticated
- Verifies apps exist on Fly.io
- Deploys backend from GHCR image
- Runs database migrations (automatic on startup)
- Deploys frontend from GHCR image
- Verifies deployment health
- Provides deployment summary and monitoring commands

**Production safety:**
- Requires explicit `yes` confirmation for production deployments
- Uses rolling deployment strategy (zero-downtime)
- Auto-rollback on health check failures (configured in fly.toml)

**Example output:**
```
╔═══════════════════════════════════════════════════════════════╗
║           BoostedBudget Fly.io Deployment Script              ║
╚═══════════════════════════════════════════════════════════════╝

Environment: production
Version: v1.2.3

⚠ WARNING: You are deploying to PRODUCTION
Are you sure you want to continue? (yes/NO) yes

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Deploying Backend: boostedbudget-api
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Backend deployed successfully
```

### 3. verify-deployment.sh

Verifies deployment health by running smoke tests against deployed applications.

**Usage:**
```bash
# Verify staging deployment
./scripts/verify-deployment.sh staging

# Verify production deployment
./scripts/verify-deployment.sh production
```

**What it checks:**
- Backend health endpoint (`/health`)
- Backend metrics endpoint (`/metrics`)
- Frontend homepage
- Frontend static assets (favicon)
- Fly.io app status (running instances)

**Example output:**
```
╔═══════════════════════════════════════════════════════════════╗
║           BoostedBudget Deployment Verification               ║
╚═══════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Backend Health Checks
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Health endpoint returned valid JSON with 'status'
✓ Metrics endpoint returned 200
```

### 4. rollback.sh

Rolls back deployment to a previous release.

**Usage:**
```bash
# Rollback to previous release
./scripts/rollback.sh staging

# Rollback to specific release
./scripts/rollback.sh production v123
```

**What it does:**
- Lists recent releases for both backend and frontend
- Shows current release versions
- Confirms rollback action
- Rolls back backend to specified or previous release
- Rolls back frontend to specified or previous release
- Verifies rollback health
- Provides monitoring commands

**Safety features:**
- Requires `yes` confirmation for production rollbacks
- Shows release history before rollback
- Verifies rollback with health checks
- Provides monitoring commands if verification fails

**Example output:**
```
╔═══════════════════════════════════════════════════════════════╗
║           BoostedBudget Rollback Script                       ║
╚═══════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Recent Releases for Backend
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ID         Version  Status       Reason
v125       125      complete     Deployed via API
v124       124      complete     Deployed via API
```

## Complete Deployment Workflow

### Initial Setup (One-time)

1. **Authenticate with GHCR:**
   ```bash
   echo $GITHUB_TOKEN | docker login ghcr.io -u maxnussbaum --password-stdin
   ```

2. **Authenticate with Fly.io:**
   ```bash
   flyctl auth login
   ```

3. **Create Fly.io apps** (if not already created):
   ```bash
   flyctl apps create boostedbudget-api
   flyctl apps create boostedbudget-web
   ```

### Regular Deployment

1. **Build and push Docker images:**
   ```bash
   ./scripts/build-and-push.sh --push v1.2.3
   ```

2. **Deploy to staging:**
   ```bash
   ./scripts/deploy-to-fly.sh staging --image v1.2.3
   ```

3. **Verify staging deployment:**
   ```bash
   ./scripts/verify-deployment.sh staging
   ```

4. **Deploy to production:**
   ```bash
   ./scripts/deploy-to-fly.sh production --image v1.2.3
   ```

5. **Verify production deployment:**
   ```bash
   ./scripts/verify-deployment.sh production
   ```

### Emergency Rollback

If something goes wrong:

```bash
# Rollback production to previous version
./scripts/rollback.sh production

# Or rollback to specific version
./scripts/rollback.sh production v122
```

## CI/CD Integration

These scripts are designed to work with GitHub Actions. Example workflow:

```yaml
name: Deploy to Production

on:
  push:
    tags:
      - 'v*'

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build and Push
        run: ./scripts/build-and-push.sh --push ${{ github.ref_name }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Deploy to Production
        run: ./scripts/deploy-to-fly.sh production --image ${{ github.ref_name }}
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}

      - name: Verify Deployment
        run: ./scripts/verify-deployment.sh production
```

## Troubleshooting

### Docker build fails

```bash
# Check Docker is running
docker info

# Clean up old images
docker system prune -a
```

### GHCR authentication fails

```bash
# Re-authenticate
echo $GITHUB_TOKEN | docker login ghcr.io -u maxnussbaum --password-stdin

# Verify authentication
docker pull ghcr.io/hello-world
```

### Fly.io deployment fails

```bash
# Check authentication
flyctl auth whoami

# View app logs
flyctl logs --app boostedbudget-api

# Check app status
flyctl status --app boostedbudget-api

# SSH into app
flyctl ssh console --app boostedbudget-api
```

### Verification fails

```bash
# Check backend health manually
curl https://api.boostedbudget.com/health

# Check frontend
curl https://boostedbudget.com

# View Fly.io dashboard
flyctl dashboard --app boostedbudget-api
```

## App Names by Environment

### Production
- Backend: `boostedbudget-api`
- Frontend: `boostedbudget-web`
- URLs:
  - Backend: `https://api.boostedbudget.com`
  - Frontend: `https://boostedbudget.com`

### Staging
- Backend: `boostedbudget-api-staging`
- Frontend: `boostedbudget-web-staging`
- URLs:
  - Backend: `https://boostedbudget-api-staging.fly.dev`
  - Frontend: `https://boostedbudget-web-staging.fly.dev`

## Script Conventions

All scripts follow these conventions:

- **Exit codes**: 0 for success, non-zero for failure
- **Colors**: Green for success, red for errors, yellow for warnings, blue for info
- **Confirmation**: Production operations require explicit confirmation
- **Dry-run**: Where applicable, default to safe mode (e.g., build without push)
- **Verbose**: Clear output with section headers and status messages
- **Error handling**: `set -euo pipefail` for strict error handling

## References

- [Fly.io Documentation](https://fly.io/docs/)
- [GHCR Documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [BoostedBudget Fly.io Setup Guide](../docs/FLYIO_SETUP.md)
- [BoostedBudget Secrets Reference](../docs/SECRETS_REFERENCE.md)
