# Auth0 Setup Guide for BoostedBudget

This guide walks you through setting up Auth0 for BoostedBudget's global authentication system.

## Table of Contents
1. [Create Auth0 Account](#1-create-auth0-account)
2. [Configure Tenant](#2-configure-tenant)
3. [Create Backend API Application (M2M)](#3-create-backend-api-application-m2m)
4. [Create Frontend Application (SPA)](#4-create-frontend-application-spa)
5. [Configure Auth0 Actions](#5-configure-auth0-actions)
6. [Update Local Environment](#6-update-local-environment)
7. [Configure GitHub Secrets](#7-configure-github-secrets)
8. [Testing Authentication](#8-testing-authentication)

---

## 1. Create Auth0 Account

1. Go to [auth0.com](https://auth0.com)
2. Click **Sign Up** (Free tier includes 7,000 active users)
3. Choose **Personal** account type
4. Verify your email address

---

## 2. Configure Tenant

### 2.1 Create Tenant
1. Navigate to **Settings** → **Tenant Settings**
2. Set **Tenant Name**: `boostedbudget` (or your preferred name)
3. Set **Region**: Choose closest to `us-east-1` (United States)
4. Click **Save**

### 2.2 Configure Universal Login
1. Go to **Branding** → **Universal Login**
2. **Experience**: Select **New Universal Login**
3. **Logo URL**: (Optional) Add your logo
4. **Primary Color**: `#4F46E5` (or your brand color)
5. **Page Background**: Choose **Color** or upload image
6. Click **Save Changes**

### 2.3 Custom Domain (Optional - Production)
For production, configure custom domain `auth.boostedbudget.com`:
1. Go to **Branding** → **Custom Domains**
2. Click **+ Set Up Custom Domain**
3. Enter: `auth.boostedbudget.com`
4. Follow DNS verification steps
5. **Skip this for development**

---

## 3. Create Backend API Application (M2M)

This application allows the Go backend to validate JWT tokens.

### 3.1 Create API
1. Navigate to **Applications** → **APIs**
2. Click **+ Create API**
3. Fill in:
   - **Name**: `BoostedBudget API`
   - **Identifier**: `https://api.boostedbudget.com`
   - **Signing Algorithm**: `RS256`
4. Click **Create**

### 3.2 Configure API Settings
1. Go to **Settings** tab
2. **Token Expiration (Seconds)**: `86400` (24 hours)
3. **Allow Offline Access**: `ON`
4. **Allow Skipping User Consent**: `OFF`
5. Click **Save**

### 3.3 Enable RBAC
1. Still in **Settings** tab
2. **Enable RBAC**: `ON`
3. **Add Permissions in the Access Token**: `ON`
4. Click **Save**

### 3.4 Define Permissions
1. Go to **Permissions** tab
2. Add these permissions:
   - `read:profile` - Read user profile
   - `write:profile` - Update user profile
   - `read:accounts` - Read financial accounts
   - `write:accounts` - Manage financial accounts
   - `read:budgets` - Read budgets
   - `write:budgets` - Manage budgets
   - `read:rules` - Read automation rules
   - `write:rules` - Manage automation rules
   - `delete:account` - Delete user account (GDPR)
   - `export:data` - Export user data (GDPR)

### 3.5 Get API Credentials
1. Note down:
   - **Identifier**: `https://api.boostedbudget.com` → This is your `AUTH0_AUDIENCE`

---

## 4. Create Frontend Application (SPA)

This application handles user login in the React frontend.

### 4.1 Create Application
1. Navigate to **Applications** → **Applications**
2. Click **+ Create Application**
3. Fill in:
   - **Name**: `BoostedBudget Web App`
   - **Application Type**: **Single Page Web Applications**
4. Click **Create**

### 4.2 Configure Application Settings
1. Go to **Settings** tab
2. **Application Logo** (optional): Upload logo
3. **Allowed Callback URLs**:
   ```
   http://localhost:3000/callback,
   https://boostedbudget.com/callback,
   https://staging.boostedbudget.com/callback
   ```
4. **Allowed Logout URLs**:
   ```
   http://localhost:3000,
   https://boostedbudget.com,
   https://staging.boostedbudget.com
   ```
5. **Allowed Web Origins**:
   ```
   http://localhost:3000,
   https://boostedbudget.com,
   https://staging.boostedbudget.com
   ```
6. **Allowed Origins (CORS)**:
   ```
   http://localhost:3000,
   https://boostedbudget.com,
   https://staging.boostedbudget.com
   ```
7. **Application Login URI**: `http://localhost:3000/login` (for development)
8. Click **Save Changes**

### 4.3 Get Application Credentials
1. Still in **Settings** tab, note down:
   - **Domain**: e.g., `boostedbudget.auth0.com` → This is your `AUTH0_DOMAIN`
   - **Client ID**: e.g., `abc123...` → This is your `AUTH0_CLIENT_ID`
   - **Client Secret**: e.g., `xyz789...` → This is your `AUTH0_CLIENT_SECRET`

### 4.4 Configure Token Settings
1. Scroll to **Advanced Settings** → **OAuth**
2. **JsonWebToken Signature Algorithm**: `RS256`
3. **OIDC Conformant**: `ON`
4. Click **Save Changes**

### 4.5 Grant API Access
1. Go to **APIs** tab
2. Expand `BoostedBudget API`
3. Enable all permissions:
   - ✅ `read:profile`
   - ✅ `write:profile`
   - ✅ `read:accounts`
   - ✅ `write:accounts`
   - ✅ `read:budgets`
   - ✅ `write:budgets`
   - ✅ `read:rules`
   - ✅ `write:rules`
   - ✅ `delete:account`
   - ✅ `export:data`
4. Click **Update**

---

## 5. Configure Auth0 Actions

Actions replace the legacy Rules system. We'll create an action to add user metadata to JWT tokens.

### 5.1 Create Add User Metadata Action
1. Navigate to **Actions** → **Library**
2. Click **+ Build Custom**
3. Fill in:
   - **Name**: `Add User Metadata`
   - **Trigger**: **Login / Post Login**
   - **Runtime**: **Node 18**
4. Click **Create**

### 5.2 Add Code
Replace the default code with:

```javascript
/**
 * Handler that will be called during the execution of a PostLogin flow.
 *
 * @param {Event} event - Details about the user and the context in which they are logging in.
 * @param {PostLoginAPI} api - Interface whose methods can be used to change the behavior of the login.
 */
exports.onExecutePostLogin = async (event, api) => {
  const namespace = 'https://boostedbudget.com';

  // Get user metadata
  const userMetadata = event.user.user_metadata || {};
  const appMetadata = event.user.app_metadata || {};

  // Add subscription tier (default to 'free')
  const subscriptionTier = appMetadata.subscription_tier || 'free';

  // Add primary region (default to 'us-east-1')
  const primaryRegion = userMetadata.primary_region || 'us-east-1';

  // Add GDPR consent tracking
  const gdprConsent = {
    necessary: userMetadata.gdpr_necessary_consent || true,
    analytics: userMetadata.gdpr_analytics_consent || false,
    advertising: userMetadata.gdpr_advertising_consent || false,
    timestamp: userMetadata.gdpr_consent_timestamp || new Date().toISOString()
  };

  // Add custom claims to access token
  api.accessToken.setCustomClaim(`${namespace}/subscription_tier`, subscriptionTier);
  api.accessToken.setCustomClaim(`${namespace}/primary_region`, primaryRegion);
  api.accessToken.setCustomClaim(`${namespace}/gdpr_consent`, gdprConsent);

  // Add user_id for convenience
  api.accessToken.setCustomClaim(`${namespace}/user_id`, event.user.user_id);

  // Add email to ID token (for frontend use)
  api.idToken.setCustomClaim(`${namespace}/email`, event.user.email);
  api.idToken.setCustomClaim(`${namespace}/subscription_tier`, subscriptionTier);
};
```

### 5.3 Deploy Action
1. Click **Deploy** (top right)
2. Go to **Actions** → **Flows** → **Login**
3. Click **Custom** tab on the right
4. Drag **Add User Metadata** action to the flow
5. Click **Apply**

---

## 6. Update Local Environment

### 6.1 Update .env File
Copy `.env.example` to `.env` and update:

```bash
# Auth0 Configuration
AUTH0_DOMAIN=your-tenant.auth0.com          # From Frontend App Settings
AUTH0_AUDIENCE=https://api.boostedbudget.com  # From API Identifier
AUTH0_CLIENT_ID=your_auth0_client_id         # From Frontend App Settings
AUTH0_CLIENT_SECRET=your_auth0_client_secret # From Frontend App Settings
```

### 6.2 Verify Configuration
Run this command to verify your Auth0 setup:

```bash
curl https://${AUTH0_DOMAIN}/.well-known/openid-configuration
```

You should see JSON with `issuer`, `authorization_endpoint`, `token_endpoint`, etc.

---

## 7. Configure GitHub Secrets

For CI/CD pipeline and deployments, add these secrets to your GitHub repository:

### 7.1 Navigate to GitHub Secrets
1. Go to your repository: `https://github.com/maxnussbaum/BoostedBudget`
2. Click **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret**

### 7.2 Add Auth0 Secrets

Add each of these as separate secrets:

| Secret Name | Value | Description |
|-------------|-------|-------------|
| `AUTH0_DOMAIN` | `your-tenant.auth0.com` | Your Auth0 tenant domain |
| `AUTH0_AUDIENCE` | `https://api.boostedbudget.com` | API identifier |
| `AUTH0_CLIENT_ID` | `abc123...` | Frontend SPA Client ID |
| `AUTH0_CLIENT_SECRET` | `xyz789...` | Frontend SPA Client Secret |

### 7.3 For Each Secret:
1. Click **New repository secret**
2. Enter **Name** (e.g., `AUTH0_DOMAIN`)
3. Enter **Secret** value
4. Click **Add secret**

### 7.4 Verify Secrets
After adding all secrets, you should see:
- ✅ `AUTH0_DOMAIN`
- ✅ `AUTH0_AUDIENCE`
- ✅ `AUTH0_CLIENT_ID`
- ✅ `AUTH0_CLIENT_SECRET`

**Note**: These secrets will be automatically injected into your CI/CD workflows and Fly.io deployments.

---

## 8. Testing Authentication

### 8.1 Test with Auth0 Dashboard
1. Go to **Applications** → **Applications** → **BoostedBudget Web App**
2. Click **Quick Start** tab
3. Select **React** framework
4. Follow the test flow to verify login works

### 8.2 Test Locally
Once the backend Auth0 middleware (Issue #5) and frontend integration (Issue #6) are complete:

```bash
# Start development environment
docker-compose up

# Frontend should be at: http://localhost:3000
# Backend should be at: http://localhost:8080
```

1. Navigate to `http://localhost:3000`
2. Click **Sign Up** or **Log In**
3. You should be redirected to Auth0 Universal Login
4. Create a test account
5. After login, you should be redirected back to the app

### 8.3 Verify JWT Token
After logging in, open browser DevTools:

```javascript
// Get access token from Auth0
const token = localStorage.getItem('access_token'); // or from auth0-react hook

// Decode (using jwt.io or browser console)
const payload = JSON.parse(atob(token.split('.')[1]));
console.log(payload);

// You should see custom claims:
// {
//   "https://boostedbudget.com/subscription_tier": "free",
//   "https://boostedbudget.com/primary_region": "us-east-1",
//   "https://boostedbudget.com/gdpr_consent": {...},
//   "https://boostedbudget.com/user_id": "auth0|...",
//   ...
// }
```

---

## Troubleshooting

### Issue: "Invalid Callback URL"
**Solution**: Make sure the callback URL in your app matches exactly what's configured in Auth0 (including http vs https, trailing slashes, etc.)

### Issue: "Access Denied"
**Solution**: Check that the API permissions are properly granted to the frontend application (Step 4.5)

### Issue: "Invalid Audience"
**Solution**: Verify `AUTH0_AUDIENCE` matches the API Identifier exactly

### Issue: "CORS Error"
**Solution**: Add your origin to **Allowed Web Origins** and **Allowed Origins (CORS)** in Auth0 application settings

---

## Security Best Practices

✅ **DO**:
- Use RS256 for JWT signing (asymmetric)
- Enable RBAC for fine-grained permissions
- Keep Client Secrets in environment variables/secrets (never commit to git)
- Use different Auth0 tenants for dev/staging/production
- Enable MFA for production users
- Regularly rotate Client Secrets

❌ **DON'T**:
- Commit `.env` files to git
- Share Client Secrets in Slack/email
- Use HS256 signing (symmetric keys)
- Skip Auth0 Actions for custom claims
- Disable OIDC Conformant mode

---

## Production Checklist

Before deploying to production:

- [ ] Configure custom domain (`auth.boostedbudget.com`)
- [ ] Enable Multi-Factor Authentication (MFA)
- [ ] Set up anomaly detection rules
- [ ] Configure password policies
- [ ] Set up email templates (verification, password reset)
- [ ] Enable brute force protection
- [ ] Configure session timeouts
- [ ] Set up Auth0 logs forwarding
- [ ] Create separate production tenant
- [ ] Enable Auth0 Monitoring

---

## Additional Resources

- [Auth0 React SDK](https://github.com/auth0/auth0-react)
- [Auth0 Go SDK](https://github.com/auth0/go-jwt-middleware)
- [Auth0 Actions Documentation](https://auth0.com/docs/customize/actions)
- [OIDC Conformant Applications](https://auth0.com/docs/authenticate/login/oidc-conformant-authentication)
- [Auth0 Best Practices](https://auth0.com/docs/secure/security-guidance/best-practices)

---

**Last Updated**: 2025-10-18
**Issue**: #3
**Related Issues**: #5 (Backend Middleware), #6 (Frontend Integration)
