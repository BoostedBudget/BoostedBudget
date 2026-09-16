# BoostedBudget MCP Server — Deployment Guide

Infrastructure setup for deploying the BoostedBudget MCP Cloudflare Worker.

## Prerequisites

- [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/) installed and authenticated
- A Cloudflare account with a Workers Paid plan (Durable Objects required)
- A Clerk application (for OAuth)
- The Go backend deployed to Fly.io (staging or production)

---

## 1. Clerk OAuth App Setup

In [dashboard.clerk.com](https://dashboard.clerk.com) → Your Application → OAuth Applications:

1. Create a new OAuth application: name "BoostedBudget MCP"
2. Enable **Dynamic Client Registration** (toggle in settings)
3. Enable **PKCE S256** (default for public clients)
4. Set allowed redirect URIs — include `http://localhost:*` for Claude Desktop local callback
5. Copy:
   - **Client ID** → this becomes `MCP_OAUTH_AUDIENCE`
   - **Frontend API URL** (e.g. `https://clerk.boostedbudget.com`) → `CLERK_ISSUER_URL`
   - JWKS URL = `{CLERK_ISSUER_URL}/.well-known/jwks.json` → `CLERK_JWKS_URL`

---

## 2. Generate Shared Secrets

```bash
# MCP_WORKER_SECRET: proves Worker identity to the Go backend
openssl rand -hex 32   # set as BOTH wrangler secret AND backend env var

# This secret must match between:
#   - wrangler secret put MCP_WORKER_SECRET (Worker side)
#   - MCP_WORKER_SECRET env var on the Go backend (Fly.io secret)
```

---

## 3. Set Worker Secrets

Run for each environment (staging first, then production):

```bash
# ── Staging ──────────────────────────────────────────────
wrangler secret put CLERK_SECRET_KEY          --env staging
wrangler secret put CLERK_PUBLISHABLE_KEY     --env staging
wrangler secret put CLERK_JWKS_URL            --env staging   # from step 1
wrangler secret put CLERK_ISSUER_URL          --env staging   # from step 1
wrangler secret put MCP_OAUTH_AUDIENCE        --env staging   # from step 1
wrangler secret put BOOSTEDBUDGET_ORIGIN_SECRET --env staging # must match CF Transform Rule
wrangler secret put MCP_WORKER_SECRET         --env staging   # generated in step 2
# BOOSTEDBUDGET_API_URL defaults to https://staging-api.boostedbudget.com via wrangler.toml

# ── Production (after staging validated) ─────────────────
wrangler secret put CLERK_SECRET_KEY
wrangler secret put CLERK_PUBLISHABLE_KEY
wrangler secret put CLERK_JWKS_URL
wrangler secret put CLERK_ISSUER_URL
wrangler secret put MCP_OAUTH_AUDIENCE
wrangler secret put BOOSTEDBUDGET_ORIGIN_SECRET
wrangler secret put MCP_WORKER_SECRET
```

---

## 4. Set Backend Secrets (Fly.io)

The Go backend needs `MCP_WORKER_SECRET` to verify requests from the MCP Worker:

```bash
# Staging
fly secrets set MCP_WORKER_SECRET=<same-value-as-worker> --app boostedbudget-api-staging

# Production
fly secrets set MCP_WORKER_SECRET=<same-value-as-worker> --app boostedbudget-api
```

---

## 5. Apply Database Migrations

```bash
DATABASE_URL=<staging-db-url> ./scripts/test-migrations.sh
```

Required migrations: 000084 (mcp_audit_log), 000085 (mcp_recipes), 000086 (idempotency_keys), 000087 (idempotency_keys PK fix).

---

## 6. Deploy the Worker

```bash
# Staging
wrangler deploy --env staging

# Verify Durable Object migrations apply:
wrangler migrations apply --env staging   # wrangler v4 usually handles this on deploy

# Production (after staging validation passes)
wrangler deploy
```

---

## 7. Cloudflare Transform Rule (Belt-and-Suspenders)

The Worker injects `X-Origin-Secret` via its binding. A CF Transform Rule provides an
additional layer for belt-and-suspenders. See `scripts/setup-cloudflare.py` and
`docs/FLYIO_SETUP.md §"Inject Origin Secret"` for setup via the Cloudflare API.

---

## 8. DNS

Add a Cloudflare DNS record (orange-clouded):

```
Type  Name                           Content (proxied)
A     staging-mcp.boostedbudget.com  192.0.2.1    (proxied)
A     mcp.boostedbudget.com          192.0.2.1    (proxied)
```

---

## 9. Validate with Claude Desktop

1. Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "boostedbudget-staging": {
      "type": "remote",
      "url": "https://staging-mcp.boostedbudget.com/mcp"
    }
  }
}
```

2. Restart Claude Desktop
3. Complete the OAuth flow (browser opens → Clerk consent → redirects back)
4. Run the example prompts from [README.md](./README.md)

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| 401 on all requests | `MCP_OAUTH_AUDIENCE` mismatch or Clerk JWKS not configured | Verify Clerk OAuth app Client ID matches `MCP_OAUTH_AUDIENCE` |
| 403 on audit endpoint | `MCP_WORKER_SECRET` mismatch between Worker and backend | Ensure same value set via `wrangler secret put` and `fly secrets set` |
| 404 on `mcp.*` domain | DNS not configured or not proxied | Add orange-clouded A record in Cloudflare DNS |
| Recipe schedule rejected | The `schedule` field is not available in v1 | Omit `schedule` — it will be added in a future release (#1220) |
