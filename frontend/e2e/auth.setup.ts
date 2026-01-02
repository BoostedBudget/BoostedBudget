import { clerk } from '@clerk/testing/playwright';
import { test as setup } from '@playwright/test';

const authFile = 'e2e/.auth/user.json';

/**
 * Authentication Setup for E2E Tests
 *
 * This setup test logs in once using Clerk's programmatic sign-in helper
 * and saves the browser session state, which is then reused by all authenticated tests.
 *
 * Uses clerk.signIn() helper which:
 * - Handles testing token setup internally (no need for setupClerkTestingToken)
 * - Provides fast, reliable authentication
 * - Works with Clerk's bot detection bypass
 *
 * Required environment variables:
 * - TEST_CLERK_EMAIL: Email of a Clerk test user
 * - TEST_CLERK_PASSWORD: Password for the test user
 * - CLERK_PUBLISHABLE_KEY: Clerk publishable key
 * - CLERK_SECRET_KEY: Clerk secret key
 *
 * The session is saved to e2e/.auth/user.json and reused by the chromium project.
 */

// Increase timeout for auth setup (Clerk sign-in may take time)
setup.setTimeout(60000);

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

  console.log(`🔐 Authenticating with email: ${email}`);

  // Navigate to home (required before clerk.signIn)
  await page.goto('/');

  // Handle cookie consent if present
  try {
    const acceptBtn = page.locator('button:has-text("Accept All")');
    if (await acceptBtn.isVisible({ timeout: 2000 })) {
      await acceptBtn.click();
      console.log('   Dismissed cookie consent dialog');
    }
  } catch {
    // Cookie dialog may not be present, continue
  }

  // Sign in using Clerk's helper with emailAddress parameter
  // This uses CLERK_SECRET_KEY to directly sign in the user
  // Alternative to signInParams - may be more reliable
  console.log('   Signing in with Clerk (using emailAddress method)...');
  await clerk.signIn({
    page,
    emailAddress: email,
  });

  // Wait for the session to be established
  // Clerk needs time to set up cookies and session state
  console.log('   Waiting for session to establish...');
  await page.waitForLoadState('networkidle');

  // Wait for Clerk user button to appear (indicates logged in state)
  // This ensures the session is properly established before proceeding
  try {
    await page.waitForSelector('[data-testid="user-button"], button[aria-label*="User"], .cl-userButtonBox', {
      timeout: 10000,
    });
    console.log('   ✓ User session established');
  } catch {
    console.log('   ⚠️  User button not found, session may not be established');
    // Continue anyway - some UI may not show user button
  }

  // Verify by accessing protected page
  // This confirms the session is properly established
  console.log('   Verifying access to protected page...');
  await page.goto('/dashboard');
  await page.waitForSelector('h1:has-text("Dashboard")', { timeout: 15000 });

  console.log('✅ Authentication successful - saving session state');
  await page.context().storageState({ path: authFile });
});
