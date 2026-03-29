---
name: qa-login
description: Authenticate into the BoostedBudget QA or Staging environment using the auth bypass endpoint and agent-browser. Use when "login to QA", "authenticate to staging", "test the QA app", "dogfood QA", "open the QA app", or starting any QA/Staging browser testing session.
argument-hint: "[role: admin|premium]  (default: admin)"
---

# QA Auth Bypass Login

Authenticate into the QA or Staging BoostedBudget app via the Clerk sign-in token bypass endpoint, then open the app in `agent-browser` ready for testing or dogfooding.

## Step 1: Determine Role

Role is taken from `$ARGUMENTS` (e.g., `/qa-login premium`). Default to `admin` if not provided.

Valid roles:
| Role | Email | Use for |
|------|-------|---------|
| `admin` | qa-admin@boostedbudget.com | Admin features, full access |
| `premium` | qa-premium@boostedbudget.com | Premium user flows |

## Step 2: Read Config from .env.qa

```bash
grep -E "^(QA_BYPASS_SECRET|FRONTEND_URL|BACKEND_URL)" .env.qa 2>/dev/null
```

Extract:
- `QA_BYPASS_SECRET` — the shared secret for the bypass endpoint
- `BACKEND_URL` — backend base URL (e.g., `https://qa.example.ts.net:3443`)
- `FRONTEND_URL` — frontend base URL (same host for QA)

If `.env.qa` is missing or `QA_BYPASS_SECRET` is empty, stop and tell the user to configure `.env.qa` (see `.env.qa.example`).

## Step 3: Call the Bypass Endpoint

Use `rtk proxy` to bypass RTK output filtering and get the full URL:

```bash
rtk proxy curl -s -X POST "<BACKEND_URL>/api/v1/qa/auth/bypass" \
  -H "Content-Type: application/json" --insecure \
  -d '{"role":"<ROLE>","secret":"<QA_BYPASS_SECRET>"}'
```

Parse the `url` field from the JSON response:
```json
{"url":"https://<clerk-instance>.accounts.dev/sign-in?__clerk_ticket=...","email":"qa-<role>@boostedbudget.com","tier":"premium"}
```

**Error handling:**
| Response | Cause | Fix |
|----------|-------|-----|
| `401` RFC 9457 format (`{"status":401,"title":"Unauthorized"}`) | Wrong secret | Check `QA_BYPASS_SECRET` in `.env.qa` |
| `404` | Endpoint not registered | Env vars missing from Docker container; redeploy using `deploy-qa.yml` from the feature branch |
| `401` plain format (`{"message":"Missing authorization header"}`) | Route not registered | Same as 404 cause — env vars not in container |

## Step 4: Authenticate with agent-browser

The QA app uses a self-signed SSL cert. `agent-browser.json` at the project root already sets `ignoreHttpsErrors: true`.

Close any stale session, then navigate to the Clerk sign-in token URL:

```bash
agent-browser close 2>/dev/null || true
agent-browser open "<CLERK_SIGN_IN_URL>"
agent-browser wait --load networkidle
agent-browser wait 3000
agent-browser get url
```

Expected URL after redirect: contains `/dashboard` on the QA domain.

If the browser lands on the marketing homepage or Clerk's sign-in page, the token expired — repeat Step 3 to get a fresh one (tokens expire in 5 minutes).

## Step 5: Accept Cookie Consent

Take a snapshot and dismiss the cookie banner if present:

```bash
agent-browser snapshot -i
```

If `button "Accept all cookies"` appears, click it:

```bash
agent-browser click <ref>
agent-browser wait 500
```

## Step 6: Report + Optional Dogfood

Tell the user:
> Authenticated as `<EMAIL>` on `<FRONTEND_URL>/dashboard`.

If the user asked to dogfood or explore the app, invoke the `/dogfood` skill now — the browser is already open and authenticated. Follow the dogfood skill's workflow from Step 3 (Orient), skipping Steps 1–2 (Initialize and Authenticate).
