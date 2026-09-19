import { clerk } from '@clerk/testing/playwright';
import { test as setup } from '@playwright/test';
import { prepareRevenueCatFixture } from './helpers/revenuecat';

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

  // Issue #698: Add diagnostic listeners to capture errors
  // Browser console errors will now appear in test output
  page.on('console', msg => {
    if (msg.type() === 'error') {
      console.log(`   [Browser Error] ${msg.text()}`);
    }
  });

  // Capture API error responses for debugging
  page.on('response', response => {
    if (response.url().includes('/api/') && response.status() >= 400) {
      console.log(`   [API Error] ${response.status()} ${response.url()}`);
    }
  });

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

  // Wait for page to stabilize after sign-in.
  // clerk.signIn() can trigger a page reload that destroys the JS execution context.
  // Waiting for domcontentloaded + a brief hydration pause prevents "Execution context
  // was destroyed" errors during subsequent page.evaluate() calls.
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(2000);

  // Wait for Clerk session to be fully hydrated before proceeding.
  console.log('   Waiting for session to establish...');
  try {
    await page.waitForFunction(
      () => {
        const clerk = (window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk;
        return clerk?.session?.id != null;
      },
      { timeout: 15000 }
    );
    console.log('   ✓ User session established');
  } catch {
    console.log('   ⚠️  Clerk session not detected within 15s, continuing...');
  }

  console.log('   Preparing simulated RevenueCat access for the configured test identity...');
  await prepareRevenueCatFixture(page, email);

  // Verify by accessing protected page
  // This confirms the session is properly established
  console.log('   Verifying access to protected page...');
  await page.goto('/dashboard');

  // Wait for page to stabilize
  await page.waitForLoadState('domcontentloaded');

  // Handle cookie consent again if it reappears after navigation
  try {
    const acceptBtn = page.locator('button:has-text("Accept All")');
    if (await acceptBtn.isVisible({ timeout: 1000 })) {
      await acceptBtn.click();
      console.log('   Dismissed cookie consent dialog (post-navigation)');
    }
  } catch {
    // Cookie dialog may not be present, continue
  }

  // Issue #698: Diagnostic page state detection
  // Wait for ANY indicator and log what's showing for debugging
  const dashboard = page.locator('h1:has-text("Dashboard")');
  const profileError = page.locator('h2:has-text("Unable to Load Profile")');
  const pricing = page.locator('text=Choose your plan');
  const pricingLoading = page.locator('text=Loading pricing plans');
  const pricingError = page.locator('text=Failed to load pricing plans');
  const loadingSkeleton = page.locator('[data-testid="premium-loading"]');

  console.log('   Waiting for page to show dashboard or error state...');

  // Use Promise.race to detect which component appears first
  // Include all possible states for better diagnostics
  let result = await Promise.race([
    dashboard.waitFor({ timeout: 30000 }).then(() => 'dashboard' as const),
    profileError.waitFor({ timeout: 30000 }).then(() => 'profileError' as const),
    pricing.waitFor({ timeout: 30000 }).then(() => 'pricing' as const),
    pricingLoading.waitFor({ timeout: 30000 }).then(() => 'pricingLoading' as const),
    pricingError.waitFor({ timeout: 30000 }).then(() => 'pricingError' as const),
  ]).catch(() => 'timeout' as const);

  console.log(`   [Diagnostic] Page showing: ${result}`);

  // Recheck the fixture if navigation raced the previous profile response.
  if (result === 'pricing' || result === 'pricingLoading') {
    await prepareRevenueCatFixture(page, email);
    await page.goto('/dashboard');
    await page.waitForLoadState('domcontentloaded');
    result = await Promise.race([
      dashboard.waitFor({ timeout: 30000 }).then(() => 'dashboard' as const),
      profileError.waitFor({ timeout: 30000 }).then(() => 'profileError' as const),
      pricing.waitFor({ timeout: 30000 }).then(() => 'pricing' as const),
      pricingLoading.waitFor({ timeout: 30000 }).then(() => 'pricingLoading' as const),
      pricingError.waitFor({ timeout: 30000 }).then(() => 'pricingError' as const),
    ]).catch(() => 'timeout' as const);
    console.log(`   [Diagnostic] Page showing after recovery: ${result}`);
  }

  if (result !== 'dashboard') {
    // Take screenshot for debugging (will be uploaded as artifact)
    await page.screenshot({ path: 'e2e-auth-diagnostic.png' });

    // Try to capture any error message displayed on page
    const errorMsg = await page.locator('[data-testid="error-message"]').textContent().catch(() => null);
    if (errorMsg) {
      console.log(`   [Error Message] ${errorMsg}`);
    }

    // Check if we're stuck on loading skeleton
    const isLoading = await loadingSkeleton.isVisible().catch(() => false);
    if (isLoading) {
      console.log('   [Diagnostic] Still showing loading skeleton - profile API may be hanging');
    }

    throw new Error(`Auth verification failed - expected Dashboard but got: ${result}`);
  }

  console.log('✅ Authentication successful - saving session state');
  await page.context().storageState({ path: authFile });
});
