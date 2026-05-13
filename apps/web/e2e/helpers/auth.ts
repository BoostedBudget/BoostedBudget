import { Page, expect, test, TestInfo } from '@playwright/test';

/**
 * Clerk Helper Functions for E2E Tests
 * These helpers handle Clerk authentication flows
 */

export interface ClerkCredentials {
  email: string;
  password: string;
}

/**
 * Perform login via Clerk
 * @param page - Playwright page object
 * @param credentials - Clerk test user credentials
 */
export async function loginWithClerk(
  page: Page,
  credentials: ClerkCredentials
): Promise<void> {
  // Click login button to open Clerk sign-in modal or redirect
  await page.click('button:has-text("Log In")');

  // Wait for Clerk sign-in UI (could be modal or redirect)
  // Clerk hosted pages are at accounts.*.clerk.accounts.dev or clerk.* domains
  await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

  // Fill in email
  await page.fill('input[name="identifier"], input[type="email"]', credentials.email);

  // Click continue button
  await page.click('button:has-text("Continue")');

  // Wait for password field
  await page.waitForSelector('input[type="password"]', { timeout: 10000 });

  // Fill in password
  await page.fill('input[type="password"]', credentials.password);

  // Click submit button
  await page.click('button:has-text("Continue"), button[type="submit"]');

  // Wait for redirect back to app (use dynamic port from env, fallback to 3000)
  await page.waitForURL(new RegExp(`localhost:${process.env.E2E_FRONTEND_PORT || '3000'}`), { timeout: 15000 });

  // Wait for authentication to complete
  await page.waitForTimeout(2000);
}

/**
 * Perform signup via Clerk
 * @param page - Playwright page object
 * @param credentials - New user credentials
 */
export async function signupWithClerk(
  page: Page,
  credentials: ClerkCredentials
): Promise<void> {
  // Click login button to open Clerk UI
  await page.click('button:has-text("Log In")');

  // Wait for Clerk sign-in UI
  await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 10000 });

  // Look for "Sign up" or "Don't have an account" link
  const signupLink = page.locator('a:has-text("Sign up"), button:has-text("Sign up"), a:has-text("Don\'t have an account")');
  if (await signupLink.isVisible({ timeout: 2000 })) {
    await signupLink.click();
  }

  // Fill in email
  await page.fill('input[name="emailAddress"], input[type="email"]', credentials.email);

  // Fill in password
  await page.fill('input[name="password"], input[type="password"]', credentials.password);

  // Click submit button
  await page.click('button[type="submit"], button:has-text("Continue"), button:has-text("Sign up")');

  // Wait for redirect back to app (use dynamic port from env, fallback to 3000)
  await page.waitForURL(new RegExp(`localhost:${process.env.E2E_FRONTEND_PORT || '3000'}`), { timeout: 15000 });

  // Wait for authentication to complete
  await page.waitForTimeout(2000);
}

/**
 * Perform logout
 * @param page - Playwright page object
 */
export async function logout(page: Page): Promise<void> {
  // Click logout button
  await page.click('button:has-text("Log Out")');

  // Wait for logout to complete and redirect
  await page.waitForTimeout(1000);

  // Verify logged out state
  await expect(page.locator('button:has-text("Log In")')).toBeVisible();
}

/**
 * Check if user is authenticated
 * @param page - Playwright page object
 */
export async function isAuthenticated(page: Page): Promise<boolean> {
  try {
    // Check for logout button (indicates user is logged in)
    return await page.locator('button:has-text("Log Out")').isVisible({ timeout: 2000 });
  } catch {
    return false;
  }
}

/**
 * Accept cookie consent banner (GDPR)
 * @param page - Playwright page object
 */
export async function acceptCookieConsent(page: Page): Promise<void> {
  try {
    const acceptButton = page.locator('button:has-text("Accept"), button:has-text("Accept All")');
    if (await acceptButton.isVisible({ timeout: 2000 })) {
      await acceptButton.click();
      await page.locator('[aria-label="Cookie Consent"]').waitFor({ state: 'hidden', timeout: 5_000 });
    }
  } catch {
    // Cookie banner not present or already accepted
  }
}

/**
 * Skip test if running in an unauthenticated project
 * Use this in beforeEach for tests that require authentication
 *
 * @param testInfo - Test info object from Playwright
 * @example
 * test.beforeEach(async ({ page }, testInfo) => {
 *   skipIfNoAuth(testInfo);
 *   await setupPage(page);
 * });
 */
export function skipIfNoAuth(testInfo: TestInfo): void {
  // Skip if running in chromium-no-auth project (these tests require auth)
  if (testInfo.project.name === 'chromium-no-auth') {
    test.skip(true, 'Test requires authentication - skipping in chromium-no-auth project');
  }
}

