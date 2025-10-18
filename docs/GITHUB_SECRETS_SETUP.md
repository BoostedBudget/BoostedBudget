# GitHub Secrets Setup for BoostedBudget

Quick reference guide for configuring GitHub repository secrets for CI/CD and deployments.

## Prerequisites

Before setting up GitHub secrets, you must have:
- ✅ Completed [Auth0 Setup](./AUTH0_SETUP.md)
- ✅ Created Plaid developer account
- ✅ Created Stripe account
- ✅ (Optional) Set up Google AdSense

---

## Adding Secrets to GitHub

### Navigate to Secrets Page
1. Go to: `https://github.com/maxnussbaum/BoostedBudget`
2. Click **Settings** (repository settings)
3. Click **Secrets and variables** → **Actions**
4. Click **New repository secret**

---

## Required Secrets

### Auth0 Secrets (Priority: HIGH)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `AUTH0_DOMAIN` | Auth0 Dashboard → Applications → BoostedBudget Web App → Settings → Domain | `boostedbudget.auth0.com` |
| `AUTH0_AUDIENCE` | Auth0 Dashboard → APIs → BoostedBudget API → Settings → Identifier | `https://api.boostedbudget.com` |
| `AUTH0_CLIENT_ID` | Auth0 Dashboard → Applications → BoostedBudget Web App → Settings → Client ID | `abc123def456...` |
| `AUTH0_CLIENT_SECRET` | Auth0 Dashboard → Applications → BoostedBudget Web App → Settings → Client Secret | `xyz789...` (click to reveal) |

**Setup Instructions**: See [AUTH0_SETUP.md](./AUTH0_SETUP.md) Section 7

---

### Database Secrets (Priority: HIGH)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `DATABASE_URL` | Fly.io Postgres or your database provider | `postgres://user:pass@host:5432/db?sslmode=require` |
| `REDIS_URL` | Fly.io Redis or your Redis provider | `redis://default:password@host:6379` |

**Note**: These will be set up when configuring Fly.io (Issue #10)

---

### Plaid Secrets (Priority: MEDIUM)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `PLAID_CLIENT_ID` | Plaid Dashboard → Team Settings → Keys → Client ID | `5f9a...` |
| `PLAID_SECRET` | Plaid Dashboard → Team Settings → Keys → Sandbox Secret | `abc123...` |
| `PLAID_ENV` | Manual configuration | `sandbox` (for development) |

**Where**: https://dashboard.plaid.com/developers/keys

---

### Stripe Secrets (Priority: MEDIUM)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `STRIPE_SECRET_KEY` | Stripe Dashboard → Developers → API Keys → Secret key (Test mode) | `sk_test_...` |
| `STRIPE_PUBLISHABLE_KEY` | Stripe Dashboard → Developers → API Keys → Publishable key (Test mode) | `pk_test_...` |
| `STRIPE_WEBHOOK_SECRET` | Stripe Dashboard → Developers → Webhooks → Add endpoint → Signing secret | `whsec_...` |

**Where**: https://dashboard.stripe.com/test/apikeys

---

### Google AdSense Secrets (Priority: LOW - Free Tier Only)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `GOOGLE_ADSENSE_CLIENT` | AdSense Dashboard → Account → Account Information → Publisher ID | `ca-pub-1234567890` |

**Where**: https://www.google.com/adsense/

**Note**: Only needed for free tier ad display. Can be added later in Phase 5.

---

### Fly.io Secrets (Priority: HIGH - For Deployment)

| Secret Name | Where to Find | Example Value |
|-------------|---------------|---------------|
| `FLY_API_TOKEN` | Fly.io Dashboard → Account → Access Tokens → Create Token | `fo1_...` |

**Where**: https://fly.io/user/personal_access_tokens

**Note**: Required for CI/CD automatic deployments (Issue #9)

---

## Step-by-Step: Adding a Secret

For each secret in the tables above:

1. Click **New repository secret**
2. **Name**: Enter the exact secret name (e.g., `AUTH0_DOMAIN`)
3. **Secret**: Paste the value from your provider dashboard
4. Click **Add secret**

**Example**:
```
Name: AUTH0_DOMAIN
Secret: boostedbudget.auth0.com
```

---

## Verifying Secrets

After adding all secrets, your GitHub Actions secrets page should show:

### Phase 0 (Immediate - Required for Auth0):
- ✅ `AUTH0_DOMAIN`
- ✅ `AUTH0_AUDIENCE`
- ✅ `AUTH0_CLIENT_ID`
- ✅ `AUTH0_CLIENT_SECRET`

### Phase 0 (Deployment):
- ✅ `DATABASE_URL` (added during Fly.io setup)
- ✅ `REDIS_URL` (added during Fly.io setup)
- ✅ `FLY_API_TOKEN` (added during CI/CD setup)

### Phase 2 (Plaid Integration):
- ⏳ `PLAID_CLIENT_ID`
- ⏳ `PLAID_SECRET`
- ⏳ `PLAID_ENV`

### Phase 5 (Monetization):
- ⏳ `STRIPE_SECRET_KEY`
- ⏳ `STRIPE_PUBLISHABLE_KEY`
- ⏳ `STRIPE_WEBHOOK_SECRET`
- ⏳ `GOOGLE_ADSENSE_CLIENT`

---

## Using Secrets in GitHub Actions

Secrets are automatically available in GitHub Actions workflows:

```yaml
# .github/workflows/deploy.yml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to Fly.io
        env:
          AUTH0_DOMAIN: ${{ secrets.AUTH0_DOMAIN }}
          AUTH0_AUDIENCE: ${{ secrets.AUTH0_AUDIENCE }}
          AUTH0_CLIENT_ID: ${{ secrets.AUTH0_CLIENT_ID }}
          AUTH0_CLIENT_SECRET: ${{ secrets.AUTH0_CLIENT_SECRET }}
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
        run: |
          fly deploy
```

---

## Security Best Practices

✅ **DO**:
- Use separate credentials for dev/staging/production
- Rotate secrets regularly (every 90 days)
- Use test/sandbox keys for development
- Limit secret access to necessary personnel
- Enable secret scanning in repository settings

❌ **DON'T**:
- Share secrets via email/Slack/chat
- Commit secrets to git (even in .env files)
- Use production keys in development
- Store secrets in code comments
- Log secret values in CI/CD outputs

---

## Updating Secrets

To update an existing secret:
1. Go to repository **Settings** → **Secrets and variables** → **Actions**
2. Click on the secret name
3. Click **Update secret**
4. Enter new value
5. Click **Update secret**

**Note**: You cannot view existing secret values, only update them.

---

## Deleting Secrets

To delete a secret:
1. Go to repository **Settings** → **Secrets and variables** → **Actions**
2. Click on the secret name
3. Click **Remove secret**
4. Confirm deletion

---

## Troubleshooting

### Secret Not Available in Workflow
**Problem**: GitHub Action can't access secret
**Solution**:
- Check secret name matches exactly (case-sensitive)
- Verify workflow has permission to access secrets
- Check if running on fork (secrets not available in PRs from forks)

### Invalid Secret Value
**Problem**: Auth0/Plaid/Stripe returns authentication error
**Solution**:
- Verify you copied the complete value (no trailing spaces)
- Ensure using correct environment (sandbox vs production)
- Check secret hasn't expired or been rotated

---

## Quick Reference Commands

### Test Auth0 Configuration
```bash
# Verify Auth0 domain is accessible
curl https://YOUR_AUTH0_DOMAIN/.well-known/openid-configuration
```

### Test Plaid Configuration
```bash
# Verify Plaid credentials (requires API call)
curl -X POST https://sandbox.plaid.com/item/public_token/create \
  -H 'Content-Type: application/json' \
  -d '{
    "client_id": "YOUR_PLAID_CLIENT_ID",
    "secret": "YOUR_PLAID_SECRET",
    "institution_id": "ins_3",
    "initial_products": ["transactions"]
  }'
```

### Test Stripe Configuration
```bash
# Verify Stripe secret key
curl https://api.stripe.com/v1/customers \
  -u YOUR_STRIPE_SECRET_KEY: \
  -d "email=test@example.com"
```

---

## Next Steps

After configuring GitHub secrets:
1. ✅ Secrets are ready for CI/CD workflows (Issue #9)
2. ✅ Backend can use Auth0 for JWT validation (Issue #5)
3. ✅ Frontend can integrate Auth0 login (Issue #6)
4. ✅ Deployments will have access to credentials

---

**Last Updated**: 2025-10-18
**Issue**: #3
**Related Docs**: [AUTH0_SETUP.md](./AUTH0_SETUP.md)
