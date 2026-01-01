import { test as setup, expect } from '@playwright/test';
import { setupClerkTestingToken } from '@clerk/testing/playwright';

const authFile = 'e2e/.auth/user.json';

/**
 * Authentication Setup for E2E Tests
 *
 * This setup test logs in once and saves the browser session state,
 * which is then reused by all authenticated tests. This is more efficient
 * than logging in for every test.
 *
 * Required environment variables:
 * - TEST_CLERK_EMAIL: Email of a Clerk test user
 * - TEST_CLERK_PASSWORD: Password for the test user
 * - CLERK_PUBLISHABLE_KEY: Clerk publishable key (for testing token)
 * - CLERK_SECRET_KEY: Clerk secret key (for testing token)
 *
 * The session is saved to e2e/.auth/user.json and reused by the chromium project.
 */
setup('authenticate', async ({ page }) => {
  const email = process.env.TEST_CLERK_EMAIL;
  const password = process.env.TEST_CLERK_PASSWORD;

  // Skip if credentials not provided - tests will run without auth
  if (!email || !password) {
    console.log('⚠️  TEST_CLERK_EMAIL or TEST_CLERK_PASSWORD not set - skipping auth setup');
    console.log('   Tests requiring authentication will be skipped.');
    setup.skip();
    return;
  }

  // Set up Clerk testing token to bypass bot detection
  await setupClerkTestingToken({ page });

  // Navigate to home page
  await page.goto('/');

  // Wait for Clerk to load
  await page.waitForTimeout(2000);

  // Click login button to open Clerk sign-in
  const loginButton = page.locator('button:has-text("Log In"), a:has-text("Log In"), [data-testid="login-button"]');
  await loginButton.waitFor({ state: 'visible', timeout: 10000 });
  await loginButton.click();

  // Wait for Clerk sign-in UI (could be modal or redirect)
  await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 15000 });

  // Fill in email
  await page.fill('input[name="identifier"], input[type="email"]', email);

  // Click continue button
  await page.click('button:has-text("Continue")');

  // Wait for password field
  await page.waitForSelector('input[type="password"]', { timeout: 10000 });

  // Fill in password
  await page.fill('input[type="password"]', password);

  // Click submit button
  await page.click('button:has-text("Continue"), button[type="submit"]');

  // Wait for redirect to dashboard (indicates successful login)
  await page.waitForURL(/\/(dashboard|accounts|transactions)/, { timeout: 30000 });

  // Wait for page to stabilize
  await page.waitForTimeout(2000);

  // Verify we're logged in by checking for logout button or user menu
  const loggedInIndicator = page.locator('button:has-text("Log Out"), [data-testid="user-menu"], [data-testid="logout-button"]');
  await expect(loggedInIndicator.first()).toBeVisible({ timeout: 10000 });

  console.log('✅ Authentication successful - saving session state');

  // Save the authentication state
  await page.context().storageState({ path: authFile });
});
