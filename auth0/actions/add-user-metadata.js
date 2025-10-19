/**
 * Auth0 Action: Add User Metadata
 *
 * Trigger: Login / Post Login
 * Runtime: Node 18
 *
 * This Action adds custom claims to JWT tokens including:
 * - Subscription tier (free, premium, premium_plus)
 * - Primary region (iad for MVP)
 * - GDPR consent tracking
 * - User ID
 *
 * Installation:
 * 1. Go to Auth0 Dashboard → Actions → Library
 * 2. Click "+ Build Custom"
 * 3. Name: "Add User Metadata"
 * 4. Trigger: "Login / Post Login"
 * 5. Copy this code
 * 6. Click "Deploy"
 * 7. Go to Actions → Flows → Login
 * 8. Drag "Add User Metadata" to the flow
 * 9. Click "Apply"
 *
 * Testing:
 * - Log in to your application
 * - Decode the access_token JWT
 * - Verify custom claims under "https://boostedbudget.com/" namespace
 *
 * @param {Event} event - Details about the user and the context in which they are logging in.
 * @param {PostLoginAPI} api - Interface whose methods can be used to change the behavior of the login.
 */
exports.onExecutePostLogin = async (event, api) => {
  // Custom claims namespace (must be a URL)
  const namespace = 'https://boostedbudget.com';

  // Get user metadata (user-editable data like preferences)
  const userMetadata = event.user.user_metadata || {};

  // Get app metadata (admin-managed data like subscription tier)
  const appMetadata = event.user.app_metadata || {};

  // ============================================
  // Subscription Tier
  // ============================================
  // Default to 'free' if not set
  // Possible values: 'free', 'premium', 'premium_plus'
  const subscriptionTier = appMetadata.subscription_tier || 'free';

  // ============================================
  // Primary Region (GDPR Data Residency)
  // ============================================
  // Default to 'iad' (MVP single region)
  // Future: 'eu-west-1', 'ap-southeast-1', etc.
  const primaryRegion = userMetadata.primary_region || 'iad';

  // ============================================
  // GDPR Consent Tracking
  // ============================================
  const gdprConsent = {
    // Necessary cookies (required for app to function)
    necessary: userMetadata.gdpr_necessary_consent !== undefined
      ? userMetadata.gdpr_necessary_consent
      : true,

    // Analytics cookies (Google Analytics, etc.)
    analytics: userMetadata.gdpr_analytics_consent || false,

    // Advertising cookies (Google AdSense for free tier)
    advertising: userMetadata.gdpr_advertising_consent || false,

    // Timestamp when consent was last updated
    timestamp: userMetadata.gdpr_consent_timestamp || new Date().toISOString()
  };

  // ============================================
  // Add Custom Claims to Access Token
  // ============================================
  // These claims will be available in the backend JWT validation
  api.accessToken.setCustomClaim(`${namespace}/subscription_tier`, subscriptionTier);
  api.accessToken.setCustomClaim(`${namespace}/primary_region`, primaryRegion);
  api.accessToken.setCustomClaim(`${namespace}/gdpr_consent`, gdprConsent);
  api.accessToken.setCustomClaim(`${namespace}/user_id`, event.user.user_id);

  // ============================================
  // Add Custom Claims to ID Token
  // ============================================
  // These claims will be available in the frontend
  api.idToken.setCustomClaim(`${namespace}/email`, event.user.email);
  api.idToken.setCustomClaim(`${namespace}/subscription_tier`, subscriptionTier);
  api.idToken.setCustomClaim(`${namespace}/primary_region`, primaryRegion);

  // ============================================
  // Debug Logging (remove in production)
  // ============================================
  // console.log('User logged in:', event.user.user_id);
  // console.log('Subscription tier:', subscriptionTier);
  // console.log('Primary region:', primaryRegion);
};

/**
 * Expected JWT Payload:
 *
 * {
 *   "iss": "https://boostedbudget.auth0.com/",
 *   "sub": "auth0|123456789",
 *   "aud": "https://api.boostedbudget.com",
 *   "iat": 1234567890,
 *   "exp": 1234654290,
 *   "scope": "read:profile write:profile",
 *   "https://boostedbudget.com/subscription_tier": "free",
 *   "https://boostedbudget.com/primary_region": "iad",
 *   "https://boostedbudget.com/gdpr_consent": {
 *     "necessary": true,
 *     "analytics": false,
 *     "advertising": false,
 *     "timestamp": "2025-10-18T12:00:00.000Z"
 *   },
 *   "https://boostedbudget.com/user_id": "auth0|123456789"
 * }
 */

/**
 * Setting User Metadata via Management API:
 *
 * // Update user's subscription tier (app_metadata - admin only)
 * PATCH https://boostedbudget.auth0.com/api/v2/users/{user_id}
 * {
 *   "app_metadata": {
 *     "subscription_tier": "premium"
 *   }
 * }
 *
 * // Update user's GDPR consent (user_metadata - user can update)
 * PATCH https://boostedbudget.auth0.com/api/v2/users/{user_id}
 * {
 *   "user_metadata": {
 *     "gdpr_advertising_consent": true,
 *     "gdpr_consent_timestamp": "2025-10-18T12:00:00.000Z"
 *   }
 * }
 */
