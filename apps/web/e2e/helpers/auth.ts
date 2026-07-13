import { Browser, BrowserContext, Page, expect, test, TestInfo } from '@playwright/test';
import { clerk } from '@clerk/testing/playwright';
import { ensureTestUserExists } from './clerk-admin';
import { getAuthToken, API_BASE } from './api/client';

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
 * Accept cookie consent banner (GDPR).
 *
 * Handles two call patterns:
 *
 * Pattern A — called BEFORE goto() (e.g. category-editor, savings):
 *   page.addInitScript() pre-sets the cookie before the next navigation so the
 *   banner never renders. The DOM-click fallback is a no-op (no page loaded yet).
 *
 * Pattern B — called AFTER goto() (e.g. landing page):
 *   addInitScript() takes effect on future navigations only, so we also perform
 *   a DOM-click to dismiss any banner already visible on the current page.
 *   This also restores the implicit ~2 s timing buffer that the old implementation
 *   provided (waiting for the banner) which gave the React app time to hydrate.
 *
 * @param page - Playwright page object
 */
export async function acceptCookieConsent(page: Page): Promise<void> {
  // 1. Register init script for future navigations (Pattern A — before goto)
  await page.addInitScript(() => {
    const consent = {
      consents: { necessary: true, analytics: false, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0', // Must match COOKIE_CONSENT_VERSION in src/privacy/types.ts
    }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
  })

  // 2. Dismiss any banner already on the page (Pattern B — after goto)
  try {
    const acceptButton = page.locator('button:has-text("Accept"), button:has-text("Accept All")')
    if (await acceptButton.isVisible({ timeout: 2000 })) {
      await acceptButton.click()
      await page.locator('[aria-label="Cookie Consent"]').waitFor({ state: 'hidden', timeout: 5_000 })
    }
  } catch {
    // Cookie banner not present or already accepted — no action needed
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

/**
 * Result of createSecondIdentityContext: a fresh, independently-authenticated
 * browser context + page, plus a close() convenience for test cleanup.
 */
export interface SecondIdentitySession {
  context: BrowserContext;
  page: Page;
  close: () => Promise<void>;
}

/**
 * Open a second, independently-authenticated Page for household invite E2E tests
 * (issue #1370's "invitee" identity).
 *
 * Design choice: this uses an in-test `browser.newContext()` + `clerk.signIn()` rather
 * than a second Playwright project + `*.setup.ts` producing a persisted `e2e/.auth/invitee.json`.
 * Only 1-2 specs need a second identity, so a per-spec context avoids a global
 * project-matrix change in playwright.config.ts. It deliberately does NOT touch or reuse
 * the primary `e2e/.auth/user.json` storage state — the returned context/page are fully
 * separate from the default `page` fixture (which is pre-authenticated as the primary user
 * via the `chromium` project's `storageState: authFile`).
 *
 * Calls ensureTestUserExists(email) first since the invitee may not exist yet in this
 * Clerk test tenant. Also grants a trial subscription (mirroring auth.setup.ts's
 * grant-trial call for the primary user) so the invitee can pass PremiumRoute's
 * subscription-status gate (src/auth/PremiumRoute.tsx) and reach protected routes like
 * /dashboard — a brand-new user's first profile fetch normally auto-grants a trial via
 * registration_service.go, but auth.setup.ts's own comments document a known race where
 * that grant doesn't land before the first protected-route check, hence the explicit
 * defensive call here too.
 *
 * Caller is responsible for calling the returned close() (or context.close()) when done.
 */
export async function createSecondIdentityContext(
  browser: Browser,
  email: string
): Promise<SecondIdentitySession> {
  await ensureTestUserExists(email);

  const context = await browser.newContext();

  // Exception-safety: if anything below throws (goto, signIn, waitForFunction, etc.),
  // close the context we just opened before rethrowing. Without this, a failure here
  // would leak the context for the rest of the test run — the caller only receives (and
  // can only close) a session object if this function returns successfully, so it has no
  // handle to clean up a partially-constructed one.
  try {
    const page = await context.newPage();

    await page.goto('/');
    await acceptCookieConsent(page);

    await clerk.signIn({ page, emailAddress: email });

    // Mirror auth.setup.ts's post-signIn stabilization: clerk.signIn() can trigger a page
    // reload that destroys the JS execution context, so wait for load + a brief hydration
    // pause before any page.evaluate()-based calls (e.g. getAuthToken via apiClient).
    await page.waitForLoadState('domcontentloaded');
    await page.waitForTimeout(2000);

    try {
      await page.waitForFunction(
        () => {
          const clerkWindow = (window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk;
          return clerkWindow?.session?.id != null;
        },
        { timeout: 15000 }
      );
    } catch {
      // Session may still hydrate on first API call; let the caller's assertions surface
      // any real failure rather than throwing here.
    }

    // Best-effort trial grant — same admin endpoint auth.setup.ts uses for the primary
    // user. Not fatal if it fails (e.g. endpoint disabled outside ENVIRONMENT=qa, or the
    // auto-grant on first profile fetch already succeeded) — let the caller's own
    // assertions be the source of truth on whether the invitee can reach protected routes.
    const doTrialGrant = async (authToken: string | null): Promise<boolean> => {
      try {
        const response = await page.request.post(
          `${API_BASE}/api/v1/admin/subscription/grant-trial`,
          {
            headers: authToken ? { Authorization: `Bearer ${authToken}` } : {},
          }
        );
        return response.ok();
      } catch {
        return false;
      }
    };

    const token = await getAuthToken(page);
    await doTrialGrant(token);

    // Mirror auth.setup.ts's recovery path: the grant-trial call can race Clerk's
    // session hydration (or land before the grant has propagated), leaving the invitee
    // stuck on the pricing gate instead of /dashboard. Check page state the same way
    // auth.setup.ts does and, if we landed on pricing, re-extract a fresh JWT and retry
    // the grant once before handing the session back to the caller. Still best-effort —
    // no throw here — the caller's own assertions remain the source of truth.
    await page.goto('/dashboard');
    await page.waitForLoadState('domcontentloaded');

    const dashboard = page.locator('h1:has-text("Dashboard")');
    const pricing = page.locator('text=Choose your plan');
    const pricingLoading = page.locator('text=Loading pricing plans');

    const checkPageState = () =>
      Promise.race([
        dashboard.waitFor({ timeout: 30000 }).then(() => 'dashboard' as const),
        pricing.waitFor({ timeout: 30000 }).then(() => 'pricing' as const),
        pricingLoading.waitFor({ timeout: 30000 }).then(() => 'pricingLoading' as const),
      ]).catch(() => 'timeout' as const);

    const initialResult = await checkPageState();

    if (initialResult === 'pricing' || initialResult === 'pricingLoading') {
      const freshToken = await getAuthToken(page);
      const granted = await doTrialGrant(freshToken);
      if (granted) {
        await page.goto('/dashboard');
        await page.waitForLoadState('domcontentloaded');
        await checkPageState();
      }
    }

    return {
      context,
      page,
      close: () => context.close(),
    };
  } catch (err) {
    await context.close();
    throw err;
  }
}

