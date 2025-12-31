# Clerk Setup Guide for BoostedBudget

This guide walks you through setting up Clerk for BoostedBudget's authentication system.

## Table of Contents
1. [Create Clerk Account](#1-create-clerk-account)
2. [Create Application](#2-create-application)
3. [Configure JWT Templates](#3-configure-jwt-templates)
4. [Configure Public Metadata](#4-configure-public-metadata)
5. [Get API Keys](#5-get-api-keys)
6. [Update Local Environment](#6-update-local-environment)
7. [Configure GitHub Secrets](#7-configure-github-secrets)
8. [Testing Authentication](#8-testing-authentication)

---

## 1. Create Clerk Account

1. Go to [clerk.com](https://clerk.com)
2. Click **Get started** (Free tier includes 10,000 monthly active users)
3. Create your account using email or OAuth
4. Verify your email address

---

## 2. Create Application

### 2.1 Create New Application
1. Navigate to [Clerk Dashboard](https://dashboard.clerk.com)
2. Click **+ Create application**
3. Fill in:
   - **Application name**: `BoostedBudget QA` (or `BoostedBudget Staging`, `BoostedBudget Production`)
   - **Sign-in options**: Select desired providers (Email, Google, GitHub, etc.)
4. Click **Create application**

### 2.2 Configure Application Settings
1. Go to **Configure** → **User & Authentication** → **Email, Phone, Username**
2. Enable/disable authentication methods as needed
3. Go to **Configure** → **User & Authentication** → **Social Connections**
4. Enable desired OAuth providers (Google, GitHub, etc.)

### 2.3 Configure Allowed Origins
1. Go to **Configure** → **Settings**
2. Under **Allowed origins**, add:
   ```
   http://localhost:3000
   http://localhost:8080
   https://your-qa-domain.com
   ```
3. Click **Save**

---

## 3. Configure JWT Templates

Custom claims allow the backend to access user metadata from the JWT token.

### 3.1 Create JWT Template
1. Go to **Configure** → **JWT Templates**
2. Click **+ New template**
3. Select **Blank** template
4. Name it: `BoostedBudget`

### 3.2 Configure Claims
In the **Claims** section, add the following JSON:

```json
{
  "subscription_tier": "{{user.public_metadata.subscription_tier}}",
  "primary_region": "{{user.public_metadata.primary_region}}",
  "email": "{{user.primary_email_address}}",
  "email_verified": "{{user.email_verified}}"
}
```

### 3.3 Configure Token Lifetime
1. **Token lifetime**: `3600` (1 hour, adjust as needed)
2. Click **Save**

### 3.4 Set as Default (Optional)
If you want this template to be used by default:
1. Click the three-dot menu on the template
2. Select **Set as default**

---

## 4. Configure Public Metadata

Public metadata is readable by the frontend and can be set via the Backend API.

### 4.1 Default User Metadata
When a user signs up, set default metadata via Clerk webhooks or Backend API:

```json
{
  "subscription_tier": "free",
  "primary_region": "iad"
}
```

### 4.2 Update Metadata via Backend API
Use the Clerk Backend API to update user metadata:

```go
import "github.com/clerk/clerk-sdk-go/v2/user"

// Update user's subscription tier
_, err := user.Update(ctx, userID, &user.UpdateParams{
    PublicMetadata: &map[string]interface{}{
        "subscription_tier": "premium",
    },
})
```

---

## 5. Get API Keys

### 5.1 Navigate to API Keys
1. Go to **Configure** → **API keys**

### 5.2 Note Down Keys

| Key Type | Location | Environment Variable |
|----------|----------|---------------------|
| **Publishable Key** | Visible on page | `VITE_CLERK_PUBLISHABLE_KEY` (frontend) |
| **Secret Key** | Click "Reveal" | `CLERK_SECRET_KEY` (backend) |

**Important**:
- **Publishable Key** starts with `pk_test_` (development) or `pk_live_` (production)
- **Secret Key** starts with `sk_test_` (development) or `sk_live_` (production)
- Never commit secret keys to version control

---

## 6. Update Local Environment

### 6.1 Update .env File
Copy `.env.example` to `.env` and update:

```bash
# Clerk Configuration
CLERK_SECRET_KEY=sk_test_your_secret_key_here
VITE_CLERK_PUBLISHABLE_KEY=pk_test_your_publishable_key_here
```

### 6.2 Verify Configuration
Test that your keys are valid by making a request:

```bash
curl -H "Authorization: Bearer $CLERK_SECRET_KEY" \
  https://api.clerk.com/v1/users?limit=1
```

You should receive a JSON response (even if empty array).

---

## 7. Configure GitHub Secrets

For CI/CD pipeline and deployments, add these secrets to your GitHub repository:

### 7.1 Navigate to GitHub Secrets
1. Go to your repository: `https://github.com/BoostedBudget/BoostedBudget`
2. Click **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret**

### 7.2 Add Clerk Secrets

| Secret Name | Value | Description |
|-------------|-------|-------------|
| `CLERK_SECRET_KEY` | `sk_test_...` | Backend secret key |
| `CLERK_PUBLISHABLE_KEY` | `pk_test_...` | Frontend publishable key |

### 7.3 For Each Secret:
1. Click **New repository secret**
2. Enter **Name** (e.g., `CLERK_SECRET_KEY`)
3. Enter **Secret** value
4. Click **Add secret**

### 7.4 Verify Secrets
After adding all secrets, you should see:
- ✅ `CLERK_SECRET_KEY`
- ✅ `CLERK_PUBLISHABLE_KEY`

---

## 8. Testing Authentication

### 8.1 Test with Clerk Dashboard
1. Go to **Users** in the Clerk Dashboard
2. Click **+ Create user** to create a test user
3. Set email and password for testing

### 8.2 Test Locally
Once the backend and frontend Clerk integration is complete:

```bash
# Start development environment
docker-compose up

# Frontend should be at: http://localhost:3000
# Backend should be at: http://localhost:8080
```

1. Navigate to `http://localhost:3000`
2. Click **Sign Up** or **Log In**
3. You should see Clerk's sign-in UI (modal or redirect based on config)
4. Create a test account or use existing credentials
5. After login, you should be redirected back to the app

### 8.3 Verify JWT Token
After logging in, verify the token contains custom claims:

```javascript
// In browser console (after login)
// Access token from Clerk
const token = await window.Clerk.session.getToken();

// Decode (using jwt.io or browser console)
const payload = JSON.parse(atob(token.split('.')[1]));
console.log(payload);

// You should see custom claims:
// {
//   "subscription_tier": "free",
//   "primary_region": "iad",
//   "email": "user@example.com",
//   ...
// }
```

---

## Troubleshooting

### Issue: "Invalid Publishable Key"
**Solution**: Ensure you're using the correct key format (`pk_test_...` or `pk_live_...`) and it matches your environment.

### Issue: "Unauthorized" from Backend
**Solution**:
- Verify `CLERK_SECRET_KEY` is set correctly
- Ensure the JWT is being passed in the `Authorization: Bearer <token>` header
- Check that the token hasn't expired

### Issue: "CORS Error"
**Solution**: Add your origin to **Configure** → **Settings** → **Allowed origins** in the Clerk Dashboard.

### Issue: Custom Claims Not in Token
**Solution**:
- Verify JWT template is configured correctly
- Ensure the template is set as default or explicitly requested
- Check that user's public_metadata contains the values

### Issue: "User Not Found" in Backend
**Solution**:
- The user may not be registered in the local database yet
- Auto-registration should create the user on first login
- Check identity repository logs

---

## Security Best Practices

✅ **DO**:
- Use separate Clerk applications for dev/staging/production
- Use test keys (`pk_test_`, `sk_test_`) for development
- Keep Secret Keys in environment variables/secrets (never commit to git)
- Enable MFA for production users
- Regularly rotate Secret Keys
- Use HTTPS for all production endpoints

❌ **DON'T**:
- Commit `.env` files to git
- Share Secret Keys in Slack/email
- Use production keys in development
- Expose Secret Keys in frontend code
- Skip JWT validation on backend

---

## Production Checklist

Before deploying to production:

- [ ] Use production Clerk application (not development)
- [ ] Use live keys (`pk_live_`, `sk_live_`)
- [ ] Configure custom domain (optional but recommended)
- [ ] Enable Multi-Factor Authentication (MFA)
- [ ] Set up password policies
- [ ] Configure session timeouts
- [ ] Set up Clerk webhooks for user events
- [ ] Enable audit logging
- [ ] Configure rate limiting

---

## Clerk SDK Versions

**Frontend (React)**:
```json
"@clerk/clerk-react": "^5.54.0"
```

**Backend (Go)**:
```
github.com/clerk/clerk-sdk-go/v2 v2.5.0
```

---

## Additional Resources

- [Clerk React Documentation](https://clerk.com/docs/quickstarts/react)
- [Clerk Go SDK Documentation](https://clerk.com/docs/references/go/overview)
- [Clerk JWT Templates](https://clerk.com/docs/backend-requests/making/jwt-templates)
- [Clerk Webhooks](https://clerk.com/docs/integrations/webhooks)
- [Clerk API Reference](https://clerk.com/docs/reference/backend-api)

---

**Last Updated**: 2025-12-29
**Issue**: #593
**Related Issues**: #598 (Staging), #599 (Production)
