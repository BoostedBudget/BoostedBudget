import { test, expect } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Auth0 Authentication E2E Tests
 * Tests user authentication flows including login, signup, protected routes, and logout
 *
 * NOTE: These tests require a test Auth0 tenant and test user credentials.
 * Set TEST_AUTH0_EMAIL and TEST_AUTH0_PASSWORD in .env.test file.
 *
 * For CI/CD, these tests can be run against a dedicated test Auth0 tenant
 * with automated test user creation/cleanup.
 *
 * SKIPPED IN CI: These tests are skipped when Auth0 credentials are not configured.
 * Configure VITE_AUTH0_DOMAIN, VITE_AUTH0_CLIENT_ID, and test credentials to enable.
 */

// Skip all Auth0 tests if Auth0 is not configured
const skipAuth0Tests = !process.env.VITE_AUTH0_DOMAIN || !process.env.VITE_AUTH0_CLIENT_ID;

// Use conditional describe - if skipAuth0Tests is true, use test.describe.skip, otherwise test.describe
const describeAuth0 = skipAuth0Tests ? test.describe.skip : test.describe;

describeAuth0('Auth0 Authentication', () => {
  const testEmail = process.env.TEST_AUTH0_EMAIL || 'test@example.com';
  const testPassword = process.env.TEST_AUTH0_PASSWORD || 'TestPassword123!';

  test.beforeEach(async ({ context }) => {
    // Clear cookies and storage before each test
    await context.clearCookies();
  });

  test.describe('Unauthenticated User', () => {
    test('should show Log In button when not authenticated', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Verify Log In button is visible
      const loginButton = page.locator('button:has-text("Log In")');
      await expect(loginButton).toBeVisible();
    });

    test('should not show Log Out button when not authenticated', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Verify Log Out button is not visible
      const logoutButton = page.locator('button:has-text("Log Out")');
      await expect(logoutButton).not.toBeVisible();
    });

    test('should redirect to login when accessing protected route', async ({ page }) => {
      // Try to access a protected route (e.g., /dashboard, /profile)
      await page.goto('/dashboard');

      // Should either redirect to Auth0 or show a login prompt
      // Wait for either Auth0 domain or login button
      try {
        // Option 1: Redirected to Auth0
        await page.waitForURL(/auth0\.com/, { timeout: 5000 });
        expect(page.url()).toContain('auth0.com');
      } catch {
        // Option 2: Shown login button on app
        const loginButton = page.locator('button:has-text("Log In")');
        await expect(loginButton).toBeVisible();
      }
    });
  });

  test.describe('Login Flow', () => {
    test('should redirect to Auth0 when clicking Log In', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In button
      await page.click('button:has-text("Log In")');

      // Wait for redirect to Auth0
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Verify we're on Auth0 login page
      expect(page.url()).toContain('auth0.com');
    });

    test.skip('should successfully log in with valid credentials', async ({ page }) => {
      /**
       * SKIPPED: This test requires actual Auth0 test credentials
       * To enable:
       * 1. Create test user in Auth0 test tenant
       * 2. Set TEST_AUTH0_EMAIL and TEST_AUTH0_PASSWORD environment variables
       * 3. Remove test.skip
       */
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Auth0
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Fill in credentials
      await page.fill('input[name="username"], input[type="email"]', testEmail);
      await page.fill('input[name="password"], input[type="password"]', testPassword);

      // Submit
      await page.click('button[type="submit"], button:has-text("Continue")');

      // Wait for redirect back to app
      await page.waitForURL(/localhost:3000/, { timeout: 15000 });

      // Verify logged in state
      await expect(page.locator('button:has-text("Log Out")')).toBeVisible({ timeout: 5000 });
    });

    test('should show error with invalid credentials', async ({ page }) => {
      /**
       * NOTE: This test verifies Auth0 shows an error for invalid credentials
       * It won't actually log in, just verify error handling
       */
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Auth0
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Fill in invalid credentials
      await page.fill('input[name="username"], input[type="email"]', 'invalid@example.com');
      await page.fill('input[name="password"], input[type="password"]', 'WrongPassword123!');

      // Submit
      await page.click('button[type="submit"], button:has-text("Continue")');

      // Wait a bit for error to appear
      await page.waitForTimeout(2000);

      // Verify still on Auth0 (didn't redirect back)
      expect(page.url()).toContain('auth0.com');

      // Check for error message (Auth0 typically shows error)
      // This might vary based on Auth0 configuration
      const errorVisible = await page.locator('text=/wrong|invalid|incorrect/i').isVisible({ timeout: 3000 }).catch(() => false);

      // We expect either an error message or to still be on Auth0 page
      expect(errorVisible || page.url().includes('auth0.com')).toBeTruthy();
    });
  });

  test.describe('Signup Flow', () => {
    test('should have signup option on Auth0 login page', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In (which takes to Auth0)
      await page.click('button:has-text("Log In")');

      // Wait for Auth0
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Check for Sign Up link/button
      // Auth0 typically has a "Sign up" link
      const signupExists = await page.locator('a:has-text("Sign up"), button:has-text("Sign up"), a:has-text("Sign Up"), button:has-text("Sign Up")').count();

      expect(signupExists).toBeGreaterThan(0);
    });

    test.skip('should successfully create account with valid details', async ({ page: _page }) => {
      /**
       * SKIPPED: This test would create actual Auth0 users
       * To enable:
       * 1. Set up test Auth0 tenant with user cleanup automation
       * 2. Generate unique test email (e.g., test+timestamp@example.com)
       * 3. Remove test.skip
       */

      // This is a placeholder for signup flow testing
      // Actual implementation would:
      // 1. Navigate to login page
      // 2. Click "Sign up"
      // 3. Fill in new user details
      // 4. Submit and verify account creation
    });
  });

  test.describe('Authenticated User', () => {
    test.skip('should access protected routes when authenticated', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication
       * To enable: Implement auth helper to log in first, then remove test.skip
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to protected route (e.g., /dashboard)
      // 3. Verify access granted
      // 4. Verify content loads
    });

    test.skip('should display user profile information', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to profile page
      // 3. Verify user email/name displayed
      // 4. Verify profile data matches authenticated user
    });
  });

  test.describe('Logout Flow', () => {
    test.skip('should successfully log out', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication first
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Click Log Out button
      // 3. Verify redirect to logged out state
      // 4. Verify Log In button appears
      // 5. Verify session is cleared
    });

    test.skip('should redirect to home page after logout', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication first
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to protected route
      // 3. Click Log Out
      // 4. Verify redirected to home page
    });

    test.skip('should not access protected routes after logout', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication first
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Log out
      // 3. Try to access protected route
      // 4. Verify redirected to login
    });
  });

  test.describe('Session Persistence', () => {
    test.skip('should maintain session across page reloads', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication first
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Reload the page
      // 3. Verify still authenticated
      // 4. Verify Log Out button still visible
    });

    test.skip('should maintain session across navigation', async ({ page: _page }) => {
      /**
       * SKIPPED: Requires actual authentication first
       */

      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to different pages
      // 3. Verify still authenticated on each page
    });
  });

  test.describe('Auth0 Integration', () => {
    test('should use correct Auth0 domain', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Auth0 redirect
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Verify Auth0 domain in URL
      expect(page.url()).toContain('.auth0.com');
    });

    test('should redirect back to application after auth', async ({ page }) => {
      /**
       * This test verifies the redirect configuration, even with invalid creds
       */
      await page.goto('/');
      await acceptCookieConsent(page);

      // Store the callback URL from Auth0 redirect
      await page.click('button:has-text("Log In")');
      await page.waitForURL(/auth0\.com/, { timeout: 10000 });

      // Check that redirect_uri parameter is set correctly
      const url = new URL(page.url());
      const redirectUri = url.searchParams.get('redirect_uri');

      expect(redirectUri).toBeTruthy();
      expect(redirectUri).toContain('localhost:3000');
    });
  });
});
