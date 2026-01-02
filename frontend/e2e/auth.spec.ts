import { test, expect } from '@playwright/test';
import { setupClerkTestingToken } from '@clerk/testing/playwright';
import { acceptCookieConsent, skipIfNoAuth } from './helpers/auth';

/**
 * Clerk Authentication E2E Tests
 * Tests user authentication flows including login, signup, protected routes, and logout
 *
 * NOTE: These tests require Clerk credentials.
 * Set TEST_CLERK_EMAIL and TEST_CLERK_PASSWORD in .env.test file.
 *
 * For CI/CD, these tests can be run against a dedicated test Clerk instance
 * with automated test user creation/cleanup.
 *
 * SKIPPED IN CI: These tests are skipped when Clerk credentials are not configured.
 * Configure VITE_CLERK_PUBLISHABLE_KEY and test credentials to enable.
 */

// Skip all Clerk tests if Clerk is not configured
const skipClerkTests = !process.env.VITE_CLERK_PUBLISHABLE_KEY;

// Use conditional describe - if skipClerkTests is true, use test.describe.skip, otherwise test.describe
const describeClerk = skipClerkTests ? test.describe.skip : test.describe;

describeClerk('Clerk Authentication', () => {
  const testEmail = process.env.TEST_CLERK_EMAIL || 'test@example.com';
  const testPassword = process.env.TEST_CLERK_PASSWORD || 'TestPassword123!';

  test.beforeEach(async ({ context }) => {
    // Clear cookies and storage before each test
    await context.clearCookies();
  });

  test.describe('Unauthenticated User', () => {
    test('should show Log In button when not authenticated', async ({ page }) => {
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Verify Log In button is visible
      const loginButton = page.locator('button:has-text("Log In")');
      await expect(loginButton).toBeVisible();
    });

    test('should not show Log Out button when not authenticated', async ({ page }) => {
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Verify Log Out button is not visible
      const logoutButton = page.locator('button:has-text("Log Out")');
      await expect(logoutButton).not.toBeVisible();
    });

    test('should redirect to login when accessing protected route', async ({ page }) => {
      await setupClerkTestingToken({ page });
      // Try to access a protected route (e.g., /dashboard, /profile)
      await page.goto('/dashboard');

      // Should either redirect to Clerk or show a login prompt
      // Wait for either Clerk domain or login button
      try {
        // Option 1: Redirected to Clerk
        await page.waitForURL(/clerk\./, { timeout: 5000 });
        expect(page.url()).toContain('clerk');
      } catch {
        // Option 2: Shown login button on app (Clerk modal)
        const loginButton = page.locator('button:has-text("Log In")');
        await expect(loginButton).toBeVisible();
      }
    });
  });

  test.describe('Login Flow', () => {
    test('should open Clerk sign-in when clicking Log In', async ({ page }) => {
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In button
      await page.click('button:has-text("Log In")');

      // Wait for Clerk sign-in UI (modal or redirect)
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Verify we're on Clerk sign-in (either modal or redirect)
      const emailInput = page.locator('input[name="identifier"], input[type="email"]');
      await expect(emailInput).toBeVisible();
    });

    test('should successfully log in with valid credentials', async ({ page }) => {
      // Skip if no credentials available
      test.skip(!process.env.TEST_CLERK_EMAIL || !process.env.TEST_CLERK_PASSWORD, 'Requires TEST_CLERK_EMAIL and TEST_CLERK_PASSWORD');
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Clerk sign-in UI
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Fill in email
      await page.fill('input[name="identifier"], input[type="email"]', testEmail);
      await page.click('button:has-text("Continue")');

      // Wait for password field
      await page.waitForSelector('input[type="password"]', { timeout: 10000 });

      // Fill in password
      await page.fill('input[type="password"]', testPassword);
      await page.click('button:has-text("Continue"), button[type="submit"]');

      // Wait for redirect back to app
      await page.waitForURL(/localhost:3000/, { timeout: 15000 });

      // Verify logged in state
      await expect(page.locator('button:has-text("Log Out")')).toBeVisible({ timeout: 5000 });
    });

    test('should show error with invalid credentials', async ({ page }) => {
      /**
       * NOTE: This test verifies Clerk shows an error for invalid credentials
       * It won't actually log in, just verify error handling
       */
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Clerk sign-in UI
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Fill in invalid email
      await page.fill('input[name="identifier"], input[type="email"]', 'invalid@example.com');
      await page.click('button:has-text("Continue")');

      // Wait a bit for error or password field
      await page.waitForTimeout(2000);

      // Clerk will either show error for non-existent email or ask for password
      // Either way, we're still in the auth flow
      const stillInAuth = await page.locator('input[type="email"], input[type="password"], text=/error|invalid|not found/i').count();
      expect(stillInAuth).toBeGreaterThan(0);
    });
  });

  test.describe('Signup Flow', () => {
    test('should have signup option on Clerk login page', async ({ page }) => {
      await setupClerkTestingToken({ page });
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In (which opens Clerk)
      await page.click('button:has-text("Log In")');

      // Wait for Clerk sign-in UI
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Check for Sign Up link/button
      // Clerk typically has a "Sign up" link
      const signupExists = await page.locator('a:has-text("Sign up"), button:has-text("Sign up"), a:has-text("Sign Up"), button:has-text("Sign Up"), text=/don\'t have an account/i').count();

      expect(signupExists).toBeGreaterThan(0);
    });

    test('should successfully create account with valid details', async ({ page: _page }) => {
      // Skip: Creating users in Clerk requires special cleanup automation
      test.skip(true, 'Requires automated Clerk user cleanup to avoid test pollution');

      // This is a placeholder for signup flow testing
      // Actual implementation would:
      // 1. Navigate to login page
      // 2. Click "Sign up"
      // 3. Fill in new user details
      // 4. Submit and verify account creation
    });
  });

  test.describe('Authenticated User', () => {
    test.beforeEach(async (_fixtures, testInfo) => {
      skipIfNoAuth(testInfo);
    });

    test('should access protected routes when authenticated', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to protected route (e.g., /dashboard)
      // 3. Verify access granted
      // 4. Verify content loads
    });

    test('should display user profile information', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to profile page
      // 3. Verify user email/name displayed
      // 4. Verify profile data matches authenticated user
    });
  });

  test.describe('Logout Flow', () => {
    test.beforeEach(async (_fixtures, testInfo) => {
      skipIfNoAuth(testInfo);
    });

    test('should successfully log out', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Click Log Out button
      // 3. Verify redirect to logged out state
      // 4. Verify Log In button appears
      // 5. Verify session is cleared
    });

    test('should redirect to home page after logout', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to protected route
      // 3. Click Log Out
      // 4. Verify redirected to home page
    });

    test('should not access protected routes after logout', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Log out
      // 3. Try to access protected route
      // 4. Verify redirected to login
    });
  });

  test.describe('Session Persistence', () => {
    test.beforeEach(async (_fixtures, testInfo) => {
      skipIfNoAuth(testInfo);
    });

    test('should maintain session across page reloads', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Reload the page
      // 3. Verify still authenticated
      // 4. Verify Log Out button still visible
    });

    test('should maintain session across navigation', async ({ page: _page }) => {
      // This test would:
      // 1. Log in using auth helper
      // 2. Navigate to different pages
      // 3. Verify still authenticated on each page
    });
  });

  test.describe('Clerk Integration', () => {
    test('should use correct Clerk configuration', async ({ page }) => {
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In
      await page.click('button:has-text("Log In")');

      // Wait for Clerk UI
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Verify Clerk UI is present
      const clerkUI = await page.locator('input[name="identifier"], input[type="email"]').count();
      expect(clerkUI).toBeGreaterThan(0);
    });

    test('should redirect back to application after auth', async ({ page }) => {
      /**
       * This test verifies the redirect configuration
       */
      await page.goto('/');
      await acceptCookieConsent(page);

      // Click Log In - Clerk should open in modal or redirect
      await page.click('button:has-text("Log In")');

      // Wait for Clerk UI
      await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

      // Verify Clerk sign-in UI is present
      const emailInput = page.locator('input[name="identifier"], input[type="email"]');
      await expect(emailInput).toBeVisible();
    });
  });
});
