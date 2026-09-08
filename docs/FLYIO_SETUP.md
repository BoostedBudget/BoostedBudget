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

# Expected response — exactly one field, deliberately (issue #1531 criterion 20):
# {"status":"ok"}
#
# No version, no component breakdown, no error text: this is an unauthenticated,
# Cloudflare-exempt endpoint, so anything it says is public. Component detail lives on
# the admin-gated /api/v1/admin/system/health instead.

# Metrics endpoint — must NOT be publicly reachable (expect 404).
# Issue #1514 moved /metrics onto an internal-only listener (port 9091), scraped by Fly
# over the private 6PN network. A 200 here means that fix regressed and the Prometheus
# registry — signup counts, transaction volume, full route inventory — is public.
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

#### Coordinated app-and-schema rollback (Issue #1464)

**`flyctl releases rollback` alone does not roll back the database schema, and rolling back the
schema alone is unsafe.** The down migrations are correctly written (fail-closed), but they are not
safe to run against the *currently deployed* application binary in isolation:

- Rolling back migration `000119` (`backend/migrations/000119_add_plaid_cleanup_pending_at.down.sql`)
  drops `users.plaid_cleanup_pending_at`. Current application code (the Plaid-disconnect repair pass,
  `ExpireTrialIfNoOverride`, `UpdateSubscriptionFromPaddle`, and others in
  `backend/internal/repository/profile_repository.go`) reads and writes that column unconditionally
  — dropping it out from under the running binary causes SQL failures on those code paths, not a
  clean revert.
- Rolling back migration `000115` (`backend/migrations/000115_add_work_claims_lease_token.down.sql`)
  drops `work_claims.lease_token`. Every destructive flow that uses a durable work claim
  (`DeletionService`, `PastDueService`, `SubscriptionWebhookWorker`, `TrialService`) requires that
  column to exist — the same failure mode applies.

**Rollback must be coordinated: roll back the application release to a version that predates the
column's introduction *before or together with* rolling back the migration that dropped it** — never
roll back the schema against a binary that still expects the newer columns to exist. If only the app
needs to roll back (schema unaffected), `flyctl releases rollback` alone is fine. If a migration must
be reverted, treat it as a joint app+schema operation, not a `migrate down` run against whatever
binary happens to be live.

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

---

## Cloudflare API Protection (#657)

The backend API is protected by `CloudflareOnly` middleware (`backend/internal/middleware/cloudflare.go`) which enforces two layers of verification on every non-exempted request in `staging`/`production`:

1. **IP validation** — `Fly-Client-IP` (set by Fly.io's proxy edge, not forgeable by clients) must be within Cloudflare's published CIDR ranges
2. **Shared origin secret** — `X-Origin-Secret` header (injected by a Cloudflare Transform Rule) must match `CF_ORIGIN_SECRET` env var, validated with `subtle.ConstantTimeCompare`

Exempted path (not gated by Cloudflare — reached directly from Fly.io infrastructure): `/health`.
Fly's own `http_checks` hit it every 30s from inside Fly's network with no Cloudflare headers at
all, so the exemption is load-bearing: removing it would fail every machine's health check and
cause Fly to cycle the app.

`/metrics` was previously exempt here too. Issue #1514 moved it onto a separate internal-only
listener (port 9091), so it is no longer served on the public listener and needs no exemption.

### Generate and Set the Origin Secret

```bash
# Generate a 256-bit secret (do this once per environment)
openssl rand -hex 32
# Example: a3f9e2c1b0d84726f5e3a1c0b9d87264e2a1c0b9d872645f3e2a1c0b9d87264

# Set on Fly.io (staging and production can use the same or different values)
fly secrets set CF_ORIGIN_SECRET=<64-char-hex> --app staging-boostedbudget-api
fly secrets set CF_ORIGIN_SECRET=<64-char-hex> --app boostedbudget-api
```

> The server refuses to start in `staging`/`production` if `CF_ORIGIN_SECRET` is unset — this triggers Fly.io's automatic rollback before a bad deploy goes live.

### Cloudflare Transform Rules (inject X-Origin-Secret)

In **Cloudflare dashboard → boostedbudget.com → Rules → Transform Rules → Request Header Modification**, create three rules:

| Rule name | Filter expression | Header name | Header value |
|-----------|------------------|-------------|--------------|
| `Inject Origin Secret — Production API` | `(http.host eq "api.boostedbudget.com")` | `X-Origin-Secret` | `<CF_ORIGIN_SECRET for boostedbudget-api>` |
| `Inject Origin Secret — Staging API` | `(http.host eq "staging-api.boostedbudget.com")` | `X-Origin-Secret` | `<CF_ORIGIN_SECRET for staging-boostedbudget-api>` |
| `Inject Origin Secret — MCP Worker subrequests` | `(cf.worker.upstream_zone != "")` | `X-Origin-Secret` | `<CF_ORIGIN_SECRET for boostedbudget-api>` |

Click **Deploy** after each rule. The header value must exactly match the corresponding `CF_ORIGIN_SECRET` Fly.io secret.

⚠️ The third rule exists because a Cloudflare Worker's subrequest does not re-enter the zone the way
a client request does, so the host-matched rules above never fire for it and the MCP Worker would be
rejected by the origin gate. Verified live on 2026-09-08: all three rules present and enabled.

### 🔴 The cardinal rule: no challenge action may ever apply to `/webhooks/`

**A Cloudflare `managed_challenge` is equivalent to `block` for any non-browser client.** Webhook
senders are HTTP clients — they cannot solve an interstitial challenge — so a challenge on a webhook
path is a silent, total outage that the provider reports as a plain `403`.

This has happened **twice**, on 2026-09-08, from two different rules:

1. **Bot Fight Mode** (zone-wide) Managed-Challenged Plaid's `Go-http-client/2.0` from AS14618
   (Amazon). Confirmed in Security Events: two `Bot fight mode / Managed Challenge` rows at
   07:12:48 EDT for `POST api.boostedbudget.com/webhooks/plaid`, matching the two `TRANSACTIONS`
   deliveries Plaid logged as 403 in the same second. Undetected for 11+ hours across three webhook
   types. **Fixed by disabling Bot Fight Mode** — plain BFM runs ahead of WAF custom rules and
   Cloudflare provides no exclusion mechanism, so a skip rule cannot save you. On Free there is no
   way to keep it enabled and exempt a path.
2. **A WAF custom rule** challenging requests with no `User-Agent` header, which had no path scope.
   Reproduced live: `POST /webhooks/plaid` with an empty UA returned `403` while the same request
   with a UA returned `401`. **Fixed by scoping the rule to exclude `/webhooks/`.**

`scripts/smoke-test.sh` now fails the production deploy when either webhook endpoint returns
anything but `401` to an unsigned POST, and reports Cloudflare's `cf-mitigated` header so an edge
block is named as an edge block. See `scripts/__tests__/smoke-test-failure-paths.test.sh`.

### WAF Managed Rules

⚠️ **Not deployed, and mostly not available on this plan.** The zone is **Free**
(verified via `GET /zones/{id}` → `plan.name = "Free Website"`). `GET` on the
`http_request_firewall_managed` phase returns `10003: could not find entrypoint ruleset` — nothing
is deployed. The `Cloudflare Managed Free Ruleset` *is* in the catalog and could be deployed; the
**OWASP Core Ruleset requires Business**.

If you ever deploy the free managed ruleset, **scope an exception for `/webhooks/` from the start**
rather than discovering the need during an outage, and start in Log mode — API JSON (merchant names,
transaction descriptions, user notes) triggers OWASP-style false positives.

### Cloudflare Rate Limiting Rules

⚠️ **Free allows exactly ONE rate limiting rule**, minimum period 10s. The three production rules
this document used to list could never all exist. The live rule, read from the API:

| Field | Value |
|---|---|
| Expression | `(http.host eq "api.boostedbudget.com") and starts_with(http.request.uri.path, "/api/")` |
| Action | `block` |
| Rate | **34 requests / 10s** (≈204/min) |
| Characteristics | `cf.colo.id`, `ip.src` — `cf.colo.id` is **mandatory** |
| Mitigation timeout | **10s** |

Two properties that are easy to misread:

- **The limit is per Cloudflare colo, not global.** A distributed source gets ~204/min *per
  datacenter*. This is inherent to the plan, not a misconfiguration.
- **`/webhooks/` is deliberately NOT rate limited.** Throttling it risks dropping real deliveries,
  which loses financial data — a worse outcome than the compute cost. Do not "fix" this by widening
  the expression to the whole host without weighing that trade-off explicitly.

### WAF Custom Rules

Free allows **5**. All five slots are in use; adding one means consolidating another.

| # | Action | Purpose |
|---|---|---|
| 1 | block | OFAC-sanctioned countries (`KP`, `IR`, `SY`, `CU`) — US fintech compliance |
| 2 | block | Scanner UAs + probe paths + traversal/SQLi patterns (**consolidated** from three rules) |
| 3 | managed_challenge | Empty `User-Agent` — **`/webhooks/` exempt**, see the cardinal rule above |
| 4 | block | Allowlist: only `/api/`, `/webhooks/`, `/health` reach the origin |
| 5 | block | `/webhooks/` accepts **POST only** |

⚠️ **Rule 4 is an allowlist and is a standing maintenance hazard.** Any future endpoint outside those
three prefixes gets a silent edge `403` that looks exactly like the outages above. It deliberately
blocks the OpenAPI spec (both the JSON and YAML forms), the docs UI and the JSON-schema
directory on the production API host — verified that nothing fetches
these at runtime (every reference in the tree is to the local build-time `api/openapi.yaml`), and
serving them publicly leaked the full API surface. **If you add a public non-`/api/` route, this
rule is the first place to look.**

⚠️ Rule 2 matches User-Agent **substrings**, so a legitimate client whose UA ever contains `Nmap` or
`Hydra` is silently blocked. Rule 4 blocks POST floods to no path — see below.

### Cost exposure (what these rules do and do not bound)

Requests blocked at the edge are **free**. Requests reaching the origin cost Fly compute plus **at
least one Redis command each**, because `IPRateLimiter` (`main.go`, immediately after
`CloudflareOnly`) consults Redis on every request — *including the ones it rejects*. Rate limiting is
therefore a per-request toll on hostile traffic, not free protection.

**Remaining unbounded vector: a POST flood to `/webhooks/plaid` or `/webhooks/paddle`.** No edge rate
limit applies there, so each request costs a Redis command before the signature check rejects it.
Data is never at risk — Plaid webhooks are verified with ES256 + a `request_body_sha256` claim + a
5-minute `iat` window (`internal/services/webhook_service.go`) — but the compute is billed.

The only true ceiling is a **spend cap** at the provider (Upstash monthly budget, Fly machine-count
cap). The structural fix is to have `IPRateLimiter` consult its in-memory limiter *before* Redis.

### Webhook Pre-Deploy Check

Cloudflare protection blocks direct `*.fly.dev` access. Confirm all webhook providers send to the Cloudflare-proxied domains, not the Fly.io URLs directly:

- **Plaid (Sandbox)**: `https://staging-api.boostedbudget.com/webhooks/plaid`
- **Plaid (Production)**: `https://api.boostedbudget.com/webhooks/plaid`
- **Paddle (Staging)**: `https://staging-api.boostedbudget.com/webhooks/paddle`
- **Paddle (Production)**: `https://api.boostedbudget.com/webhooks/paddle`

Also confirm both CNAME records in Cloudflare DNS are **Proxied (orange cloud ☁️)**, not DNS-only.

🔴 **Check the hostname character by character.** As of 2026-09-08 the Plaid dashboard's staging
entries pointed at **`api-staging.boostedbudget.com`** — the components transposed. That hostname has
**no DNS record at all** (`dig +short A api-staging.boostedbudget.com` → empty), so every staging
webhook was being delivered nowhere, silently, with no dashboard error resembling the production
403s. The correct host is `staging-api.boostedbudget.com`, which is also what the `X-Origin-Secret`
Transform Rule matches on (`http.host eq "staging-api.boostedbudget.com"`) — a transposed hostname
would fail the origin gate even if it did resolve.

⚠️ Separately, `staging-api.boostedbudget.com` resolves to Cloudflare but was **completely
unreachable** on 2026-09-08 (15s timeout on `/health`, while production `/health` returned 200 in
145ms). Verify staging is actually serving before trusting any staging webhook test.

### Post-Deploy Smoke Tests

After setting Fly.io secrets and deploying Transform Rules, verify the protection is active:

```bash
# 1. Direct fly.dev access must return 403
curl -s -o /dev/null -w "Status: %{http_code}\n" \
  https://staging-boostedbudget-api.fly.dev/api/v1/user/profile
# Expected: 403

# 2. Via Cloudflare domain must return 401 (CF check passed, Clerk rejected)
curl -s -o /dev/null -w "Status: %{http_code}\n" \
  https://staging-api.boostedbudget.com/api/v1/user/profile
# Expected: 401

# 3. Health check via fly.dev must return 200 (exempted from Cloudflare check)
curl -s -o /dev/null -w "Status: %{http_code}\n" \
  https://staging-boostedbudget-api.fly.dev/health
# Expected: 200

# 4. Metrics via fly.dev must NOT be publicly reachable (issue #1514 criterion 18).
#    It moved to an internal-only listener on port 9091, scraped by Fly over 6PN.
#    ⚠ A 200 here is a security regression, not a healthy result: it would mean the
#    Prometheus registry (signup counts, transaction volume, full route inventory) is
#    readable by anyone who guesses the hostname.
curl -s -o /dev/null -w "Status: %{http_code}\n" \
  https://staging-boostedbudget-api.fly.dev/metrics
# Expected: 403 — hitting fly.dev directly skips Cloudflare, so CloudflareOnly's origin
# gate rejects the request before routing ever gets a chance to 404 (/metrics was removed
# from exemptedPaths; see internal/middleware/cloudflare.go). Any non-200 is acceptable —
# the point is that it is not served publicly — but 403 is what you should actually see
# here. (Through Cloudflare, at the api.boostedbudget.com hostname above, the same request
# gets a 404 instead, because the gate passes and routing is what rejects it.)
#
# Confirm the scrape still WORKS, separately — "not public" and "not collected" look the
# same from out here. Check that samples are still arriving in fly-metrics.net.

# 5. Spoofed CF-Connecting-IP (original naive approach) must still return 403
curl -s -o /dev/null -w "Status: %{http_code}\n" \
  -H "CF-Connecting-IP: 1.2.3.4" \
  https://staging-boostedbudget-api.fly.dev/api/v1/user/profile
# Expected: 403 (proves the original header-only approach is insufficient)
```

Also add to the **Pre-deployment Checklist** above:
- [ ] `CF_ORIGIN_SECRET` set via `fly secrets set` for each app
- [ ] Cloudflare Transform Rules created (X-Origin-Secret injection)
- [ ] DNS records for `api.boostedbudget.com` and `staging-api.boostedbudget.com` are Proxied (orange cloud)

---

## References

- [Fly.io Documentation](https://fly.io/docs/)
- [Fly.io Postgres](https://fly.io/docs/postgres/)
- [Fly.io Custom Domains](https://fly.io/docs/app-guides/custom-domains-with-fly/)
- [Porkbun DNS](https://kb.porkbun.com/category/3-dns)
- [Cloudflare IP Ranges](https://www.cloudflare.com/ips/) — used by `CloudflareFallbackCIDRs()` in `backend/internal/middleware/cloudflare_ips.go`
