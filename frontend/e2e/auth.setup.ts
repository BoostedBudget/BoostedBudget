import { test as setup, expect } from '@playwright/test';
import { clerk, setupClerkTestingToken } from '@clerk/testing/playwright';

const authFile = 'e2e/.auth/user.json';

/**
 * Authentication Setup for E2E Tests
 *
 * This setup test logs in once using Clerk's programmatic sign-in helper
 * and saves the browser session state, which is then reused by all authenticated tests.
 *
 * Uses clerk.signIn() helper instead of manual UI interaction for:
 * - Faster, more reliable authentication
 * - No dependency on Clerk UI selectors
 * - Works with Testing Token to bypass bot detection
 *
 * Required environment variables:
 * - TEST_CLERK_EMAIL: Email of a Clerk test user
 * - TEST_CLERK_PASSWORD: Password for the test user
 * - CLERK_PUBLISHABLE_KEY: Clerk publishable key (for testing token)
 * - CLERK_SECRET_KEY: Clerk secret key (for testing token)
 *
 * Note: Test user must be created in Clerk Dashboard with username+password auth
 * (not OAuth) as OAuth providers block automated tools.
 *
 * The session is saved to e2e/.auth/user.json and reused by the chromium project.
 */
// Increase timeout for auth setup (default 15s is too short for Clerk auth flow)
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

  // Set up Clerk testing token to bypass bot detection
  await setupClerkTestingToken({ page });

  // Navigate to home page (required before clerk.signIn)
  await page.goto('/');

  // Dismiss cookie consent dialog if present
  try {
    const acceptButton = page.locator('button:has-text("Accept All"), button:has-text("Necessary Only")');
    if (await acceptButton.isVisible({ timeout: 2000 })) {
      await acceptButton.first().click();
      console.log('   Dismissed cookie consent dialog');
    }
  } catch {
    // Cookie dialog may not be present, continue
  }

  // Wait for Clerk to initialize
  await page.waitForTimeout(2000);

  // Use Clerk's programmatic sign-in helper
  // This is faster and more reliable than manual UI interaction
  console.log('   Attempting clerk.signIn() with password strategy...');
  try {
    await clerk.signIn({
      page,
      signInParams: {
        strategy: 'password',
        identifier: email,
        password: password,
      },
    });
    console.log('   ✓ clerk.signIn() completed');

    // Wait for Clerk to process the sign-in
    await page.waitForTimeout(3000);

    // Check if user appears signed in (UserButton or similar should be visible)
    const userButton = page.locator('.cl-userButtonBox, [data-clerk-user-button], button:has-text("Log Out")');
    const isSignedIn = await userButton.isVisible({ timeout: 5000 }).catch(() => false);

    if (!isSignedIn) {
      console.log('   ⚠️  clerk.signIn() completed but user not visible, falling back to manual sign-in');
      throw new Error('User not signed in after clerk.signIn()');
    }
  } catch (error) {
    console.error('❌ clerk.signIn() failed:', error);
    console.log('   Falling back to manual UI sign-in...');

    // Fallback to manual UI sign-in if clerk.signIn() fails
    await manualSignIn(page, email, password);
  }

  // Navigate to dashboard to verify authentication
  await page.goto('/dashboard');

  // Wait for dashboard to load (indicates successful login)
  // Use a longer timeout as the first authenticated request may be slow
  try {
    await page.waitForSelector('[data-testid="dashboard"], [data-testid="account-summary"], .dashboard-content', {
      timeout: 15000,
    });
  } catch {
    // If specific selector not found, just verify we're on dashboard URL
    await expect(page).toHaveURL(/\/(dashboard|accounts|transactions)/, { timeout: 10000 });
  }

  console.log('✅ Authentication successful - saving session state');

  // Save the authentication state
  await page.context().storageState({ path: authFile });
});

/**
 * Fallback manual sign-in via UI
 * Used if clerk.signIn() helper fails
 */
async function manualSignIn(page: import('@playwright/test').Page, email: string, password: string) {
  console.log('   Starting manual UI sign-in...');

  // Dismiss cookie consent dialog if present (may appear again after redirect)
  try {
    const acceptButton = page.locator('button:has-text("Accept All"), button:has-text("Necessary Only")');
    if (await acceptButton.isVisible({ timeout: 1000 })) {
      await acceptButton.first().click();
    }
  } catch {
    // Cookie dialog may not be present
  }

  // Click login button to open Clerk sign-in
  const loginButton = page.locator('button:has-text("Log In"), a:has-text("Log In"), [data-testid="login-button"]');
  await loginButton.waitFor({ state: 'visible', timeout: 10000 });
  console.log('   Clicking Log In button...');
  await loginButton.click();

  // Wait for Clerk sign-in UI (could be modal or redirect)
  await page.waitForSelector('input[name="identifier"], input[type="email"]', { timeout: 15000 });

  // Fill in email
  console.log('   Filling email...');
  await page.fill('input[name="identifier"], input[type="email"]', email);

  // Click continue button
  console.log('   Clicking Continue...');
  await page.click('button:has-text("Continue")');

  // Wait for password field
  console.log('   Waiting for password field...');
  await page.waitForSelector('input[type="password"]', { timeout: 10000 });

  // Fill in password
  console.log('   Filling password...');
  await page.fill('input[type="password"]', password);

  // Click submit button (use visible Continue button in Clerk modal)
  console.log('   Clicking Continue/Submit...');
  const continueButton = page.locator('button:has-text("Continue"):visible').first();
  await continueButton.click();

  // Wait for either verification screen or successful login redirect
  console.log('   Waiting for verification screen or dashboard redirect...');

  // Race between verification screen and dashboard URL
  const verificationHeading = page.locator('h1:has-text("Check your email")');
  const dashboardUrl = /\/(dashboard|accounts|transactions)/;

  // Poll for either condition
  let needsVerification = false;
  for (let i = 0; i < 30; i++) {
    // Check if redirected to dashboard
    if (dashboardUrl.test(page.url())) {
      console.log('   ✓ Redirected to dashboard');
      break;
    }
    // Check if verification screen appeared
    if (await verificationHeading.isVisible().catch(() => false)) {
      needsVerification = true;
      break;
    }
    await page.waitForTimeout(500);
  }

  // Handle email verification if required (Clerk test mode: code is 424242)
  if (needsVerification) {
    console.log('   Email verification required - entering test code 424242...');
    const verificationInput = page.getByRole('textbox', { name: /verification code/i });
    await verificationInput.waitFor({ state: 'visible', timeout: 5000 });

    // Type the code character by character (Clerk may auto-submit on 6 digits)
    await verificationInput.pressSequentially('424242', { delay: 100 });
    console.log('   ✓ Verification code entered');

    // Wait for Clerk to process the verification (it may auto-redirect or show error)
    console.log('   Waiting for verification to complete...');

    // Wait for redirect or for an error message
    try {
      await page.waitForURL(dashboardUrl, { timeout: 15000 });
      console.log('   ✓ Auto-redirected after verification');
    } catch {
      // If not auto-redirected, try clicking Continue if enabled
      console.log('   Not auto-redirected, checking for enabled Continue button...');
      const verifyButton = page.locator('button:has-text("Continue"):visible:not([disabled])').first();
      if (await verifyButton.isVisible({ timeout: 5000 }).catch(() => false)) {
        await verifyButton.click();
        console.log('   ✓ Clicked Continue button');
        await page.waitForURL(dashboardUrl, { timeout: 15000 });
      } else {
        // Check for error message
        const errorText = await page.locator('.cl-formFieldError, [data-clerk-error]').textContent().catch(() => null);
        if (errorText) {
          throw new Error(`Verification failed: ${errorText}`);
        }
        // Still waiting, give more time
        await page.waitForURL(dashboardUrl, { timeout: 15000 });
      }
    }
  }

  console.log('   ✓ Manual sign-in successful');
}
