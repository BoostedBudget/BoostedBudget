# Auth0 Test Tenant Setup Guide

Complete guide for creating an Auth0 test tenant and configuring test credentials for E2E testing with Playwright.

## Table of Contents
- [Overview](#overview)
- [Creating Auth0 Test Tenant](#creating-auth0-test-tenant)
- [Creating Test Applications](#creating-test-applications)
- [Creating Test User](#creating-test-user)
- [Configuring Environment Variables](#configuring-environment-variables)
- [Testing the Configuration](#testing-the-configuration)
- [CI/CD Integration](#cicd-integration)
- [Security Best Practices](#security-best-practices)

## Overview

For E2E testing, you need a separate Auth0 test tenant to avoid polluting production data and to safely test authentication flows with automated credentials.

**Why a separate test tenant?**
- Isolated test users and data
- Safe to reset/delete test users
- Different rate limits won't affect production
- Can use test-specific configurations
- CI/CD can safely run automated tests

## Creating Auth0 Test Tenant

### 1. Sign Up or Log In to Auth0
Visit: https://auth0.com/signup

If you already have an Auth0 account, log in to the [Auth0 Dashboard](https://manage.auth0.com).

### 2. Create New Tenant

1. Click on your tenant name (top left)
2. Click **"+ Create Tenant"**
3. Fill in tenant details:
   - **Tenant Domain**: `boostedbudget-test` (or similar)
     - Full domain will be: `boostedbudget-test.us.auth0.com`
   - **Region**: `US` (match your production region)
   - **Environment Tag**: `Development` or `Testing`

4. Click **"Create"**

### 3. Configure Tenant Settings

Navigate to **Settings** > **General**:

- **Friendly Name**: BoostedBudget Test
- **Logo URL**: (optional, same as production)
- **Support Email**: your-email@example.com
- **Support URL**: https://boostedbudget.com/support

**Session Settings**:
- **Session Timeout**: 720 (minutes) - default
- **Inactivity Timeout**: 72 (hours) - default

Click **Save Changes**

## Creating Test Applications

You need to create two applications in your test tenant:

### Application 1: Frontend SPA (Single Page Application)

1. Navigate to **Applications** > **Applications**
2. Click **"+ Create Application"**
3. Configure:
   - **Name**: `BoostedBudget Frontend (Test)`
   - **Application Type**: `Single Page Application`
4. Click **Create**

#### Configure Frontend SPA Settings

**Settings Tab**:

- **Allowed Callback URLs**:
  ```
  http://localhost:3000/callback,
  http://localhost:3000
  ```

- **Allowed Logout URLs**:
  ```
  http://localhost:3000
  ```

- **Allowed Web Origins**:
  ```
  http://localhost:3000
  ```

- **Allowed Origins (CORS)**:
  ```
  http://localhost:3000
  ```

**Advanced Settings** > **Grant Types**:
- ✅ Implicit
- ✅ Authorization Code
- ✅ Refresh Token

Click **Save Changes**

**Copy Credentials**:
- **Domain**: `boostedbudget-test.us.auth0.com`
- **Client ID**: (copy this for frontend .env)

### Application 2: Backend API (M2M Application)

1. Navigate to **Applications** > **APIs**
2. Click **"+ Create API"**
3. Configure:
   - **Name**: `BoostedBudget API (Test)`
   - **Identifier**: `https://api.boostedbudget.com`
   - **Signing Algorithm**: `RS256`
4. Click **Create**

**Note**: The identifier should match your production API identifier for consistency.

#### Configure API Settings

**Settings Tab**:
- **Enable RBAC**: ✅ (if using role-based access control)
- **Add Permissions in the Access Token**: ✅
- **Allow Skipping User Consent**: ✅ (for testing)
- **Allow Offline Access**: ✅ (for refresh tokens)

**Permissions** (optional for Phase 0):
- `read:profile`
- `write:profile`
- `read:accounts`
- `write:accounts`
- `read:rules`
- `write:rules`

Click **Save**

**Copy Credentials**:
- **Identifier (Audience)**: `https://api.boostedbudget.com`

## Creating Test User

### Option A: Manual Test User Creation

1. Navigate to **User Management** > **Users**
2. Click **"+ Create User"**
3. Fill in details:
   - **Email**: `test@boostedbudget-test.com`
   - **Password**: `TestPassword123!`
     - Must meet Auth0 password requirements:
       - At least 8 characters
       - At least 1 lowercase letter
       - At least 1 uppercase letter
       - At least 1 number
       - At least 1 special character
   - **Connection**: `Username-Password-Authentication`
   - **Email Verified**: ✅ (check this box)

4. Click **Create**

### Option B: Automated Test User Creation (Recommended for CI/CD)

For CI/CD pipelines, you can create test users programmatically using the Auth0 Management API.

**Script Example** (save as `scripts/create-test-user.sh`):

```bash
#!/bin/bash

# Create test user via Auth0 Management API
# Requires: AUTH0_DOMAIN, AUTH0_CLIENT_ID, AUTH0_CLIENT_SECRET

# Get Management API token
TOKEN_RESPONSE=$(curl -s --request POST \
  --url "https://${AUTH0_DOMAIN}/oauth/token" \
  --header 'content-type: application/json' \
  --data "{
    \"client_id\":\"${AUTH0_MANAGEMENT_CLIENT_ID}\",
    \"client_secret\":\"${AUTH0_MANAGEMENT_CLIENT_SECRET}\",
    \"audience\":\"https://${AUTH0_DOMAIN}/api/v2/\",
    \"grant_type\":\"client_credentials\"
  }")

ACCESS_TOKEN=$(echo $TOKEN_RESPONSE | jq -r '.access_token')

# Create test user
curl -s --request POST \
  --url "https://${AUTH0_DOMAIN}/api/v2/users" \
  --header "authorization: Bearer ${ACCESS_TOKEN}" \
  --header 'content-type: application/json' \
  --data "{
    \"email\":\"test@boostedbudget-test.com\",
    \"email_verified\":true,
    \"password\":\"TestPassword123!\",
    \"connection\":\"Username-Password-Authentication\"
  }"

echo "Test user created: test@boostedbudget-test.com"
```

## Configuring Environment Variables

### 1. Backend Environment Variables

Update `backend/.env` or create `backend/.env.test`:

```env
# Auth0 Test Tenant Configuration
AUTH0_DOMAIN=boostedbudget-test.us.auth0.com
AUTH0_AUDIENCE=https://api.boostedbudget.com

# Backend M2M Application
AUTH0_CLIENT_ID=your_backend_m2m_client_id_here
AUTH0_CLIENT_SECRET=your_backend_m2m_client_secret_here

# Auth0 Management API (for user management)
AUTH0_MANAGEMENT_CLIENT_ID=your_management_client_id_here
AUTH0_MANAGEMENT_CLIENT_SECRET=your_management_client_secret_here
```

### 2. Frontend Environment Variables

Update `frontend/.env` or create `frontend/.env.test`:

```env
# Auth0 Test Tenant Configuration
VITE_AUTH0_DOMAIN=boostedbudget-test.us.auth0.com
VITE_AUTH0_CLIENT_ID=your_frontend_spa_client_id_here
VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com
VITE_AUTH0_REDIRECT_URI=http://localhost:3000/callback
```

### 3. E2E Test Environment Variables

Create `frontend/e2e/.env.test`:

```env
# Auth0 Test Credentials
TEST_AUTH0_EMAIL=test@boostedbudget-test.com
TEST_AUTH0_PASSWORD=TestPassword123!

# Auth0 Configuration (should match frontend .env)
VITE_AUTH0_DOMAIN=boostedbudget-test.us.auth0.com
VITE_AUTH0_CLIENT_ID=your_frontend_spa_client_id_here
VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com

# Test Environment URLs
PLAYWRIGHT_BASE_URL=http://localhost:3000
PLAYWRIGHT_API_URL=http://localhost:8080

# Optional: Headless mode
HEADLESS=true
```

**Important**: Add `frontend/e2e/.env.test` to `.gitignore` (already done)

## Testing the Configuration

### 1. Manual Login Test

1. Start backend server:
   ```bash
   cd backend
   ./bin/server
   ```

2. Start frontend dev server:
   ```bash
   cd frontend
   npm run dev
   ```

3. Open browser to http://localhost:3000
4. Click "Log In"
5. You should be redirected to Auth0 test tenant
6. Log in with test credentials:
   - Email: `test@boostedbudget-test.com`
   - Password: `TestPassword123!`
7. You should be redirected back to the app as logged in

### 2. E2E Test Verification

Enable skipped tests by removing `test.skip()` from `frontend/e2e/auth.spec.ts`:

```bash
cd frontend

# Run E2E tests with test credentials
npm run test:e2e

# Or run in headed mode to see the browser
npm run test:e2e:headed
```

**Expected Results**:
- All health tests pass ✅
- All cookie consent tests pass ✅
- All auth tests pass (including previously skipped ones) ✅

## CI/CD Integration

### GitHub Actions Setup

Store Auth0 test credentials as GitHub Secrets:

1. Navigate to your repository on GitHub
2. Go to **Settings** > **Secrets and variables** > **Actions**
3. Click **"New repository secret"**
4. Add the following secrets:

| Secret Name | Value | Description |
|-------------|-------|-------------|
| `TEST_AUTH0_DOMAIN` | `boostedbudget-test.us.auth0.com` | Test tenant domain |
| `TEST_AUTH0_CLIENT_ID` | `your_frontend_spa_client_id` | Frontend SPA client ID |
| `TEST_AUTH0_AUDIENCE` | `https://api.boostedbudget.com` | API identifier |
| `TEST_AUTH0_EMAIL` | `test@boostedbudget-test.com` | Test user email |
| `TEST_AUTH0_PASSWORD` | `TestPassword123!` | Test user password |

### GitHub Actions Workflow Example

Update `.github/workflows/ci.yml`:

```yaml
name: CI Pipeline

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

jobs:
  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Node.js
        uses: actions/setup-node@v3
        with:
          node-version: '18'
          cache: 'npm'
          cache-dependency-path: frontend/package-lock.json

      - name: Install dependencies
        run: |
          cd frontend
          npm ci

      - name: Install Playwright
        run: |
          cd frontend
          npx playwright install chromium

      - name: Create .env.test
        run: |
          cd frontend/e2e
          cat > .env.test << EOF
          TEST_AUTH0_EMAIL=${{ secrets.TEST_AUTH0_EMAIL }}
          TEST_AUTH0_PASSWORD=${{ secrets.TEST_AUTH0_PASSWORD }}
          VITE_AUTH0_DOMAIN=${{ secrets.TEST_AUTH0_DOMAIN }}
          VITE_AUTH0_CLIENT_ID=${{ secrets.TEST_AUTH0_CLIENT_ID }}
          VITE_AUTH0_AUDIENCE=${{ secrets.TEST_AUTH0_AUDIENCE }}
          PLAYWRIGHT_BASE_URL=http://localhost:3000
          PLAYWRIGHT_API_URL=http://localhost:8080
          HEADLESS=true
          EOF

      - name: Start backend server
        run: |
          cd backend
          ./bin/server &
          sleep 5

      - name: Run E2E tests
        run: |
          cd frontend
          npm run test:e2e

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v3
        with:
          name: playwright-report
          path: frontend/playwright-report/
```

## Security Best Practices

### 1. Tenant Isolation
- ✅ Use separate test tenant (never use production)
- ✅ Different domain name (e.g., `-test` suffix)
- ✅ Different user database

### 2. Credential Management
- ✅ Never commit `.env.test` to git
- ✅ Use GitHub Secrets for CI/CD
- ✅ Rotate test user password periodically
- ✅ Use strong passwords even for test accounts

### 3. Test User Hygiene
- Create dedicated test users (don't use personal emails)
- Mark test users with identifiable emails (e.g., `test@...`)
- Periodically clean up old test users
- Email verification: enabled for realistic testing

### 4. Rate Limiting
- Be aware of Auth0 rate limits (even in test)
- Don't run E2E tests too frequently in parallel
- Consider Auth0 plan limits for test tenant

### 5. Access Control
- Limit who has access to test tenant dashboard
- Use least privilege for Management API credentials
- Audit test tenant access logs periodically

## Troubleshooting

### Issue: "Invalid Callback URL"
**Solution**: Ensure `http://localhost:3000/callback` is in **Allowed Callback URLs** in your SPA application settings.

### Issue: "Access Denied" error during login
**Solution**:
1. Check that API audience matches in frontend config and Auth0 API settings
2. Verify API has correct permissions enabled
3. Ensure CORS origins include `http://localhost:3000`

### Issue: Tests fail with "Email not verified"
**Solution**: When creating test user, ensure "Email Verified" checkbox is ✅ checked.

### Issue: E2E tests timeout waiting for Auth0
**Solution**:
1. Check Auth0 tenant is accessible: `curl https://boostedbudget-test.us.auth0.com`
2. Verify network connectivity
3. Check rate limiting (try again in a few minutes)

### Issue: "Invalid credentials" in tests
**Solution**:
1. Double-check `.env.test` has correct email/password
2. Verify test user exists in Auth0 dashboard
3. Try logging in manually first to confirm credentials

## Maintenance

### Regular Tasks
- **Monthly**: Review and clean up test users
- **Quarterly**: Rotate test user passwords
- **Quarterly**: Review Auth0 tenant settings match production
- **As needed**: Update E2E tests when Auth0 UI changes

### Updating Test Credentials
1. Change password in Auth0 dashboard (User Management > Users)
2. Update `frontend/e2e/.env.test` locally
3. Update GitHub Secrets for CI/CD
4. Test locally before pushing

## Resources

- [Auth0 Testing Guide](https://auth0.com/docs/test)
- [Auth0 Management API](https://auth0.com/docs/api/management/v2)
- [Auth0 Rate Limits](https://auth0.com/docs/troubleshoot/customer-support/operational-policies/rate-limit-policy)
- [Playwright Auth Testing](https://playwright.dev/docs/auth)
- [BoostedBudget E2E Tests README](../frontend/e2e/README.md)

## Summary Checklist

- [ ] Created Auth0 test tenant
- [ ] Created Frontend SPA application
- [ ] Created Backend API
- [ ] Configured callback URLs and CORS
- [ ] Created test user with verified email
- [ ] Updated backend `.env` with test credentials
- [ ] Updated frontend `.env` with test credentials
- [ ] Created `frontend/e2e/.env.test` with test credentials
- [ ] Tested manual login works
- [ ] Enabled and ran E2E tests successfully
- [ ] Added GitHub Secrets for CI/CD
- [ ] Updated CI/CD workflow to use test credentials
- [ ] Documented any custom configurations

---

**Last Updated**: 2025-10-18
**Maintainer**: BoostedBudget Team
**Related Issues**: #12 (Phase 0 Integration Testing)
