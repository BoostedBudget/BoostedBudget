# Mac Mini Staging Environment Setup Guide

Complete guide for setting up the Mac Mini as a $0/month staging environment with auto-start on boot, Tailscale VPN access, and automated CI/CD deployment.

## 📋 Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Phase 1: Infrastructure Setup](#phase-1-infrastructure-setup)
4. [Phase 2: Credentials & Secrets](#phase-2-credentials--secrets)
5. [Phase 3: Initial Deployment](#phase-3-initial-deployment)
6. [Phase 4: Verification & Testing](#phase-4-verification--testing)
7. [Usage & Access](#usage--access)
8. [Troubleshooting](#troubleshooting)
9. [Maintenance](#maintenance)

---

## Overview

### Architecture

- **Environment**: Staging (pre-production testing)
- **Platform**: Mac Mini (self-hosted)
- **Cost**: $0/month
- **Access**: Tailscale VPN + local network
- **Deployment**: Automated via GitHub Actions
- **Images**: Pre-built on GHCR, pulled by Mac Mini
- **Auto-start**: All containers restart on boot

### Deployment Flow

```
Push to staging branch
    ↓
GitHub Actions runs (on Mac Mini self-hosted runner)
    ↓
Build Docker images → Push to GHCR
    ↓
Security scan images
    ↓
Pull images from GHCR → Deploy to Mac Mini
    ↓
Run smoke tests
    ↓
Deployment complete ✅
```

### File Structure

```
BoostedBudget/
├── docker-compose.staging.yml     # Staging compose config
├── .env.staging                    # Secrets (gitignored)
├── .env.staging.example            # Template (committed)
├── scripts/deploy-staging.sh       # Deployment script
├── .github/workflows/
│   ├── test.yml                   # PR tests
│   ├── deploy-staging.yml         # Staging deployment
│   └── deploy-production.yml      # Production deployment
└── docs/
    └── MAC_MINI_STAGING_SETUP.md  # This file
```

---

## Prerequisites

### Already Configured ✅

- [x] Mac Mini set up as GitHub Actions self-hosted runner
- [x] Docker Desktop installed and configured
- [x] Docker auto-starts on boot (via GitHub runners setup)
- [x] Homebrew installed
- [x] Git configured

### Need to Install 🔧

- [ ] Tailscale (full app, not just CLI)
- [ ] Auth0 dev tenant credentials
- [ ] Stripe test mode API keys
- [ ] GitHub Secrets configured

---

## Phase 1: Infrastructure Setup

### 1.1 Install Tailscale VPN

**⚠️ Important**: Install the full Tailscale app, not just the CLI.

```bash
# Uninstall CLI-only version if you installed it
brew uninstall tailscale

# Install full Tailscale app (includes daemon + GUI)
brew install --cask tailscale

# Start Tailscale app (will appear in menu bar)
open -a Tailscale
```

**Configuration**:

1. Click the Tailscale icon in menu bar
2. Click "Log in to Tailscale"
3. Authenticate via browser
4. Your Mac Mini will get a Tailscale hostname (e.g., `mac-mini.tailnet-name.ts.net`)

**Verify installation**:

```bash
# Check Tailscale status
tailscale status

# Get your Tailscale IP
tailscale ip -4

# Get your Tailscale hostname
tailscale status | grep "$(hostname)" | awk '{print $1}'
```

**Result**: You should see output like:
```
100.x.x.x   mac-mini.tailnet-name.ts.net   yingyueliu@  macOS   -
```

### 1.2 Configure Static Local IP (Optional but Recommended)

To prevent IP changes after reboot:

1. Open **System Settings** → **Network**
2. Select **Ethernet** (or Wi-Fi) → **Details**
3. Go to **TCP/IP** tab
4. Change **Configure IPv4** from "Using DHCP" to "Manually"
5. Set **IPv4 Address**: `192.168.1.79` (current IP)
6. Set **Subnet Mask**: `255.255.255.0`
7. Set **Router**: `192.168.1.1` (your router's IP)
8. Click **OK** → **Apply**

---

## Phase 2: Credentials & Secrets

### 2.1 Auth0 Dev Tenant Setup

Create a **separate** Auth0 dev tenant for staging (never use production credentials).

1. Go to https://manage.auth0.com/
2. Create new tenant: **dev-boostedbudget** (or similar)
3. Note your domain: `dev-boostedbudget.us.auth0.com`

#### Create Backend API Application (M2M)

1. **Applications** → **Create Application**
2. Name: `BoostedBudget Backend API (Staging)`
3. Type: **Machine to Machine**
4. API: Create new API
   - Name: `BoostedBudget API (Staging)`
   - Identifier: `https://api-staging.boostedbudget.com`
5. Note credentials:
   - `AUTH0_DOMAIN`: `dev-boostedbudget.us.auth0.com`
   - `AUTH0_CLIENT_ID`: (M2M client ID)
   - `AUTH0_CLIENT_SECRET`: (M2M client secret)
   - `AUTH0_AUDIENCE`: `https://api-staging.boostedbudget.com`

#### Create Frontend SPA Application

1. **Applications** → **Create Application**
2. Name: `BoostedBudget Frontend (Staging)`
3. Type: **Single Page Application**
4. Settings:
   - **Allowed Callback URLs**:
     - `http://localhost:3000/callback`
     - `http://mac-mini.tailnet-name.ts.net:3000/callback` (update after Tailscale setup)
   - **Allowed Logout URLs**:
     - `http://localhost:3000`
     - `http://mac-mini.tailnet-name.ts.net:3000`
   - **Allowed Web Origins**:
     - `http://localhost:3000`
     - `http://mac-mini.tailnet-name.ts.net:3000`
5. Note credentials:
   - `VITE_AUTH0_CLIENT_ID`: (SPA client ID)

### 2.2 Get Plaid Sandbox Credentials

**✅ Already have**: Plaid sandbox credentials from initial setup.

Verify you have:
- `PLAID_CLIENT_ID`: Your Plaid client ID
- `PLAID_SECRET`: Your Plaid sandbox secret

### 2.3 Get Stripe Test Mode Keys

1. Go to https://dashboard.stripe.com/test
2. Click **Developers** → **API keys**
3. Copy keys:
   - **Publishable key**: `pk_test_...`
   - **Secret key**: `sk_test_...`
4. Create webhook endpoint (optional for staging):
   - **Developers** → **Webhooks** → **Add endpoint**
   - URL: `http://mac-mini.tailnet-name.ts.net:8080/webhooks/stripe`
   - Events: Select relevant events
   - Copy **Signing secret**: `whsec_test_...`

### 2.4 Generate Encryption Key

```bash
# Generate 32-byte encryption key for staging
openssl rand -base64 24 | tr -d '=' | head -c 32
```

Save this key - you'll use it for `ENCRYPTION_KEY_STAGING`.

### 2.5 Configure GitHub Secrets

Add these secrets to your repository:

1. Go to **GitHub repo** → **Settings** → **Secrets and variables** → **Actions**
2. Click **New repository secret**
3. Add each secret:

| Secret Name | Value | Source |
|-------------|-------|--------|
| `AUTH0_DOMAIN_STAGING` | `dev-boostedbudget.us.auth0.com` | Auth0 dev tenant |
| `AUTH0_AUDIENCE_STAGING` | `https://api-staging.boostedbudget.com` | Auth0 API identifier |
| `AUTH0_CLIENT_ID_STAGING` | `your_m2m_client_id` | Auth0 Backend M2M app |
| `AUTH0_CLIENT_SECRET_STAGING` | `your_m2m_client_secret` | Auth0 Backend M2M app |
| `VITE_AUTH0_CLIENT_ID_STAGING` | `your_spa_client_id` | Auth0 Frontend SPA app |
| `PLAID_CLIENT_ID_STAGING` | `your_plaid_client_id` | Plaid dashboard |
| `PLAID_SECRET_STAGING` | `your_plaid_sandbox_secret` | Plaid dashboard |
| `STRIPE_PUBLISHABLE_KEY_TEST` | `pk_test_...` | Stripe test mode |
| `STRIPE_SECRET_KEY_TEST` | `sk_test_...` | Stripe test mode |
| `STRIPE_WEBHOOK_SECRET_TEST` | `whsec_test_...` | Stripe webhook (optional) |
| `ENCRYPTION_KEY_STAGING` | `32_character_key` | Generated above |
| `POSTGRES_PASSWORD_STAGING` | `your_secure_password` | Choose a strong password |
| `SESSION_SECRET_STAGING` | `$(openssl rand -base64 32)` | Generate random |
| `JWT_SECRET_STAGING` | `$(openssl rand -base64 32)` | Generate random |

---

## Phase 3: Initial Deployment

### 3.1 Create Local .env.staging File

On your Mac Mini:

```bash
cd /Users/yingyueliu/coding/BoostedBudget

# Copy template
cp .env.staging.example .env.staging

# Edit with your actual credentials
nano .env.staging
```

Fill in all values from Phase 2. **Important sections**:

- Auth0 credentials (from Auth0 dev tenant)
- Plaid sandbox credentials
- Stripe test mode keys
- Encryption key (32 characters)
- Postgres password

### 3.2 Update Tailscale URLs

After Tailscale is set up, update URLs in `.env.staging`:

```bash
# Get your Tailscale hostname
TAILSCALE_HOSTNAME=$(tailscale status | grep "$(hostname)" | awk '{print $1}')
echo "Your Tailscale hostname: $TAILSCALE_HOSTNAME"

# Update URLs in .env.staging
sed -i '' "s|mac-mini.tailnet-name.ts.net|$TAILSCALE_HOSTNAME|g" .env.staging
```

### 3.3 First Manual Deployment

Run the first deployment manually to verify everything works:

```bash
cd /Users/yingyueliu/coding/BoostedBudget

# Make sure you're on staging branch
git checkout staging
git pull origin staging

# Run deployment script
chmod +x ./scripts/deploy-staging.sh
./scripts/deploy-staging.sh
```

**Expected output**:
```
╔═══════════════════════════════════════════════════════════════╗
║     BoostedBudget Mac Mini Staging Deployment Script         ║
╚═══════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Checking Prerequisites
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Docker Compose found
✓ Environment file found: .env.staging
✓ Compose file found: docker-compose.staging.yml

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Building Docker Images Locally
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
...
✓ Images built successfully

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Starting Staging Containers
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Containers started

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Verifying Restart Policies
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ boostedbudget-db-staging: unless-stopped
✓ boostedbudget-redis-staging: unless-stopped
...
✓ All containers have correct restart policies

✓ Deployment completed successfully! 🚀
```

---

## Phase 4: Verification & Testing

### 4.1 Verify Containers Are Running

```bash
# Check all staging containers
docker-compose -f docker-compose.staging.yml ps

# Should show all services as "Up" with health status
```

Expected output:
```
NAME                           STATUS                   PORTS
boostedbudget-api-staging      Up (healthy)             0.0.0.0:8080->8080/tcp
boostedbudget-db-staging       Up (healthy)             0.0.0.0:5433->5432/tcp
boostedbudget-redis-staging    Up (healthy)             0.0.0.0:6380->6379/tcp
boostedbudget-rabbitmq-staging Up (healthy)             0.0.0.0:5673->5672/tcp, 0.0.0.0:15673->15672/tcp
boostedbudget-web-staging      Up                       0.0.0.0:3000->3000/tcp
```

### 4.2 Verify Restart Policies

```bash
# Check restart policies for all containers
docker inspect boostedbudget-db-staging boostedbudget-redis-staging boostedbudget-rabbitmq-staging boostedbudget-api-staging boostedbudget-web-staging \
  | grep -A 5 "RestartPolicy"
```

All should show:
```json
"RestartPolicy": {
    "Name": "unless-stopped",
    "MaximumRetryCount": 0
}
```

### 4.3 Test Health Endpoints

```bash
# Backend health check
curl http://localhost:8080/health

# Expected: {"status":"ok"} or similar

# Frontend accessibility
curl -I http://localhost:3000

# Expected: HTTP/1.1 200 OK
```

### 4.4 Test Via Tailscale

```bash
# Get your Tailscale hostname
TAILSCALE_HOSTNAME=$(tailscale status | grep "$(hostname)" | awk '{print $1}')

# Test backend via Tailscale
curl http://$TAILSCALE_HOSTNAME:8080/health

# Test frontend via Tailscale
curl http://$TAILSCALE_HOSTNAME:3000
```

### 4.5 Reboot Test (CRITICAL)

This verifies auto-start on boot:

```bash
# Before reboot - note running containers
docker ps | grep staging

# Reboot Mac Mini
sudo reboot
```

**After reboot** (wait ~2 minutes for Docker to start):

```bash
# Check containers auto-started
docker ps | grep staging

# All staging containers should be running again

# Test health endpoints
curl http://localhost:8080/health
curl http://localhost:3000
```

✅ **Success criteria**: All containers running without manual intervention

### 4.6 Test CI/CD Deployment

Push to staging branch to trigger automated deployment:

```bash
# Make a trivial change
echo "# Staging test" >> README.md
git add README.md
git commit -m "#162: Test staging deployment"
git push origin staging
```

Watch GitHub Actions:
1. Go to **GitHub repo** → **Actions**
2. Watch "Deploy to Staging" workflow
3. Should complete successfully with green checkmarks

---

## Usage & Access

### Local Access (Mac Mini)

- **Frontend**: http://localhost:3000
- **Backend API**: http://localhost:8080
- **RabbitMQ UI**: http://localhost:15673 (guest/guest)
- **Database**: `postgresql://staginguser:password@localhost:5433/boostedbudget_staging`

### Remote Access (Tailscale VPN)

From any device connected to your Tailscale network:

```bash
# Get Mac Mini's Tailscale hostname
HOSTNAME=mac-mini.tailnet-name.ts.net  # Replace with your actual hostname

# Access staging environment
Frontend:    http://$HOSTNAME:3000
Backend API: http://$HOSTNAME:8080
RabbitMQ UI: http://$HOSTNAME:15673
```

### Viewing Logs

```bash
# All services
docker-compose -f docker-compose.staging.yml logs -f

# Specific service
docker-compose -f docker-compose.staging.yml logs -f backend
docker-compose -f docker-compose.staging.yml logs -f frontend

# Last 100 lines
docker-compose -f docker-compose.staging.yml logs --tail=100 backend
```

### Managing Containers

```bash
# Stop staging environment
docker-compose -f docker-compose.staging.yml down

# Start staging environment
docker-compose -f docker-compose.staging.yml up -d

# Restart a service
docker-compose -f docker-compose.staging.yml restart backend

# View service status
docker-compose -f docker-compose.staging.yml ps
```

---

## Troubleshooting

### Issue: Tailscale daemon not running

**Symptoms**:
```
failed to connect to local Tailscale service; is Tailscale running?
```

**Solution**:
```bash
# Make sure you installed full app, not just CLI
brew uninstall tailscale
brew install --cask tailscale
open -a Tailscale
```

### Issue: Containers not auto-starting after reboot

**Symptoms**: Containers don't start automatically after Mac Mini reboot

**Diagnosis**:
```bash
# Check restart policies
docker inspect boostedbudget-db-staging | grep -A 5 "RestartPolicy"
```

**Solution**:
```bash
# Ensure Docker Desktop is set to start on login
# Docker Desktop → Settings → General → "Start Docker Desktop when you log in"

# Re-deploy to set correct restart policies
./scripts/deploy-staging.sh
```

### Issue: Port conflicts

**Symptoms**:
```
Error: Bind for 0.0.0.0:8080 failed: port is already allocated
```

**Solution**:
```bash
# Check what's using the port
lsof -i :8080

# Stop conflicting dev containers
docker-compose down

# Or change staging ports in docker-compose.staging.yml
```

### Issue: Health checks failing

**Symptoms**: Backend health check returns connection refused

**Diagnosis**:
```bash
# Check backend logs
docker-compose -f docker-compose.staging.yml logs backend

# Check if backend is running
docker-compose -f docker-compose.staging.yml ps backend
```

**Solution**:
```bash
# Restart backend
docker-compose -f docker-compose.staging.yml restart backend

# Check .env.staging has correct values
cat .env.staging | grep AUTH0_DOMAIN
```

### Issue: Database connection errors

**Symptoms**:
```
connection refused: localhost:5433
```

**Solution**:
```bash
# Check postgres is running
docker-compose -f docker-compose.staging.yml ps postgres

# Check postgres logs
docker-compose -f docker-compose.staging.yml logs postgres

# Verify credentials in .env.staging match docker-compose.staging.yml
```

### Issue: CI/CD deployment fails

**Symptoms**: GitHub Actions workflow fails on "Deploy to staging"

**Diagnosis**:
1. Check GitHub Actions logs
2. Look for specific error message

**Common issues**:
- **Missing secrets**: Add all required secrets to GitHub
- **GHCR login fails**: Check `GITHUB_TOKEN` permissions
- **Image pull fails**: Check network connectivity on Mac Mini

---

## Maintenance

### Regular Tasks

**Daily**: (Automated via CI/CD)
- Code changes pushed to staging → Auto-deploy

**Weekly**:
- Review container resource usage
- Check disk space
- Review logs for errors

**Monthly**:
- Update Docker images (`docker-compose pull`)
- Rotate credentials (if needed)
- Review and prune old Docker images

### Commands

```bash
# Check disk usage
docker system df

# Clean up old images (keeps last 24 hours)
docker system prune -a --filter "until=24h"

# View resource usage
docker stats

# Update images and restart
docker-compose -f docker-compose.staging.yml pull
docker-compose -f docker-compose.staging.yml up -d
```

### Backup & Recovery

**What to backup**:
- `.env.staging` file (contains secrets)
- Database data (if needed):
  ```bash
  docker exec boostedbudget-db-staging pg_dump -U staginguser boostedbudget_staging > backup.sql
  ```

**Recovery**:
```bash
# Restore database
docker exec -i boostedbudget-db-staging psql -U staginguser boostedbudget_staging < backup.sql
```

---

## Summary Checklist

- [ ] Tailscale full app installed and running
- [ ] Tailscale hostname obtained
- [ ] Auth0 dev tenant created
- [ ] Auth0 Backend M2M app configured
- [ ] Auth0 Frontend SPA app configured
- [ ] Plaid sandbox credentials verified
- [ ] Stripe test mode keys obtained
- [ ] Encryption key generated
- [ ] GitHub Secrets configured
- [ ] `.env.staging` created with all credentials
- [ ] First manual deployment successful
- [ ] All containers running with `unless-stopped` restart policy
- [ ] Health checks passing
- [ ] Reboot test passed (auto-start verified)
- [ ] CI/CD deployment tested
- [ ] Access via Tailscale verified

---

## Cost Summary

- **Hosting**: $0/month (self-hosted Mac Mini)
- **Tailscale**: $0/month (free tier)
- **Docker**: $0/month (Docker Desktop free for personal use)
- **Auth0**: $0/month (free dev tenant)
- **Plaid**: $0/month (sandbox mode)
- **Stripe**: $0/month (test mode)

**Total**: **$0/month** 🎉

---

## Next Steps

After staging is fully set up:

1. Test feature branches via staging before merging to main
2. Set up monitoring/alerting (optional)
3. Document staging→production promotion workflow
4. Consider setting up staging→production merge automation

For issues or questions, see:
- GitHub Issue #162
- CLAUDE.md (branching strategy)
- boosted-budget-complete-plan.md (architecture)
