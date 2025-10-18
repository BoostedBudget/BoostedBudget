import { Page, expect } from '@playwright/test';

/**
 * Auth0 Helper Functions for E2E Tests
 * These helpers handle Auth0 Universal Login flows
 */

export interface Auth0Credentials {
  email: string;
  password: string;
}

/**
 * Perform login via Auth0 Universal Login
 * @param page - Playwright page object
 * @param credentials - Auth0 test user credentials
 */
export async function loginWithAuth0(
  page: Page,
  credentials: Auth0Credentials
): Promise<void> {
  // Click login button to redirect to Auth0
  await page.click('button:has-text("Log In")');

  // Wait for Auth0 Universal Login page
  await page.waitForURL(/auth0\.com/, { timeout: 10000 });

  // Fill in email
  await page.fill('input[name="username"], input[type="email"]', credentials.email);

  // Fill in password
  await page.fill('input[name="password"], input[type="password"]', credentials.password);

  // Click submit/continue button
  await page.click('button[type="submit"], button:has-text("Continue")');

  // Wait for redirect back to app
  await page.waitForURL(/localhost:3000/, { timeout: 15000 });

  // Wait for authentication to complete
  await page.waitForTimeout(2000);
}

/**
 * Perform signup via Auth0 Universal Login
 * @param page - Playwright page object
 * @param credentials - New user credentials
 */
export async function signupWithAuth0(
  page: Page,
  credentials: Auth0Credentials
): Promise<void> {
  // Click signup/register button if different from login
  // For Auth0, often you click login then switch to signup
  await page.click('button:has-text("Log In")');

  // Wait for Auth0 Universal Login page
  await page.waitForURL(/auth0\.com/, { timeout: 10000 });

  // Click "Sign up" tab/link if present
  const signupLink = page.locator('a:has-text("Sign up"), button:has-text("Sign up")');
  if (await signupLink.isVisible({ timeout: 2000 })) {
    await signupLink.click();
  }

  // Fill in email
  await page.fill('input[name="email"], input[type="email"]', credentials.email);

  // Fill in password
  await page.fill('input[name="password"], input[type="password"]', credentials.password);

  // Click submit/continue button
  await page.click('button[type="submit"], button:has-text("Continue"), button:has-text("Sign Up")');

  // Wait for redirect back to app
  await page.waitForURL(/localhost:3000/, { timeout: 15000 });

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
      await page.waitForTimeout(500);
    }
  } catch {
    // Cookie banner not present or already accepted
  }
}
