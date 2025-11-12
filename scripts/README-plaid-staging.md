# Plaid Transaction Testing in Staging

This guide explains how to trigger transaction syncing in your staging environment.

## The Problem

Your staging environment's webhook URL (`http://qa.example.ts.net:8080/webhooks/plaid`) uses Tailscale, which is not publicly accessible from Plaid's servers. This means automatic webhooks won't be delivered.

**Current Status:**
- Linked items in DB: 1 (Bank of America)
- Transactions synced: 0
- Webhooks received: 0

## Solution: Manual Webhook Triggering

For local/staging development, you can manually trigger webhooks using Plaid's sandbox testing tools.

---

## Quick Start

### 1. Check Current Status

```bash
./scripts/trigger-plaid-transactions-staging.sh check-status
```

This shows:
- Number of linked items
- Number of synced transactions
- Recent webhook events
- Troubleshooting tips

### 2. Fire Webhooks via Plaid Dashboard

**Step 1:** Go to [Plaid Dashboard → Activity → Events](https://dashboard.plaid.com/activity/events)

**Step 2:** Make sure you're in **Sandbox** environment (top-right dropdown)

**Step 3:** Find your linked item:
- Item ID: `cbf6ebb6-dca3-498c-aaa1-0df4a2eee2b8`
- Institution: Bank of America

**Step 4:** Click on the item, then click **"Send test webhook"**

**Step 5:** Select webhook type:
- `INITIAL_UPDATE` - Triggers historical transaction sync (first-time sync)
- `DEFAULT_UPDATE` - Triggers incremental transaction sync (new transactions)
- `HISTORICAL_UPDATE` - Re-sync historical transactions
- `TRANSACTIONS_REMOVED` - Simulate transaction removals

**Step 6:** Click **Send**

**Step 7:** Monitor webhook processing:

```bash
./scripts/trigger-plaid-transactions-staging.sh watch-logs
```

You should see:
- `Received Plaid webhook: type=TRANSACTIONS code=DEFAULT_UPDATE`
- `Webhook signature verified successfully`
- `Webhook stored in database: id=<uuid>`
- `Processing TRANSACTIONS webhook`
- `Synced page 1 for item <id>: X added, Y modified, Z removed`
- `Successfully processed TRANSACTIONS webhook`

**Step 8:** Verify transactions synced:

```bash
./scripts/trigger-plaid-transactions-staging.sh check-status
```

---

## Alternative Method: API Call with Access Token

If you have the access token, you can fire webhooks via API:

### Get Access Token from Backend Logs (When Linking New Account)

When you link a new account through the UI, temporarily enable debug logging to capture the access token:

1. Edit `backend/internal/handlers/plaid_handler.go`
2. Add a log statement after token exchange:
   ```go
   log.Printf("DEBUG: Access token for testing: %s", accessToken)
   ```
3. Restart backend: `docker-compose -f docker-compose.staging.yml restart backend`
4. Link a new bank account via UI
5. Check logs: `docker logs boostedbudget-staging-backend-1 | grep "DEBUG: Access token"`
6. Copy the access token and use it below
7. **REMOVE the debug log** and restart backend

### Fire Webhook via curl

```bash
curl -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "client_id": "68f673f07c634d00204cccaf",
    "secret": "88b841df5ae79bf230bf3a3e8778c1",
    "access_token": "access-sandbox-XXXXXXXX",
    "webhook_code": "DEFAULT_UPDATE"
  }'
```

**Available webhook codes:**
- `INITIAL_UPDATE` - Historical transactions ready (30-730 days based on tier)
- `DEFAULT_UPDATE` - New transactions available (since last sync)
- `HISTORICAL_UPDATE` - Historical data has been updated
- `TRANSACTIONS_REMOVED` - Transactions have been removed

---

## Future Solution: Admin Endpoint

A better long-term solution is to create an admin endpoint in the backend:

**Endpoint:** `POST /api/v1/admin/plaid/fire-webhook`

**Request:**
```json
{
  "item_id": "cbf6ebb6-dca3-498c-aaa1-0df4a2eee2b8",
  "webhook_code": "DEFAULT_UPDATE"
}
```

**Implementation:** The backend:
1. Queries `linked_items` for the item_id
2. Decrypts the `access_token_encrypted`
3. Calls Plaid's `/sandbox/item/fire_webhook` API
4. Returns success/failure

This would be tracked in a future issue (e.g., #XXX: Add admin endpoint for manual webhook triggering)

---

## Script Usage

The `trigger-plaid-transactions-staging.sh` script provides several commands:

### Interactive Menu (Default)

```bash
./scripts/trigger-plaid-transactions-staging.sh
# or
./scripts/trigger-plaid-transactions-staging.sh menu
```

Shows an interactive menu with all available options.

### Check Status

```bash
./scripts/trigger-plaid-transactions-staging.sh check-status
```

Shows current sync status, linked items, transactions, and webhooks.

### Fire Webhook Instructions

```bash
./scripts/trigger-plaid-transactions-staging.sh fire-webhook
```

Displays instructions and curl command template for firing webhooks manually.

### Watch Logs

```bash
./scripts/trigger-plaid-transactions-staging.sh watch-logs
```

Live-streams backend logs filtered for webhook/plaid/transaction events. Press Ctrl+C to stop.

### Database Info

```bash
./scripts/trigger-plaid-transactions-staging.sh db-info
```

Shows linked items and recent transactions from the database.

---

## Troubleshooting

### No transactions after firing webhook

**Check 1:** Verify webhook was received

```bash
docker exec boostedbudget-staging-postgres-1 psql -U staginguser -d boostedbudget_staging -c \
  "SELECT webhook_type, webhook_code, processed, processing_error FROM webhook_events ORDER BY created_at DESC LIMIT 5;"
```

**Check 2:** Check for processing errors

```bash
docker logs boostedbudget-staging-backend-1 2>&1 | grep -i error | tail -20
```

**Check 3:** Verify webhook processor is running

```bash
docker logs boostedbudget-staging-backend-1 2>&1 | grep "Webhook processor"
```

Should show:
- `✅ Webhook processor initialized (interval: 30s, batch: 10, types: TRANSACTIONS+ITEM)`
- `Webhook processor starting with interval: 30s`

### Webhook processor not processing webhooks

The processor runs every 30 seconds. Wait 30-60 seconds after firing a webhook, then check:

```bash
docker exec boostedbudget-staging-postgres-1 psql -U staginguser -d boostedbudget_staging -c \
  "SELECT processed, processing_completed_at FROM webhook_events WHERE webhook_type = 'TRANSACTIONS';"
```

If `processed = false` after 60 seconds:
1. Check backend logs for errors: `docker logs boostedbudget-staging-backend-1`
2. Restart backend: `docker-compose -f docker-compose.staging.yml restart backend`

### Access token expired

Plaid sandbox access tokens can expire. If you get errors about invalid access tokens:

1. Delete the old linked item via UI or database
2. Link the account again through the UI
3. Get the new access token from logs (see above)
4. Try firing the webhook again

---

## Production Webhook Setup

In production/public staging, webhooks work automatically because the URL is publicly accessible:

**Production webhook URL:** `https://api.boostedbudget.com/webhooks/plaid`

**Flow:**
1. User links bank account via Plaid Link
2. Plaid automatically sends `INITIAL_UPDATE` webhook within 5-30 seconds
3. Backend receives webhook, verifies signature, stores in database
4. Webhook processor (runs every 30s) picks it up
5. Transaction sync service fetches transactions from Plaid
6. Transactions stored in database and displayed in UI

**No manual intervention needed in production!**

---

## Monthly Sync Limits

Your staging environment enforces tier-based limits:

| Tier | Monthly Sync Limit | Historical Depth |
|------|-------------------|------------------|
| Free | 3 accounts | 30 days |
| Premium Lite | 15 accounts | 365 days |
| Premium | 30 accounts | 730 days |
| Premium+ | Unlimited | 730 days |

**First sync** (INITIAL_UPDATE with empty cursor):
- Fetches historical transactions (30/365/730 days)
- **Counts against monthly limit**
- Records audit in `sync_audit` table

**Re-sync** (DEFAULT_UPDATE with cursor):
- Fetches only new/modified/removed transactions
- **Does NOT count against monthly limit**
- Uses cursor-based pagination

---

## Related Documentation

- **Plaid Webhook Testing Guide:** `backend/docs/plaid-webhook-testing-guide.md`
- **Architecture Plan:** `docs/boosted-budget-complete-plan.md` (Section: Plaid Integration)
- **Deployment Guide:** `docs/boosted-budget-fly-deployment.md`
- **Subscription Tiers:** `config/subscription_tiers.yaml`

---

## Quick Reference

**Check status:**
```bash
./scripts/trigger-plaid-transactions-staging.sh check-status
```

**Watch logs:**
```bash
./scripts/trigger-plaid-transactions-staging.sh watch-logs
```

**Fire webhook via Plaid Dashboard:**
1. Go to https://dashboard.plaid.com/activity/events
2. Select Sandbox environment
3. Find item: `cbf6ebb6-dca3-498c-aaa1-0df4a2eee2b8` (Bank of America)
4. Click "Send test webhook" → INITIAL_UPDATE
5. Monitor logs and check status

**Verify transactions:**
```bash
docker exec boostedbudget-staging-postgres-1 psql -U staginguser -d boostedbudget_staging -c \
  "SELECT COUNT(*) FROM transactions;"
```

---

**Last updated:** 2025-11-05
