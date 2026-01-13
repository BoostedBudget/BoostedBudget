# Tailscale HTTPS Limitation - Staging Environment

## Issue

Authentication SDKs (including Clerk) require HTTPS for all non-localhost origins for security reasons. This means:
- ✅ **http://localhost:3000** - Works perfectly (localhost is exempt)
- ❌ **http://yingyues-mac-mini.tailce219d.ts.net:3000** - Blocked by security requirements

Error: Authentication must run on a secure origin (HTTPS) for non-localhost domains.

## Root Cause

Tailscale free tier doesn't support automatic HTTPS certificate provisioning:
```bash
$ tailscale cert yingyues-mac-mini.tailce219d.ts.net
500 Internal Server Error: your Tailscale account does not support getting TLS certs
```

## Current Solution

**Use localhost for testing**: The staging environment is fully functional on **http://localhost:3000**

This works for:
- Local testing on the Mac Mini
- SSH tunnel access: `ssh -L 3000:localhost:3000 user@mac-mini`
- All Clerk authentication flows
- Profile, accounts, transactions pages
- Auto-registration on first login

## Alternative Solutions

### Option 1: Upgrade Tailscale Plan (Recommended for Production)

Upgrade to Tailscale plan with HTTPS support:
1. Visit https://login.tailscale.com/admin/billing
2. Upgrade to plan with HTTPS certificates
3. Run: `tailscale cert yingyues-mac-mini.tailce219d.ts.net`
4. Configure Vite to use HTTPS (see Option 4 below)

**Cost**: Varies by plan (~$5-20/month)
**Effort**: Low (automated certificate provisioning)

### Option 2: Use Tailscale Funnel (If Available)

Tailscale Funnel provides HTTPS access through Tailscale's infrastructure:
```bash
tailscale funnel 3000
```

Check if available on your plan: https://tailscale.com/kb/1223/tailscale-funnel/

### Option 3: Use ngrok for HTTPS Tunnel

Set up HTTPS tunnel through ngrok:
```bash
# Install ngrok (if not already installed)
brew install ngrok

# Start HTTPS tunnel
ngrok http 3000
```

ngrok provides: **https://random-name.ngrok.io** → **http://localhost:3000**

Update Clerk allowed origins to include the ngrok URL.

**Pros**: Free tier available, automatic HTTPS
**Cons**: URL changes on restart (unless using paid plan), external dependency

### Option 4: Self-Signed Certificate (Development Only)

Create self-signed certificate for development:
```bash
# Generate certificate
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes \
  -subj "/CN=yingyues-mac-mini.tailce219d.ts.net"

# Move to project
mv key.pem cert.pem /Users/yingyueliu/coding/BoostedBudget/apps/web/
```

Update `apps/web/vite.config.ts`:
```typescript
import fs from 'fs'

export default defineConfig({
  server: {
    https: {
      key: fs.readFileSync('./key.pem'),
      cert: fs.readFileSync('./cert.pem'),
    },
    // ... rest of config
  }
})
```

Update docker-compose.staging.yml ports: `3000:3000` → `3443:3443`

**Pros**: Free, works offline
**Cons**: Browser security warnings, need to accept certificate manually

### Option 5: SSH Tunnel (Simplest for Remote Access)

Access staging from any machine via SSH tunnel:
```bash
# From remote machine
ssh -L 3000:localhost:3000 yingyueliu@mac-mini-ip

# Then access http://localhost:3000 in browser
```

**Pros**: Secure, no additional setup, works with Clerk
**Cons**: Requires SSH access, terminal must stay open

## Recommendation

**For current staging use**: Use **localhost:3000** (already working perfectly)

**For remote access**: Use **SSH tunnel** (Option 5) - simple and secure

**For production-like staging**: Upgrade Tailscale plan (Option 1) - professional solution

## Files Modified

- `apps/web/public/config.js` - Created to fix config.js loading in dev mode
- `apps/web/vite.config.ts` - Updated CORS and host settings for Tailscale compatibility
- `docs/TAILSCALE_HTTPS_LIMITATION.md` - This documentation

## Verification

Staging environment is working correctly on localhost:
```bash
# Health check
curl http://localhost:8080/health

# Frontend check
curl http://localhost:3000/

# Test in browser
open http://localhost:3000
```

All endpoints (profile, accounts, transactions) working correctly.
Auto-registration functioning as expected.

---

*Last updated: 2025-10-28*
*Related: #256 (Transaction Sync Service), Auto-registration implementation*
