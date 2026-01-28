# Paddle Setup Guide for BoostedBudget

**Last Updated:** 2026-01-26
**Status:** Backend & Core Frontend Complete ✅ - Awaiting YOUR Manual Setup

This guide contains all the manual steps YOU (the human user) need to perform to set up and configure Paddle for BoostedBudget's subscription payments.

## 🎯 Quick Start Checklist

**What YOU need to do (estimated time: 2-3 hours for sandbox):**

- [ ] **1. Create Paddle Sandbox Account** (15 min)
  - Sign up at https://sandbox-vendors.paddle.com/signup
  - Verify email and complete business profile

- [ ] **2. Create Products & Prices** (45 min)
  - Create 5 products (premium, household_2-5)
  - Create 10 prices (monthly + yearly for each)
  - Save all 10 Price IDs in a text file

- [ ] **3. Configure Webhook** (10 min)
  - Set endpoint URL: `https://your-backend-url.com/webhooks/paddle`
  - Subscribe to 8 event types
  - Save webhook secret

- [ ] **4. Get API Credentials** (5 min)
  - Get API key (backend)
  - Get client token (frontend)

- [ ] **5. Update Environment Variables** (10 min)
  - Update backend `.env` with API key, webhook secret
  - Update frontend `.env.local` with client token
  - Restart backend and frontend

- [ ] **6. Update subscription_tiers.yaml** (15 min)
  - Replace all `null` price IDs with your actual Paddle Price IDs
  - Commit the file to git

- [ ] **7. Test Checkout Flow** (30 min)
  - Navigate to /pricing
  - Click "Subscribe" button
  - Complete test checkout with card `4242 4242 4242 4242`
  - Verify webhook received
  - Verify subscription in database

**Once these steps are done, the integration is fully functional!**

---

## Prerequisites

- [ ] BoostedBudget backend deployed and accessible at a public URL
- [ ] Database migrations run (`make migrate-up`)
- [ ] Business registration documents (for Paddle account verification)
- [ ] Tax ID / VAT number (if applicable)

---

## Phase 1: Create Paddle Sandbox Account

### Step 1.1: Sign Up for Paddle Sandbox

1. **Visit:** https://sandbox-vendors.paddle.com/signup
2. **Create account with:**
   - Business email address
   - Company name: "BoostedBudget" (or your legal entity name)
   - Password (save in password manager)
3. **Verify email** - Check inbox and click verification link
4. **Complete business profile:**
   - Business address
   - Tax information
   - Bank account details (for payouts)
   - Business documents (as required)

**Notes:**
- Sandbox mode allows full testing without real money
- You can create test transactions with test card: `4242 4242 4242 4242`
- Sandbox and production are separate accounts with different credentials

### Step 1.2: Explore Paddle Dashboard

Familiarize yourself with:
- **Catalog** → Products & Prices
- **Developers** → Webhooks
- **Developers** → Authentication (API keys)
- **Checkout** → Checkout Settings

---

## Phase 2: Create Products and Prices

### Step 2.1: Create Premium Product

1. Navigate to **Catalog** → **Products**
2. Click **Create Product**
3. Fill in details:
   - **Name:** `Premium`
   - **Description:** `Full features for power users`
   - **Tax category:** `saas` (Software as a Service)
   - **Image:** (optional) Upload product icon
4. Click **Save Product**
5. Copy the Product ID (format: `pro_01h...`) - **SAVE THIS**

### Step 2.2: Create Premium Prices

**Monthly Price:**
1. In the Premium product page, click **Add Price**
2. Fill in:
   - **Name:** `Premium Monthly`
   - **Billing cycle:** `Monthly`
   - **Price:** `$8.99 USD`
   - **Trial period:** `0 days` (we handle trial in-app)
   - **Billing period:** `1 month`
3. Click **Save Price**
4. Copy the Price ID (format: `pri_01h...`) - **SAVE THIS AS `premium_monthly`**

**Yearly Price:**
1. Click **Add Price** again
2. Fill in:
   - **Name:** `Premium Yearly`
   - **Billing cycle:** `Yearly`
   - **Price:** `$79.00 USD`
   - **Trial period:** `0 days`
   - **Billing period:** `1 year`
3. Click **Save Price**
4. Copy the Price ID - **SAVE THIS AS `premium_yearly`**

### Step 2.3: Create Household_2 Product

Repeat Step 2.1 with:
- **Name:** `Household (2)`
- **Description:** `Premium features for 2 family members`

Then create two prices:
- **Monthly:** `$12.99 USD` - **SAVE AS `household_2_monthly`**
- **Yearly:** `$119.00 USD` - **SAVE AS `household_2_yearly`**

### Step 2.4: Create Household_3 Product

- **Name:** `Household (3)`
- **Description:** `Premium features for 3 family members`
- **Monthly:** `$14.99 USD` - **SAVE AS `household_3_monthly`**
- **Yearly:** `$139.00 USD` - **SAVE AS `household_3_yearly`**

### Step 2.5: Create Household_4 Product

- **Name:** `Household (4)`
- **Description:** `Premium features for 4 family members`
- **Monthly:** `$16.99 USD` - **SAVE AS `household_4_monthly`**
- **Yearly:** `$159.00 USD` - **SAVE AS `household_4_yearly`**

### Step 2.6: Create Household_5 Product

- **Name:** `Household (5+)`
- **Description:** `Premium features for 5+ family members`
- **Monthly:** `$18.99 USD` - **SAVE AS `household_5_monthly`**
- **Yearly:** `$179.00 USD` - **SAVE AS `household_5_yearly`**

### Step 2.7: Price IDs Summary Table

Create a table with all your Price IDs:

| Tier | Monthly Price ID | Yearly Price ID |
|------|------------------|-----------------|
| premium | `pri_01h...` | `pri_01h...` |
| household_2 | `pri_01h...` | `pri_01h...` |
| household_3 | `pri_01h...` | `pri_01h...` |
| household_4 | `pri_01h...` | `pri_01h...` |
| household_5 | `pri_01h...` | `pri_01h...` |

**IMPORTANT:** You'll need these Price IDs for Step 4.1 below.

---

## Phase 3: Configure Webhooks

### Step 3.1: Create Webhook Endpoint

1. Navigate to **Developers** → **Webhooks**
2. Click **Create Webhook**
3. Fill in:
   - **Endpoint URL:** `https://your-backend-url.com/webhooks/paddle`
     * Replace `your-backend-url.com` with your actual backend domain
     * Must be HTTPS (Paddle requires SSL)
     * Must be publicly accessible (Paddle can't reach localhost)
   - **Description:** `BoostedBudget Production Webhooks`
   - **Event types to subscribe:**
     - [x] `subscription.created`
     - [x] `subscription.activated`
     - [x] `subscription.updated`
     - [x] `subscription.canceled`
     - [x] `subscription.paused`
     - [x] `subscription.past_due`
     - [x] `transaction.completed`
     - [x] `transaction.payment_failed`
4. Click **Save Webhook**
5. Copy the **Webhook Secret** (format: `pdl_ntfset_01h...`) - **SAVE THIS**

**Testing Webhooks Locally:**
If you need to test webhooks on localhost during development:
- Use ngrok: `ngrok http 8080`
- Copy the ngrok HTTPS URL (e.g., `https://abc123.ngrok.io`)
- Create a separate webhook in Paddle with ngrok URL
- Remember to update when ngrok URL changes

### Step 3.2: Test Webhook Connection

1. In Paddle Dashboard, click on your webhook
2. Click **Send Test Event**
3. Select `subscription.created`
4. Click **Send**
5. Check your backend logs for webhook receipt
6. If successful, you should see: `Paddle webhook processed successfully`

---

## Phase 4: Get API Keys and Configure Environment

### Step 4.1: Get Paddle API Key (Backend)

1. Navigate to **Developers** → **Authentication**
2. Click **Create API Key**
3. Fill in:
   - **Name:** `BoostedBudget Backend Sandbox`
   - **Description:** `Server-side API access for checkout and subscriptions`
4. Click **Create API Key**
5. **COPY THE KEY IMMEDIATELY** (shown only once): `live_01h...` for production or `test_01h...` for sandbox
6. **SAVE THIS SECURELY** - You cannot view it again

### Step 4.2: Get Paddle Client Token (Frontend)

1. Still in **Developers** → **Authentication**
2. Look for **Client-side tokens** section
3. Find your default client token OR click **Create Client Token**
4. Copy the token (format: `live_01h...` or `test_01h...`)
5. **SAVE THIS** - This is public-safe (goes in frontend code)

### Step 4.3: Update Backend Environment Variables

Edit your backend `.env` file (or environment config):

```bash
# Paddle Configuration
PADDLE_API_KEY="test_01h..."          # From Step 4.1
PADDLE_WEBHOOK_SECRET="pdl_ntfset_01h..."  # From Step 3.1
PADDLE_ENVIRONMENT="sandbox"          # Use "production" when going live
```

### Step 4.4: Update Frontend Environment Variables

Edit `apps/web/.env.local` (or your frontend config):

```bash
# Paddle Configuration
VITE_PADDLE_CLIENT_TOKEN="test_01h..."  # From Step 4.2
VITE_PADDLE_ENV="sandbox"               # Use "production" when going live
```

### Step 4.5: Update Subscription Tier Configuration

**NEW: Environment-Specific Configuration Files**

BoostedBudget now supports separate config files for sandbox and production environments, automatically selected based on the `PADDLE_ENVIRONMENT` variable. This allows you to maintain different Paddle Price IDs for each environment.

**Configuration files:**
- `config/subscription_tiers.sandbox.yaml` - Sandbox Paddle Price IDs (automatically loaded when `PADDLE_ENVIRONMENT=sandbox`)
- `config/subscription_tiers.production.yaml` - Production Paddle Price IDs (automatically loaded when `PADDLE_ENVIRONMENT=production`)
- `config/subscription_tiers.yaml` - Default/template (fallback when no environment specified)

**Automatic selection priority:**
1. `SUBSCRIPTION_TIERS_CONFIG` env var (explicit path override)
2. `config/subscription_tiers.{PADDLE_ENVIRONMENT}.yaml` (environment-specific)
3. `config/subscription_tiers.yaml` (default fallback)

**For Sandbox Setup (recommended for initial testing):**

Edit `config/subscription_tiers.sandbox.yaml` and replace `null` price IDs with your actual **SANDBOX** Paddle Price IDs from Step 2.7:

```yaml
tiers:
  premium:
    paddle_price_id_monthly: "pri_01h..."  # Your SANDBOX premium monthly price ID
    paddle_price_id_yearly: "pri_01h..."   # Your SANDBOX premium yearly price ID

  household_2:
    paddle_price_id_monthly: "pri_01h..."  # Your SANDBOX household_2 monthly price ID
    paddle_price_id_yearly: "pri_01h..."   # Your SANDBOX household_2 yearly price ID

  household_3:
    paddle_price_id_monthly: "pri_01h..."
    paddle_price_id_yearly: "pri_01h..."

  household_4:
    paddle_price_id_monthly: "pri_01h..."
    paddle_price_id_yearly: "pri_01h..."

  household_5:
    paddle_price_id_monthly: "pri_01h..."
    paddle_price_id_yearly: "pri_01h..."
```

**For Production (when ready to go live):**

1. Repeat Phases 1-4 in Paddle Production Dashboard (https://vendors.paddle.com)
2. Get production Price IDs
3. Edit `config/subscription_tiers.production.yaml` and add **PRODUCTION** Price IDs
4. Update backend `.env` to use `PADDLE_ENVIRONMENT="production"`
5. Update frontend `.env.local` to use `VITE_PADDLE_ENV="production"`

**Important Notes:**
- Paddle Price IDs are **completely different** between sandbox and production
- The backend will automatically load the correct config based on `PADDLE_ENVIRONMENT`
- **COMMIT THESE FILES** after updating (price IDs are not secrets, but ensure you don't mix sandbox/production IDs)
- Backend logs will show which config file was loaded: `Loading environment-specific config: config/subscription_tiers.sandbox.yaml`

---

## Phase 5: Test the Integration

### Step 5.1: Restart Backend

After setting environment variables:

```bash
# If using Docker
docker-compose restart backend

# If running locally
# Kill the backend process and restart
cd backend && go run cmd/server/main.go
```

Check logs for:
```
✅ Paddle service initialized (environment: sandbox)
✅ Huma Paddle checkout routes registered
✅ Huma Paddle webhook routes registered
```

### Step 5.2: Test Checkout Flow (Manual)

1. **Start frontend dev server:**
   ```bash
   cd apps/web
   bun run dev
   ```

2. **Navigate to pricing page:** http://localhost:5173/pricing

3. **Click "Subscribe" button** for any tier

4. **Paddle checkout overlay should open** with correct pricing

5. **Use Paddle test card:**
   - Card number: `4242 4242 4242 4242`
   - Expiry: Any future date (e.g., `12/28`)
   - CVV: Any 3 digits (e.g., `123`)
   - Name: `Test User`
   - Email: Your test email
   - Country: `United States`
   - Zip: `10001`

6. **Complete checkout** - Paddle will redirect back to your app

7. **Check backend logs** for webhook processing:
   ```
   Paddle webhook received (event_id: evt_...)
   Paddle webhook processed successfully (subscription.created)
   ```

8. **Verify in database:**
   ```sql
   -- Check paddle_webhook_events table
   SELECT * FROM paddle_webhook_events ORDER BY created_at DESC LIMIT 5;

   -- Check user subscription updated
   SELECT id, email, subscription_tier, subscription_status, paddle_customer_id
   FROM users
   WHERE paddle_customer_id IS NOT NULL;
   ```

9. **Verify in Paddle Dashboard:**
   - Navigate to **Subscriptions**
   - You should see your test subscription listed
   - Status should be `active`

### Step 5.3: Test Webhook Events

1. In Paddle Dashboard, go to **Subscriptions**
2. Click on your test subscription
3. Try these actions:
   - **Pause subscription** → Should trigger `subscription.paused` webhook
   - **Resume subscription** → Should trigger `subscription.activated` webhook
   - **Cancel subscription** → Should trigger `subscription.canceled` webhook

4. Check backend logs for each webhook event being processed

5. Check database to see subscription status updates

### Step 5.4: Common Issues

**Issue: Paddle overlay doesn't open**
- Check browser console for errors
- Verify `window.Paddle` is defined
- Check `VITE_PADDLE_CLIENT_TOKEN` is set
- Ensure Paddle.js script loaded (check Network tab)

**Issue: "Price ID not configured" error**
- Verify price IDs in `subscription_tiers.yaml` match Paddle Dashboard
- Restart backend after updating config
- Check backend logs for config loading errors

**Issue: Webhook not received**
- Check webhook URL is correct and publicly accessible
- Verify webhook secret in backend `.env`
- Check Paddle Dashboard → Webhooks → Recent Deliveries for errors
- Try sending test event from Paddle Dashboard

**Issue: Signature verification fails**
- Verify `PADDLE_WEBHOOK_SECRET` matches Paddle Dashboard
- Check for whitespace in environment variable
- Ensure using correct webhook secret (not API key)

---

## Phase 6: Going to Production

### Step 6.1: Create Production Paddle Account

1. **Visit:** https://vendors.paddle.com/signup
2. **Complete business verification** (may take 1-3 business days)
3. **Provide required documents:**
   - Business registration certificate
   - Tax ID documentation
   - Bank account verification
   - Identity verification (for business owners)

### Step 6.2: Replicate Products in Production

Repeat Phase 2 steps in your production Paddle account:
- Create all 5 products
- Create monthly and yearly prices for each
- **SAVE NEW PRODUCTION PRICE IDs**
- Update `subscription_tiers.yaml` with production price IDs

### Step 6.3: Configure Production Webhooks

Repeat Phase 3 steps with production backend URL:
- Webhook URL: `https://api.boostedbudget.com/webhooks/paddle`
- Subscribe to same 8 event types
- **SAVE NEW PRODUCTION WEBHOOK SECRET**

### Step 6.4: Get Production API Keys

Repeat Phase 4.1 and 4.2 to get production credentials:
- Production API Key (starts with `live_`)
- Production Client Token (starts with `live_`)

### Step 6.5: Update Production Environment

**Backend Production Environment:**
```bash
PADDLE_API_KEY="live_01h..."          # Production API key
PADDLE_WEBHOOK_SECRET="pdl_ntfset_01h..."  # Production webhook secret
PADDLE_ENVIRONMENT="production"       # Changed from "sandbox"
```

**Frontend Production Environment:**
```bash
VITE_PADDLE_CLIENT_TOKEN="live_01h..."  # Production client token
VITE_PADDLE_ENV="production"            # Changed from "sandbox"
```

### Step 6.6: Production Testing Checklist

Before going live:

- [ ] Test complete checkout flow with real card (small amount)
- [ ] Verify subscription created in Paddle Dashboard
- [ ] Confirm webhook received and processed
- [ ] Verify user account updated in database
- [ ] Test subscription update (upgrade/downgrade)
- [ ] Test subscription cancellation
- [ ] Verify email notifications work
- [ ] Test payment failure handling
- [ ] Confirm refund process works
- [ ] Verify tax calculations are correct
- [ ] Test with different countries/currencies (if supported)
- [ ] Review Terms of Service and Privacy Policy compliance
- [ ] Ensure GDPR data export includes Paddle data
- [ ] Configure Paddle customer portal settings

### Step 6.7: Paddle Dashboard Production Settings

Configure these in production:

1. **Checkout Settings:**
   - Custom checkout theme (match BoostedBudget branding)
   - Success/cancel URLs
   - Email receipts and invoices

2. **Subscription Settings:**
   - Dunning management (payment retry logic)
   - Grace periods for payment failures
   - Cancellation flow

3. **Tax Settings:**
   - Enable automatic tax calculation
   - Configure tax thresholds by region
   - Set up VAT/GST registration numbers

4. **Notifications:**
   - Enable seller notifications for important events
   - Configure customer email templates

---

## Phase 7: Monitoring and Maintenance

### Step 7.1: Regular Checks

**Daily:**
- [ ] Monitor webhook delivery success rate in Paddle Dashboard
- [ ] Check backend logs for webhook processing errors
- [ ] Review failed payments and follow up

**Weekly:**
- [ ] Review subscription metrics (new, active, churned)
- [ ] Check for unusual payment patterns or fraud
- [ ] Verify reconciliation between Paddle and database

**Monthly:**
- [ ] Reconcile revenue in Paddle vs. database
- [ ] Review and respond to customer subscription issues
- [ ] Update pricing if needed
- [ ] Check for Paddle API updates or deprecations

### Step 7.2: Key Paddle Dashboard URLs

**Sandbox:**
- Dashboard: https://sandbox-vendors.paddle.com
- API Docs: https://developer.paddle.com

**Production:**
- Dashboard: https://vendors.paddle.com
- Customer Portal: https://subscription-management.paddle.com

### Step 7.3: Support Contacts

- **Paddle Support:** support@paddle.com
- **Paddle Developer Docs:** https://developer.paddle.com
- **Paddle API Reference:** https://developer.paddle.com/api-reference
- **Paddle Status Page:** https://status.paddle.com

---

## Checklist: Quick Reference

### Initial Setup (Sandbox)
- [ ] Create Paddle sandbox account
- [ ] Create 5 products (premium, household_2-5)
- [ ] Create 10 prices (monthly + yearly for each product)
- [ ] Save all 10 Price IDs
- [ ] Configure webhook endpoint
- [ ] Save webhook secret
- [ ] Get API key (backend)
- [ ] Get client token (frontend)
- [ ] Update backend `.env` with API key, webhook secret, environment
- [ ] Update frontend `.env.local` with client token, environment
- [ ] Update `config/subscription_tiers.yaml` with Price IDs
- [ ] Restart backend and frontend
- [ ] Test checkout flow with test card
- [ ] Verify webhook received and processed
- [ ] Verify subscription created in database

### Production Setup
- [ ] Create Paddle production account
- [ ] Complete business verification
- [ ] Replicate all products and prices
- [ ] Save production Price IDs
- [ ] Configure production webhook endpoint
- [ ] Get production API key and client token
- [ ] Update production environment variables
- [ ] Update `subscription_tiers.yaml` with production Price IDs
- [ ] Run full production testing checklist
- [ ] Monitor for 48 hours before announcing

---

## Environment Variables Reference

### Backend (.env)
```bash
# Required for Paddle Integration
PADDLE_API_KEY=test_01h...              # Paddle API key (from Dashboard)
PADDLE_WEBHOOK_SECRET=pdl_ntfset_01h... # Webhook signature secret
PADDLE_ENVIRONMENT=sandbox              # "sandbox" or "production"

# Existing required variables (must be set)
DATABASE_URL=postgresql://...           # PostgreSQL connection
CLERK_SECRET_KEY=sk_test_...           # Clerk authentication
FRONTEND_URL=http://localhost:5173      # For checkout success redirect
```

### Frontend (apps/web/.env.local)
```bash
# Required for Paddle Integration
VITE_PADDLE_CLIENT_TOKEN=test_01h...    # Paddle client-side token
VITE_PADDLE_ENV=sandbox                 # "sandbox" or "production"

# Existing required variables
VITE_CLERK_PUBLISHABLE_KEY=pk_test_... # Clerk authentication
VITE_API_URL=http://localhost:8080     # Backend API URL
```

---

**End of Setup Guide**

Update this file as you complete each step and discover new information during the setup process.
