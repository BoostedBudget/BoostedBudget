# Plaid Webhook Testing Guide (ELI5)

## What Are Webhooks?

Think of webhooks like a doorbell. Instead of you constantly checking your door to see if someone is there (polling), the doorbell rings when someone arrives (webhook notification).

Plaid webhooks notify BoostedBudget when:
- New transactions are available from a user's bank
- A bank connection needs re-authentication
- Account information has changed

## Prerequisites

Before testing webhooks, you need:

1. **Plaid Sandbox Account** - Sign up at https://dashboard.plaid.com/signup
2. **Plaid Credentials** - Get your `client_id` and `sandbox` secret from dashboard
3. **BoostedBudget Running** - Backend deployed to a publicly accessible URL

## Part 1: Environment Setup

### Staging Environment Setup

**Step 1: Set Webhook URL in Fly.io Staging**

```bash
# Set the webhook URL environment variable
fly secrets set PLAID_WEBHOOK_URL="https://staging.boostedbudget.com/webhooks/plaid" -a boostedbudget-staging

# Verify it's set
fly secrets list -a boostedbudget-staging
```

**Step 2: Register Webhook URL in Plaid Dashboard**

1. Go to https://dashboard.plaid.com/team/api
2. Scroll to **"Allowed redirect URIs"** section (webhooks are below this)
3. Add staging webhook URL: `https://staging.boostedbudget.com/webhooks/plaid`
4. Click **"Save Changes"**

⚠️ **Important:** The webhook URL MUST be:
- HTTPS (not HTTP)
- Publicly accessible (Plaid servers must reach it)
- Registered in Plaid Dashboard (or webhooks will be rejected)

**Step 3: Set Other Required Environment Variables**

```bash
# These should already be set, but verify:
fly secrets list -a boostedbudget-staging

# Required variables for webhook verification:
# - PLAID_CLIENT_ID (your Plaid client ID)
# - PLAID_SECRET (your sandbox secret)
# - PLAID_ENV=sandbox
# - DATABASE_URL (for storing webhook events)
# - ENCRYPTION_KEY (for encrypting access tokens)
```

**Step 4: Deploy to Staging**

```bash
# Deploy the new webhook code
cd /home/max/coding/BoostedBudget
./scripts/deploy-to-fly.sh staging

# Watch logs to verify webhook endpoint is registered
fly logs -a boostedbudget-staging | grep webhook
```

You should see in logs:
```
✅ Plaid webhook endpoint registered at POST /webhooks/plaid
✅ Webhook signature verification enabled (ES256 JWT)
✅ Webhook audit trail enabled (database storage)
```

### Production Environment Setup

**Step 1: Set Webhook URL in Fly.io Production**

```bash
# Set the webhook URL environment variable
fly secrets set PLAID_WEBHOOK_URL="https://api.boostedbudget.com/webhooks/plaid" -a boostedbudget-production

# Verify it's set
fly secrets list -a boostedbudget-production
```

**Step 2: Register Webhook URL in Plaid Dashboard**

1. Switch to **Production** environment in Plaid Dashboard (top right dropdown)
2. Go to https://dashboard.plaid.com/team/api
3. Add production webhook URL: `https://api.boostedbudget.com/webhooks/plaid`
4. Click **"Save Changes"**

**Step 3: Update PLAID_ENV and PLAID_SECRET**

```bash
# Production uses different secret than sandbox
fly secrets set PLAID_ENV="production" -a boostedbudget-production
fly secrets set PLAID_SECRET="<your-production-secret>" -a boostedbudget-production

# Verify
fly secrets list -a boostedbudget-production
```

**Step 4: Deploy to Production**

```bash
# Deploy the new webhook code
./scripts/deploy-to-fly.sh production

# Watch logs
fly logs -a boostedbudget-production | grep webhook
```

## Part 2: Testing Webhooks in Sandbox

### Method 1: Automatic Webhook (Easiest)

When you connect a bank account in sandbox, Plaid automatically sends webhooks:

**Step 1: Connect a Sandbox Bank Account**

1. Start BoostedBudget frontend (staging environment)
2. Click "Connect Bank Account"
3. Choose **"First Platypus Bank"** (sandbox test bank)
4. Username: `user_good`
5. Password: `pass_good`
6. Complete the Link flow

**Step 2: Wait for Webhook**

Plaid will automatically send webhooks within 5-30 seconds:
- `ITEM` webhook with code `WEBHOOK_UPDATE_ACKNOWLEDGED`
- `TRANSACTIONS` webhook with code `INITIAL_UPDATE` (historical transactions ready)

**Step 3: Verify Webhook Receipt**

```bash
# Check backend logs
fly logs -a boostedbudget-staging | grep "Received Plaid webhook"

# You should see:
# Received Plaid webhook: type=ITEM code=WEBHOOK_UPDATE_ACKNOWLEDGED item_id=<id> signature_valid=true
# Received Plaid webhook: type=TRANSACTIONS code=INITIAL_UPDATE item_id=<id> signature_valid=true
```

**Step 4: Check Database Storage**

```bash
# Connect to staging database
fly postgres connect -a boostedbudget-db-staging

# Query recent webhooks
SELECT
  id,
  webhook_type,
  webhook_code,
  signature_valid,
  processed,
  created_at
FROM webhook_events
ORDER BY created_at DESC
LIMIT 10;

# Exit database
\q
```

### Method 2: Manual Webhook Trigger (Full Control)

Use Plaid's sandbox API to fire test webhooks manually.

**Step 1: Get an Item ID**

You need a Plaid `item_id` from a connected account:

```bash
# Connect to staging database
fly postgres connect -a boostedbudget-db-staging

# Get item_id from linked_items table
SELECT item_id, institution_name FROM linked_items ORDER BY created_at DESC LIMIT 5;

# Copy the item_id (looks like: "item_abc123xyz...")
\q
```

**Step 2: Fire a Test Webhook**

Use this curl command to manually trigger a webhook:

```bash
# Replace <YOUR_CLIENT_ID>, <YOUR_SANDBOX_SECRET>, and <ITEM_ID>
curl -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "client_id": "<YOUR_CLIENT_ID>",
    "secret": "<YOUR_SANDBOX_SECRET>",
    "access_token": "<ACCESS_TOKEN>",
    "webhook_code": "DEFAULT_UPDATE"
  }'
```

**Available Webhook Codes:**

**TRANSACTIONS Webhooks:**
- `DEFAULT_UPDATE` - New transactions available
- `INITIAL_UPDATE` - Historical transactions ready
- `HISTORICAL_UPDATE` - Historical data updated
- `TRANSACTIONS_REMOVED` - Transactions were removed

**ITEM Webhooks:**
- `ERROR` - Item encountered an error
- `PENDING_EXPIRATION` - Item access expiring soon
- `WEBHOOK_UPDATE_ACKNOWLEDGED` - Webhook URL acknowledged

**Step 3: Verify Receipt**

```bash
# Check logs immediately after firing webhook
fly logs -a boostedbudget-staging --tail

# Look for:
# Received Plaid webhook: type=TRANSACTIONS code=DEFAULT_UPDATE
# Webhook signature verified successfully
# Webhook stored in database: id=<uuid>
```

**Step 4: Check Database**

```bash
fly postgres connect -a boostedbudget-db-staging

# Query the most recent webhook
SELECT * FROM webhook_events ORDER BY created_at DESC LIMIT 1;

# Check signature validation
SELECT
  webhook_type,
  webhook_code,
  signature_valid,
  error_code,
  created_at
FROM webhook_events
ORDER BY created_at DESC
LIMIT 5;
```

### Method 3: Using Plaid Postman Collection

**Step 1: Import Plaid Postman Collection**

1. Download from: https://www.postman.com/plaid/workspace/plaid-api/overview
2. Import into Postman
3. Set up environment variables:
   - `client_id`: Your Plaid client ID
   - `secret`: Your sandbox secret
   - `env`: `sandbox`

**Step 2: Fire Webhook via Postman**

1. Navigate to: **Sandbox** → **Fire Webhook**
2. Update request body with your `access_token` and desired `webhook_code`
3. Click **Send**
4. Check BoostedBudget logs for webhook receipt

## Part 3: Troubleshooting

### Problem: No Webhooks Received

**Check 1: Is Webhook URL Set?**
```bash
fly secrets list -a boostedbudget-staging | grep PLAID_WEBHOOK_URL
```

**Check 2: Is URL Registered in Plaid Dashboard?**
- Go to https://dashboard.plaid.com/team/api
- Verify your URL is in the list
- Try removing and re-adding it

**Check 3: Is Backend Running?**
```bash
fly status -a boostedbudget-staging
fly logs -a boostedbudget-staging | grep "Plaid webhook endpoint"
```

**Check 4: Test URL is Accessible**
```bash
# Test from external location (not localhost)
curl -X POST https://staging.boostedbudget.com/webhooks/plaid \
  -H 'Content-Type: application/json' \
  -d '{"webhook_type":"TEST","webhook_code":"TEST"}'

# Should return: {"status":"ok","message":"Webhook received"}
```

### Problem: Signature Verification Failing

**Check 1: Are Plaid Credentials Set?**
```bash
fly secrets list -a boostedbudget-staging | grep PLAID
```

**Check 2: Is PLAID_ENV Correct?**
```bash
# For staging, should be "sandbox"
fly secrets list -a boostedbudget-staging | grep PLAID_ENV
```

**Check 3: Check Logs for Verification Errors**
```bash
fly logs -a boostedbudget-staging | grep "signature verification failed"
```

Common signature verification errors:
- `failed to fetch JWK` - Network issue reaching Plaid API
- `invalid signature` - JWT signature doesn't match (check client_id/secret)
- `token used before issued` - Clock skew issue
- `body hash mismatch` - Body was modified before verification

### Problem: Webhooks Stored But Not Processing

**Check Database:**
```bash
fly postgres connect -a boostedbudget-db-staging

# Count processed vs unprocessed
SELECT
  processed,
  COUNT(*)
FROM webhook_events
GROUP BY processed;

# Check for errors
SELECT
  webhook_type,
  webhook_code,
  error_code,
  signature_valid
FROM webhook_events
WHERE processed = false
ORDER BY created_at DESC;
```

**Note:** Current implementation stores webhooks but does NOT automatically process them. Processing logic (fetching new transactions, updating account status) will be added in future phases. For now, verify:
- ✅ Webhooks are received (200 OK response)
- ✅ Signatures are verified (`signature_valid=true`)
- ✅ Events are stored in database

## Part 4: Testing Checklist

Before marking Issue #202 as complete, verify:

### Staging Environment
- [ ] `PLAID_WEBHOOK_URL` environment variable set in Fly.io
- [ ] Webhook URL registered in Plaid Dashboard (sandbox)
- [ ] Backend deployed with webhook code
- [ ] Logs show "✅ Webhook signature verification enabled"
- [ ] Manual webhook fires successfully (`/sandbox/item/fire_webhook`)
- [ ] Logs show "Webhook signature verified successfully"
- [ ] Database shows webhooks stored with `signature_valid=true`
- [ ] Invalid signatures are logged as warnings (not errors)
- [ ] Webhook endpoint returns 200 OK for all requests

### Production Environment (After Staging Validation)
- [ ] `PLAID_WEBHOOK_URL` environment variable set in Fly.io
- [ ] Webhook URL registered in Plaid Dashboard (production)
- [ ] `PLAID_ENV=production` set
- [ ] `PLAID_SECRET` updated to production secret
- [ ] Backend deployed with webhook code
- [ ] Real bank connection triggers webhooks
- [ ] Signature verification working in production
- [ ] Database storage working in production

## Part 5: Quick Reference Commands

### Deploy to Staging
```bash
cd /home/max/coding/BoostedBudget
./scripts/deploy-to-fly.sh staging
fly logs -a boostedbudget-staging
```

### Deploy to Production
```bash
cd /home/max/coding/BoostedBudget
./scripts/deploy-to-fly.sh production
fly logs -a boostedbudget-production
```

### Set Webhook URL
```bash
# Staging
fly secrets set PLAID_WEBHOOK_URL="https://staging.boostedbudget.com/webhooks/plaid" -a boostedbudget-staging

# Production
fly secrets set PLAID_WEBHOOK_URL="https://api.boostedbudget.com/webhooks/plaid" -a boostedbudget-production
```

### Fire Test Webhook
```bash
curl -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "client_id": "YOUR_CLIENT_ID",
    "secret": "YOUR_SANDBOX_SECRET",
    "access_token": "access-sandbox-xxx",
    "webhook_code": "DEFAULT_UPDATE"
  }'
```

### Check Recent Webhooks
```bash
fly postgres connect -a boostedbudget-db-staging
SELECT webhook_type, webhook_code, signature_valid, created_at FROM webhook_events ORDER BY created_at DESC LIMIT 10;
\q
```

### Monitor Webhook Logs
```bash
# Real-time monitoring
fly logs -a boostedbudget-staging | grep webhook

# Last 100 webhook-related logs
fly logs -a boostedbudget-staging --tail=100 | grep -i "webhook\|plaid"
```

## Part 6: Understanding Webhook Flow

Here's what happens when Plaid sends a webhook:

```
1. Plaid sends POST request to /webhooks/plaid
   ↓
2. Backend reads raw request body (preserves exact bytes)
   ↓
3. Extracts JWT signature from "Plaid-Verification" header
   ↓
4. Verifies JWT signature using Plaid's public key (ES256)
   ↓ (if verified)
5. Validates JWT claims (issued within 5 minutes)
   ↓
6. Verifies body SHA-256 hash matches JWT claim
   ↓ (all checks pass)
7. Parses JSON payload
   ↓
8. Stores webhook in database with signature_valid=true
   ↓
9. Logs webhook receipt with details
   ↓
10. Returns 200 OK to Plaid (prevents retries)
```

**Why Always Return 200 OK?**
- If we return an error (400, 500), Plaid will retry the webhook
- Retries can cause cascading failures and duplicate processing
- Better to store invalid webhooks for debugging than reject them

## Need Help?

**Plaid Documentation:**
- Webhooks Overview: https://plaid.com/docs/api/webhooks/
- Webhook Verification: https://plaid.com/docs/api/webhooks/#webhook-verification
- Sandbox Testing: https://plaid.com/docs/sandbox/test-webhooks/

**BoostedBudget Documentation:**
- Implementation: See PR #255
- Code: `backend/internal/handlers/plaid_webhook_handler.go`
- Tests: `backend/internal/handlers/plaid_webhook_handler_test.go`

**Common Issues:**
- GitHub Issue: https://github.com/BoostedBudget/BoostedBudget/issues/202
- Plaid Support: https://dashboard.plaid.com/support/contact
