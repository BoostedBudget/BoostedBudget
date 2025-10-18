# Quick Auth0 Reference for Issue #6

Quick reference for setting up Auth0 after merging PR #73 (Frontend Auth0 Integration).

## TL;DR - What You Need

### 1. Local Development (`.env` file)

Update your root `.env` file with these 7 variables:

```bash
# From Auth0 Backend M2M Application
AUTH0_DOMAIN=your-tenant.us.auth0.com
AUTH0_CLIENT_ID=<M2M_CLIENT_ID>
AUTH0_CLIENT_SECRET=<M2M_CLIENT_SECRET>
AUTH0_AUDIENCE=https://api.boostedbudget.com

# From Auth0 Frontend SPA Application
VITE_AUTH0_DOMAIN=your-tenant.us.auth0.com
VITE_AUTH0_CLIENT_ID=<SPA_CLIENT_ID>  # ⚠️ DIFFERENT from backend!
VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com
```

### 2. GitHub Secrets (for CI/CD)

Add these 7 secrets in: **Settings → Secrets and variables → Actions**

```
AUTH0_DOMAIN
AUTH0_CLIENT_ID
AUTH0_CLIENT_SECRET
AUTH0_AUDIENCE
VITE_AUTH0_DOMAIN
VITE_AUTH0_CLIENT_ID
VITE_AUTH0_AUDIENCE
```

---

## Two Auth0 Applications Required

You need **TWO** different Auth0 applications:

| Application | Type | Purpose | Used By |
|-------------|------|---------|---------|
| **BoostedBudget Backend API** | Machine-to-Machine (M2M) | Backend validates user tokens | Go backend |
| **BoostedBudget Frontend** | Single Page App (SPA) | Users log in via Universal Login | React frontend |

⚠️ **Important:** They have **DIFFERENT** Client IDs!

---

## Quick Setup Steps

### Step 1: Create Auth0 Account
- Go to [auth0.com](https://auth0.com) → Sign Up (free)
- Choose tenant domain: `yourname.us.auth0.com`
- Region: United States

### Step 2: Create Backend M2M Application
1. Auth0 Dashboard → Applications → Create Application
2. Name: `BoostedBudget Backend API`
3. Type: **Machine to Machine**
4. Copy: Domain, Client ID, Client Secret
5. Paste into `.env`:
   ```bash
   AUTH0_DOMAIN=yourname.us.auth0.com
   AUTH0_CLIENT_ID=<copy from M2M app>
   AUTH0_CLIENT_SECRET=<copy from M2M app>
   ```

### Step 3: Create Frontend SPA Application
1. Auth0 Dashboard → Applications → Create Application
2. Name: `BoostedBudget Frontend`
3. Type: **Single Page Web Application**
4. Copy: Client ID (different from backend!)
5. Paste into `.env`:
   ```bash
   VITE_AUTH0_CLIENT_ID=<copy from SPA app>
   ```

### Step 4: Configure Frontend SPA URLs
In the SPA application settings:

```
Allowed Callback URLs:
http://localhost:3000, http://localhost:3000/callback

Allowed Logout URLs:
http://localhost:3000

Allowed Web Origins:
http://localhost:3000
```

### Step 5: Create API
1. Auth0 Dashboard → APIs → Create API
2. Name: `BoostedBudget API`
3. Identifier: `https://api.boostedbudget.com` (exact value!)
4. Paste into `.env`:
   ```bash
   AUTH0_AUDIENCE=https://api.boostedbudget.com
   VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com
   ```

### Step 6: Test Locally
```bash
# Start backend
cd backend
go run cmd/server/main.go

# Start frontend
cd frontend
npm run dev

# Open http://localhost:3000
# Click "Log In" button
# Should see Auth0 Universal Login
```

---

## Credential Mapping Cheat Sheet

| `.env` Variable | Get From | App Type |
|----------------|----------|----------|
| `AUTH0_DOMAIN` | Backend M2M → Settings → Domain | M2M |
| `AUTH0_CLIENT_ID` | Backend M2M → Settings → Client ID | M2M |
| `AUTH0_CLIENT_SECRET` | Backend M2M → Settings → Client Secret | M2M |
| `AUTH0_AUDIENCE` | APIs → BoostedBudget API → Identifier | API |
| `VITE_AUTH0_DOMAIN` | Same as AUTH0_DOMAIN | SPA |
| `VITE_AUTH0_CLIENT_ID` | Frontend SPA → Settings → Client ID | SPA |
| `VITE_AUTH0_AUDIENCE` | Same as AUTH0_AUDIENCE | API |

---

## Common Mistakes

❌ **Using same Client ID for backend and frontend**
- Backend needs M2M Client ID
- Frontend needs SPA Client ID
- They are DIFFERENT!

❌ **Wrong Callback URL**
- Must include `http://localhost:3000/callback`
- Don't forget the `/callback` path

❌ **Audience mismatch**
- Must be exactly: `https://api.boostedbudget.com`
- It's an identifier, not a real URL

❌ **Committing `.env` to git**
- `.env` is in `.gitignore` (safe)
- `.env.example` is the template (safe to commit)

---

## Testing Checklist

After setup, verify:

- [ ] Frontend starts without Auth0 warnings
- [ ] Click "Log In" → See Auth0 Universal Login
- [ ] Create test account → Redirects back to app
- [ ] Browser console shows user object
- [ ] Network tab shows `Authorization: Bearer ...` header
- [ ] Backend logs show successful JWT validation

---

## Troubleshooting

**"Invalid state" after login**
→ Add `http://localhost:3000/callback` to Allowed Callback URLs

**"Invalid audience"**
→ Verify `VITE_AUTH0_AUDIENCE` matches `AUTH0_AUDIENCE`

**CORS errors**
→ Add `http://localhost:3000` to Allowed Web Origins in SPA app

**Backend can't validate tokens**
→ Check you're using M2M credentials, not SPA credentials

---

## Full Documentation

For complete setup guide with screenshots and advanced configuration:
- 📖 [AUTH0_SETUP.md](./AUTH0_SETUP.md) - Comprehensive guide
- 🔐 [GITHUB_SECRETS_SETUP.md](./GITHUB_SECRETS_SETUP.md) - CI/CD secrets

---

## Production Deployment

When deploying to Fly.io:

```bash
# Set secrets for backend
fly secrets set \
  AUTH0_DOMAIN=yourname.us.auth0.com \
  AUTH0_CLIENT_ID=<M2M_ID> \
  AUTH0_CLIENT_SECRET=<M2M_SECRET> \
  AUTH0_AUDIENCE=https://api.boostedbudget.com \
  -a boostedbudget-backend

# Set secrets for frontend
fly secrets set \
  VITE_AUTH0_DOMAIN=yourname.us.auth0.com \
  VITE_AUTH0_CLIENT_ID=<SPA_ID> \
  VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com \
  -a boostedbudget-frontend
```

Then update Auth0 SPA app to include production URLs:
```
Allowed Callback URLs:
http://localhost:3000, http://localhost:3000/callback, https://boostedbudget.com, https://boostedbudget.com/callback
```

---

**Related:**
- Issue #6: Frontend Auth0 Integration
- PR #73: https://github.com/maxnussbaum/BoostedBudget/pull/73

**Created:** 2025-10-18
