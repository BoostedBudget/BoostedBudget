# Fly.io Setup Guide for BoostedBudget

This guide covers deploying BoostedBudget to Fly.io with custom domain configuration via Porkbun.

## Prerequisites

- [Fly.io account](https://fly.io/app/sign-up)
- [Fly CLI installed](https://fly.io/docs/hands-on/install-flyctl/)
- Porkbun account with boostedbudget.com domain
- Docker installed locally (for building images)
- Clerk, Plaid, and Stripe accounts configured

## Step 1: Install and Authenticate Fly CLI

```bash
# Install Fly CLI (macOS)
brew install flyctl

# Or install on Linux
curl -L https://fly.io/install.sh | sh

# Authenticate
flyctl auth login
```

## Step 2: Create Fly.io Applications

### Create Backend API Application

```bash
cd backend

# Create the app (use iad region for US East)
flyctl apps create boostedbudget-api --org personal

# IMPORTANT: Don't run fly launch, we already have fly.toml configured!
```

### Create Frontend Web Application

```bash
cd ../frontend

# Create the app
flyctl apps create boostedbudget-web --org personal
```

## Step 3: Set up PostgreSQL Database

```bash
# Create a Postgres cluster in the same region (iad)
flyctl postgres create \
  --name boostedbudget-db \
  --region iad \
  --vm-size shared-cpu-1x \
  --volume-size 10 \
  --initial-cluster-size 1

# Attach database to backend app
flyctl postgres attach boostedbudget-db --app boostedbudget-api

# This automatically sets DATABASE_URL secret
```

## Step 4: Configure Secrets

### Backend Secrets

```bash
cd backend

# Clerk secrets
flyctl secrets set \
  CLERK_SECRET_KEY="sk_live_your_clerk_secret_key" \
  --app boostedbudget-api

# Plaid secrets
flyctl secrets set \
  PLAID_CLIENT_ID="your_plaid_client_id" \
  PLAID_SECRET="your_plaid_secret" \
  --app boostedbudget-api

# Stripe secrets
flyctl secrets set \
  STRIPE_SECRET_KEY="your_stripe_secret_key" \
  STRIPE_WEBHOOK_SECRET="your_stripe_webhook_secret" \
  --app boostedbudget-api
```

### Frontend Secrets

```bash
cd ../frontend

# Clerk publishable key (safe to commit, but we set via secrets for flexibility)
flyctl secrets set \
  VITE_CLERK_PUBLISHABLE_KEY="pk_live_your_clerk_publishable_key" \
  --app boostedbudget-web

# Plaid Link public key
flyctl secrets set \
  VITE_PLAID_PUBLIC_KEY="your_plaid_public_key" \
  --app boostedbudget-web

# Google AdSense client ID (for free tier ads)
flyctl secrets set \
  VITE_GOOGLE_ADSENSE_CLIENT="ca-pub-XXXXXXXXXXXXXXXX" \
  --app boostedbudget-web
```

## Step 5: Deploy Applications

### Deploy Backend

```bash
cd backend

# First deployment
flyctl deploy

# Check deployment status
flyctl status --app boostedbudget-api

# View logs
flyctl logs --app boostedbudget-api

# Test health endpoint
curl https://boostedbudget-api.fly.dev/health
```

### Deploy Frontend

```bash
cd ../frontend

# First deployment
flyctl deploy

# Check deployment status
flyctl status --app boostedbudget-web

# View logs
flyctl logs --app boostedbudget-web

# Test in browser
open https://boostedbudget-web.fly.dev
```

## Step 6: Configure Custom Domains (Porkbun)

### DNS Configuration in Porkbun

1. **Log into Porkbun** at https://porkbun.com/account/domainsSpeedy
2. **Click on your domain** `boostedbudget.com`
3. **Navigate to DNS Records**

### Backend API Domain (api.boostedbudget.com)

```bash
# Get Fly.io IP addresses for backend
flyctl ips list --app boostedbudget-api

# You'll see something like:
# IPv4: 66.241.124.123
# IPv6: 2a09:8280:1::1:abcd
```

**Add these DNS records in Porkbun:**

| Type  | Host | Answer/Value              | TTL  |
|-------|------|---------------------------|------|
| A     | api  | 66.241.124.123            | 600  |
| AAAA  | api  | 2a09:8280:1::1:abcd       | 600  |

**Configure SSL certificate in Fly.io:**

```bash
# Add custom domain to backend
flyctl certs create api.boostedbudget.com --app boostedbudget-api

# Check certificate status (may take a few minutes)
flyctl certs show api.boostedbudget.com --app boostedbudget-api
```

### Frontend Domain (www.boostedbudget.com and boostedbudget.com)

```bash
# Get Fly.io IP addresses for frontend
flyctl ips list --app boostedbudget-web
```

**Add these DNS records in Porkbun:**

| Type  | Host          | Answer/Value              | TTL  |
|-------|---------------|---------------------------|------|
| A     | @             | 66.241.125.234            | 600  |
| AAAA  | @             | 2a09:8280:1::2:efgh       | 600  |
| A     | www           | 66.241.125.234            | 600  |
| AAAA  | www           | 2a09:8280:1::2:efgh       | 600  |

**Configure SSL certificates in Fly.io:**

```bash
# Add root domain
flyctl certs create boostedbudget.com --app boostedbudget-web

# Add www subdomain
flyctl certs create www.boostedbudget.com --app boostedbudget-web

# Check certificate status
flyctl certs list --app boostedbudget-web
```

### DNS Propagation

DNS changes can take 5 minutes to 48 hours to propagate globally. Check status:

```bash
# Check DNS propagation
dig api.boostedbudget.com
dig boostedbudget.com
dig www.boostedbudget.com

# Or use online tool
# https://www.whatsmydns.net/#A/api.boostedbudget.com
```

## Step 7: Update Application Configuration

### Update Frontend Environment Variable

Once DNS is configured, update the API URL:

```bash
# Update VITE_API_URL to use custom domain
flyctl secrets set \
  VITE_API_URL="https://api.boostedbudget.com/v1" \
  --app boostedbudget-web

# Redeploy to pick up new URL
cd apps/web
flyctl deploy
```

### Update Clerk Configuration

1. Log into Clerk Dashboard (https://dashboard.clerk.com/)
2. Navigate to your application → **Configure** → **Paths**
3. Update allowed origins to include:
   - `https://boostedbudget.com`
   - `https://www.boostedbudget.com`
   - `http://localhost:5173`

## Step 8: Verify Deployment

### Backend Verification

```bash
# Health check
curl https://api.boostedbudget.com/health

# Expected response:
# {"status":"healthy","timestamp":"2025-10-18T..."}

# Metrics endpoint
curl https://api.boostedbudget.com/metrics
```

### Frontend Verification

```bash
# Open in browser
open https://boostedbudget.com

# Verify:
# 1. Page loads correctly
# 2. HTTPS certificate is valid
# 3. Clerk login works
# 4. Cookie consent banner appears
# 5. API calls work (check browser console)
```

## Step 9: Set up Monitoring

```bash
# View backend metrics
flyctl dashboard --app boostedbudget-api

# View frontend metrics
flyctl dashboard --app boostedbudget-web

# Set up alerts (optional)
flyctl monitor --app boostedbudget-api
```

## Useful Commands

### Viewing Logs

```bash
# Live tail logs
flyctl logs --app boostedbudget-api
flyctl logs --app boostedbudget-web

# Recent logs
flyctl logs --app boostedbudget-api --lines 100
```

### Scaling

```bash
# Scale backend
flyctl scale count 2 --app boostedbudget-api
flyctl scale vm shared-cpu-2x --app boostedbudget-api

# Scale frontend
flyctl scale count 2 --app boostedbudget-web
```

### Database Management

```bash
# Connect to database
flyctl postgres connect --app boostedbudget-db

# Run migrations manually
flyctl ssh console --app boostedbudget-api
/app/server migrate

# Backup database
flyctl postgres backup create --app boostedbudget-db
```

### Deployments

```bash
# Deploy specific version
flyctl deploy --image ghcr.io/maxnussbaum/boostedbudget-backend:v1.2.3

# Rollback to previous version
flyctl releases --app boostedbudget-api
flyctl releases rollback <release-id> --app boostedbudget-api
```

## Troubleshooting

### Certificate Issues

```bash
# Check certificate status
flyctl certs check api.boostedbudget.com --app boostedbudget-api

# Remove and recreate certificate
flyctl certs delete api.boostedbudget.com --app boostedbudget-api
flyctl certs create api.boostedbudget.com --app boostedbudget-api
```

### DNS Not Resolving

```bash
# Verify DNS records
dig A api.boostedbudget.com
dig AAAA api.boostedbudget.com

# Check Porkbun DNS settings
# Ensure TTL is set correctly (600 seconds recommended)
```

### Health Check Failing

```bash
# Check application logs
flyctl logs --app boostedbudget-api

# SSH into machine
flyctl ssh console --app boostedbudget-api

# Manually test health endpoint
curl http://localhost:8080/health
```

### Database Connection Issues

```bash
# Verify DATABASE_URL is set
flyctl secrets list --app boostedbudget-api

# Test database connection
flyctl ssh console --app boostedbudget-api
psql $DATABASE_URL
```

## Cost Estimation

- **Backend API**: ~$5-10/month (shared-cpu-1x, 512MB RAM)
- **Frontend Web**: ~$3-5/month (shared-cpu-1x, 256MB RAM)
- **PostgreSQL**: ~$0/month (free tier) or ~$5/month (10GB volume)
- **Bandwidth**: Free for first 160GB/month

**Total**: ~$8-20/month for MVP deployment

## Security Checklist

- [ ] All secrets set via `flyctl secrets set` (never in code)
- [ ] HTTPS enforced (force_https = true in fly.toml)
- [ ] Health checks configured
- [ ] Auto-rollback enabled
- [ ] Non-root user in Docker images
- [ ] PostgreSQL not publicly accessible
- [ ] Clerk allowed origins restricted to production domain
- [ ] CORS configured properly in backend

## Next Steps

After deployment:

1. Set up GitHub Actions CI/CD (Issue #9)
2. Create deployment scripts (Issue #52)
3. Set up E2E testing against production (Issue #12)
4. Configure monitoring alerts
5. Set up backup strategy for database

## References

- [Fly.io Documentation](https://fly.io/docs/)
- [Fly.io Postgres](https://fly.io/docs/postgres/)
- [Fly.io Custom Domains](https://fly.io/docs/app-guides/custom-domains-with-fly/)
- [Porkbun DNS](https://kb.porkbun.com/category/3-dns)
