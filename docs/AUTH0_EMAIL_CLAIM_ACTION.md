# Auth0 Action: Add Email to Access Token

## Problem
Backend auto-registration requires email claim in JWT access token, but Auth0 doesn't include it by default.

Error: `Missing email in token claims for google-oauth2|106012025663349959159`

## Solution
Create an Auth0 Action to add email claim to access tokens.

## Steps

### 1. Navigate to Auth0 Dashboard
- Go to https://manage.auth0.com/
- Select your tenant: `dev-evuy37ow1xcidsmw.us.auth0.com`

### 2. Create Custom Action
1. Click **Actions** → **Library** → **Build Custom**
2. Name: `Add Email to Access Token`
3. Trigger: `Login / Post Login`
4. Runtime: `Node 18`

### 3. Action Code

```javascript
/**
 * Handler that will be called during the execution of a PostLogin flow.
 *
 * @param {Event} event - Details about the user and the context in which they are logging in.
 * @param {PostLoginAPI} api - Interface whose methods can be used to change the behavior of the login.
 */
exports.onExecutePostLogin = async (event, api) => {
  // Add email to access token custom claims
  // Namespace is required for custom claims (use your API identifier as namespace)
  const namespace = 'https://api.boostedbudget.com';

  if (event.user.email) {
    api.accessToken.setCustomClaim(`${namespace}/email`, event.user.email);
    api.accessToken.setCustomClaim(`email`, event.user.email);
  }

  if (event.user.email_verified !== undefined) {
    api.accessToken.setCustomClaim(`${namespace}/email_verified`, event.user.email_verified);
    api.accessToken.setCustomClaim(`email_verified`, event.user.email_verified);
  }
};
```

### 4. Deploy Action
1. Click **Deploy** (top right)
2. Go to **Actions** → **Flows** → **Login**
3. Click **Custom** tab (right side)
4. Drag **Add Email to Access Token** action into the flow (between Start and Complete)
5. Click **Apply**

### 5. Test
1. Log out of BoostedBudget staging
2. Log back in
3. Check backend logs - should see:
   - `New user detected, auto-registering: google-oauth2|...`
   - `Successfully registered new user: google-oauth2|...`
4. Profile/accounts/transactions pages should load successfully

## Alternative: Use ID Token Email (Code Change)

If you can't modify Auth0 Actions, update middleware to get email from ID token instead:

```go
// Get email from request context (set by Auth0 SDK from ID token)
// This requires frontend to pass ID token to backend
```

## Verification

After applying the Action, decode a JWT token to verify email is present:

```bash
# Get token from browser dev tools (Network tab, any API request, Authorization header)
# Decode at https://jwt.io

# Should see in payload:
{
  "email": "user@example.com",
  "email_verified": true,
  "https://api.boostedbudget.com/email": "user@example.com",
  "https://api.boostedbudget.com/email_verified": true
}
```

## References
- [Auth0 Actions Documentation](https://auth0.com/docs/customize/actions)
- [Custom Claims](https://auth0.com/docs/secure/tokens/json-web-tokens/create-custom-claims)
