# BoostedBudget Secrets Reference

This document lists all required secrets and environment variables for deployment.

## Backend Secrets (boostedbudget-api)

Set via: `flyctl secrets set KEY=value --app boostedbudget-api`

### Database

| Secret | Description | Example | Required |
|--------|-------------|---------|----------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://user:pass@host:5432/db` | ✅ Yes |

**Note**: Automatically set when attaching Fly Postgres with `flyctl postgres attach`

### Auth0

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `AUTH0_CLIENT_SECRET` | Auth0 M2M client secret | Auth0 Dashboard → Applications → BoostedBudget Backend API → Settings | ✅ Yes |

### Plaid

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `PLAID_CLIENT_ID` | Plaid client ID | Plaid Dashboard → Team Settings → Keys | ✅ Yes |
| `PLAID_SECRET` | Plaid secret (sandbox or production) | Plaid Dashboard → Team Settings → Keys | ✅ Yes |

### Stripe

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `STRIPE_SECRET_KEY` | Stripe secret key | Stripe Dashboard → Developers → API Keys | ✅ Yes |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret | Stripe Dashboard → Developers → Webhooks | ✅ Yes |

### Backend Environment Variables (Non-Secret)

Set in `backend/fly.toml`:

```toml
[env]
  PORT = "8080"
  PRIMARY_REGION = "iad"
  ENVIRONMENT = "production"
  LOG_LEVEL = "info"
  ENABLE_METRICS = "true"
  AUTH0_DOMAIN = "boostedbudget.us.auth0.com"
  AUTH0_AUDIENCE = "https://api.boostedbudget.com"
  PLAID_ENV = "production"
```

## Frontend Secrets (boostedbudget-web)

Set via: `flyctl secrets set KEY=value --app boostedbudget-web`

### Auth0 (Public Client)

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `VITE_AUTH0_CLIENT_ID` | Auth0 SPA client ID | Auth0 Dashboard → Applications → BoostedBudget Frontend → Settings | ✅ Yes |

**Note**: This is a public identifier (safe to expose in frontend JS), but we set it via secrets for deployment flexibility.

### Plaid

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `VITE_PLAID_PUBLIC_KEY` | Plaid Link public key | Plaid Dashboard → Team Settings → Keys | ✅ Yes |

### Google AdSense

| Secret | Description | Where to Find | Required |
|--------|-------------|---------------|----------|
| `VITE_GOOGLE_ADSENSE_CLIENT` | AdSense publisher ID | Google AdSense → Account → Settings | 🟡 Yes (for free tier) |

**Format**: `ca-pub-XXXXXXXXXXXXXXXX`

### Frontend Environment Variables (Non-Secret)

Set in `frontend/fly.toml`:

```toml
[env]
  PORT = "80"
  ENVIRONMENT = "production"
  VITE_API_URL = "https://api.boostedbudget.com/v1"
  VITE_AUTH0_DOMAIN = "boostedbudget.us.auth0.com"
  VITE_AUTH0_AUDIENCE = "https://api.boostedbudget.com"
```

## Local Development (.env)

For local development, create `.env` files:

### backend/.env

```bash
# Database
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/boostedbudget

# Auth0
AUTH0_DOMAIN=boostedbudget.us.auth0.com
AUTH0_AUDIENCE=https://api.boostedbudget.com
AUTH0_CLIENT_SECRET=your_dev_secret

# Plaid (Sandbox)
PLAID_CLIENT_ID=your_plaid_client_id
PLAID_SECRET=your_plaid_sandbox_secret
PLAID_ENV=sandbox

# Stripe (Test Mode)
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# Application
PORT=8080
ENVIRONMENT=development
LOG_LEVEL=debug
ENABLE_METRICS=true
PRIMARY_REGION=us-east-1
```

### frontend/.env.local

```bash
# API
VITE_API_URL=http://localhost:8080/v1

# Auth0
VITE_AUTH0_DOMAIN=boostedbudget.us.auth0.com
VITE_AUTH0_CLIENT_ID=your_dev_client_id
VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com

# Plaid (Sandbox)
VITE_PLAID_PUBLIC_KEY=your_plaid_public_key

# Google AdSense (optional for development)
VITE_GOOGLE_ADSENSE_CLIENT=ca-pub-XXXXXXXXXXXXXXXX
```

## How to Obtain Secrets

### Auth0

1. Log into [Auth0 Dashboard](https://manage.auth0.com)
2. Navigate to **Applications** → **Applications**
3. Create two applications:
   - **BoostedBudget Backend** (Machine to Machine)
   - **BoostedBudget Frontend** (Single Page Application)
4. Copy client IDs and secrets from Settings

### Plaid

1. Sign up at [Plaid Dashboard](https://dashboard.plaid.com/)
2. Navigate to **Team Settings** → **Keys**
3. Use **Sandbox** keys for development
4. Use **Production** keys for production (requires verification)

### Stripe

1. Sign up at [Stripe Dashboard](https://dashboard.stripe.com/)
2. Navigate to **Developers** → **API Keys**
3. Use **Test mode** for development
4. Use **Live mode** for production
5. Set up webhook endpoint and get signing secret

### Google AdSense

1. Sign up at [Google AdSense](https://www.google.com/adsense/)
2. Get approved for AdSense account
3. Navigate to **Account** → **Settings**
4. Copy your Publisher ID (ca-pub-...)

## Security Best Practices

✅ **DO:**
- Use `flyctl secrets set` for all sensitive values
- Rotate secrets regularly
- Use different secrets for dev/staging/production
- Keep `.env` files in `.gitignore`
- Use Auth0 for all authentication (never custom auth)
- Use Stripe test mode keys in development

❌ **DON'T:**
- Commit secrets to git
- Share secrets via insecure channels
- Use production secrets in development
- Hard-code secrets in source code
- Log secret values

## Verifying Secrets

### Check what secrets are set

```bash
# Backend
flyctl secrets list --app boostedbudget-api

# Frontend
flyctl secrets list --app boostedbudget-web
```

**Note**: Values are hidden for security. You'll only see secret names.

### Test secrets are working

```bash
# Backend - check health endpoint
curl https://api.boostedbudget.com/health

# Backend - test database connection
flyctl ssh console --app boostedbudget-api
echo $DATABASE_URL

# Frontend - check in browser console
# Open https://boostedbudget.com
# Check: window.env or process.env values are injected
```

## Rotating Secrets

When rotating secrets (recommended every 90 days):

```bash
# Example: Rotate Auth0 client secret
# 1. Generate new secret in Auth0 Dashboard
# 2. Update Fly.io secret
flyctl secrets set AUTH0_CLIENT_SECRET=new_secret --app boostedbudget-api

# 3. Fly will automatically redeploy with new secret
# 4. Verify deployment works
curl https://api.boostedbudget.com/health

# 5. Delete old secret from Auth0
```

## Emergency Secret Removal

If a secret is compromised:

```bash
# 1. Immediately unset the secret
flyctl secrets unset SECRET_NAME --app app-name

# 2. Generate new secret from provider
# 3. Set new secret
flyctl secrets set SECRET_NAME=new_value --app app-name

# 4. Rotate related credentials
# 5. Review access logs for unauthorized usage
```

## Secrets Checklist for Deployment

Before first deployment:

- [ ] Auth0 applications created (Backend M2M + Frontend SPA)
- [ ] Plaid account created and keys obtained
- [ ] Stripe account created and webhooks configured
- [ ] Google AdSense account approved (for free tier)
- [ ] PostgreSQL database created on Fly.io
- [ ] All backend secrets set via `flyctl secrets set`
- [ ] All frontend secrets set via `flyctl secrets set`
- [ ] Auth0 callback URLs updated with production domains
- [ ] Stripe webhooks pointed to production API
- [ ] Secrets documented in team password manager
- [ ] Local `.env` files created (not committed)

## Support

If you have issues with secrets:

1. Check secret is set: `flyctl secrets list`
2. Check application logs: `flyctl logs`
3. Verify environment in provider dashboards
4. Test locally with same secrets to isolate issue

## References

- [Fly.io Secrets Documentation](https://fly.io/docs/reference/secrets/)
- [Auth0 Applications](https://auth0.com/docs/get-started/auth0-overview/create-applications)
- [Plaid Keys](https://plaid.com/docs/auth/partnerships/https://plaid.com/docs/api/tokens/#keys)
- [Stripe API Keys](https://stripe.com/docs/keys)
- [Google AdSense Setup](https://support.google.com/adsense/answer/10162)
