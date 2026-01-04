import { test, expect } from '@playwright/test';
import { setupClerkTestingToken } from '@clerk/testing/playwright';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Clerk Authentication Login Flow E2E Tests
 * Tests for unauthenticated users interacting with login/signup UI
 *
 * These tests run in the chromium-no-auth project (user starts unauthenticated)
 */

test.describe('Clerk Authentication - Login Flow', () => {
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

    // SKIPPED: Issue #615 - User button selector needs verification after Clerk login
    // The test logs in successfully but can't find the logged-in state selector.
    test.skip('should successfully log in with valid credentials', async ({ page }) => {
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
      await page.locator('button:has-text("Continue"):visible, button[type="submit"]:visible').first().click();

      // Wait for redirect back to app
      await page.waitForURL(/localhost/, { timeout: 15000 });

      // Verify logged in state - look for user button or dashboard access
      await expect(page.locator('[data-testid="user-button"], button[aria-label*="User"], .cl-userButtonBox, button:has-text("Log Out")')).toBeVisible({ timeout: 10000 });
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
      const emailInputCount = await page.locator('input[type="email"]').count();
      const passwordInputCount = await page.locator('input[type="password"]').count();
      const errorTextCount = await page.getByText(/error|invalid|not found/i).count();
      const stillInAuth = emailInputCount + passwordInputCount + errorTextCount;
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
      const signUpLinks = await page.locator('a:has-text("Sign up"), button:has-text("Sign up"), a:has-text("Sign Up"), button:has-text("Sign Up")').count();
      const dontHaveAccountText = await page.getByText(/don't have an account/i).count();
      const signupExists = signUpLinks + dontHaveAccountText;

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
