import { test, expect } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Landing Page E2E Tests - Unauthenticated
 *
 * Tests for the marketing landing page displayed to first-time visitors.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 *
 * These tests run in chromium-no-auth project (user not logged in)
 */

test.describe('Landing Page - Unauthenticated', () => {
  // Clerk SDK initialization under parallel worker load is non-deterministic.
  // A single retry is enough to distinguish infrastructure flakiness (Clerk
  // slow to initialize under load) from genuine test failures (wrong content).
  // A test must fail TWICE consecutively to be reported as failed.
  test.describe.configure({ retries: 1 });

  test.beforeEach(async ({ page }) => {
    // Extend per-test budget: Clerk SDK initialization under parallel worker load can
    // exceed the global 15 s default, causing the beforeEach hook to be killed before
    // the header appears.  60 s gives ample headroom without making CI excessively slow.
    test.setTimeout(60000);
    // Navigate to home page as unauthenticated user
    await page.goto('/');
    // Wait for the landing header to appear — proves Clerk and React have hydrated.
    // Without this wait, intermittent flakiness occurs because page.goto() resolves
    // at the browser 'load' event, before React renders the Clerk-dependent header.
    // 50 s timeout: Clerk SDK initialization under parallel worker load occasionally
    // takes much longer than expected (network + JS bundle parsing under load).
    await page.locator('header[role="banner"]').waitFor({ state: 'visible', timeout: 50000 });
    await acceptCookieConsent(page);
  });

  test.describe('Hero Section', () => {
    test('should display main headline', async ({ page }) => {
      const headline = page.getByRole('heading', { level: 1 });
      await expect(headline).toBeVisible();
      await expect(headline).toContainText('Take Control');
    });

    test('should display primary CTA button', async ({ page }) => {
      const ctaButton = page.getByRole('button', { name: /start free today/i }).first();
      await expect(ctaButton).toBeVisible();
    });

    test('should display secondary CTA', async ({ page }) => {
      await expect(page.getByText(/see how it works/i)).toBeVisible();
    });

    test('should display trust badges', async ({ page }) => {
      await expect(page.getByText(/no credit card required/i).first()).toBeVisible();
      await expect(page.getByText(/gdpr compliant/i).first()).toBeVisible();
    });
  });

  test.describe('Features Section', () => {
    test('should display features section', async ({ page }) => {
      const featuresHeading = page.getByRole('heading', { name: /why choose boostedbudget/i });
      await expect(featuresHeading).toBeVisible();
    });

    test('should display key features', async ({ page }) => {
      await expect(page.getByText(/all your accounts in one place/i)).toBeVisible();
      await expect(page.getByText(/bank-level security/i)).toBeVisible();
    });
  });

  test.describe('How It Works Section', () => {
    test('should display how it works section', async ({ page }) => {
      const howItWorksHeading = page.getByRole('heading', { name: /how it works/i });
      await expect(howItWorksHeading).toBeVisible();
    });

    test('should display 3 steps', async ({ page }) => {
      // All steps should be visible (using heading role with exact match to avoid nav links)
      await expect(page.getByRole('heading', { name: 'Connect', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Organize', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Automate', exact: true })).toBeVisible();
    });

    test('should scroll to how it works when secondary CTA clicked', async ({ page }) => {
      // Click "See How It Works" button
      await page.getByRole('button', { name: /see how it works/i }).click();

      // Wait for smooth scroll to complete
      await page.waitForTimeout(500);

      // How it works section should be in viewport
      const howItWorksSection = page.locator('#how-it-works');
      await expect(howItWorksSection).toBeInViewport();
    });
  });

  test.describe('Social Proof Section', () => {
    test('should display security messaging', async ({ page }) => {
      // Test the visible label (tooltip text is hidden by default)
      await expect(page.getByText(/gdpr compliant/i).first()).toBeVisible();
    });

    test('should display trust badges', async ({ page }) => {
      await expect(page.getByText(/powered by plaid/i)).toBeVisible();
    });
  });

  test.describe('Pricing Preview Section', () => {
    // Mock subscription tiers API for pricing tests
    test.beforeEach(async ({ page }) => {
      await page.route('**/api/v1/subscription/tiers', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            tiers: {
              premium: {
                display_name: 'Premium',
                description: 'Best for individuals',
                pricing: { price_monthly: 9.99 },
              },
              household_3: {
                display_name: 'Household 3',
                description: 'For small families',
                pricing: { price_monthly: 14.99 },
              },
              household_5: {
                display_name: 'Household 5',
                description: 'For large families',
                pricing: { price_monthly: 24.99 },
              },
            },
          }),
        });
      });
      // Reload to re-trigger the tiers API fetch with the mock active.
      // The parent beforeEach already navigated to '/' — no need to navigate again.
      await page.reload();
      await page.waitForLoadState('domcontentloaded');
      // Wait for the pricing section to render: the tiers API mock must respond and
      // React must process it before assertions on pricing content will succeed.
      // (domcontentloaded fires before async React state updates complete.)
      await page.getByRole('heading', { name: /simple.*pricing/i }).waitFor({
        state: 'visible',
        timeout: 30000,
      });
    });

    test('should display pricing section', async ({ page }) => {
      const pricingHeading = page.getByRole('heading', { name: /simple.*pricing/i });
      await expect(pricingHeading).toBeVisible();
    });

    test('should display pricing cards', async ({ page }) => {
      // At least one tier should be visible
      await expect(page.getByRole('heading', { name: 'Premium' })).toBeVisible();
    });

    test('should have link to full pricing page', async ({ page }) => {
      const pricingLink = page.getByRole('link', { name: /compare all plans/i });
      await expect(pricingLink).toBeVisible();
      await expect(pricingLink).toHaveAttribute('href', '/pricing');
    });

    test('should navigate to pricing page when link clicked', async ({ page }) => {
      await page.getByRole('link', { name: /compare all plans/i }).click();
      await expect(page).toHaveURL('/pricing');
    });
  });

  test.describe('FAQ Section', () => {
    test('should display FAQ section', async ({ page }) => {
      const faqHeading = page.getByRole('heading', { name: /frequently asked questions/i });
      await expect(faqHeading).toBeVisible();
    });

    test('should display FAQ questions', async ({ page }) => {
      await expect(page.getByText(/is my bank data secure/i)).toBeVisible();
      await expect(page.getByText(/can i cancel anytime/i)).toBeVisible();
    });

    test('should expand FAQ answer when question clicked', async ({ page }) => {
      // Click on a FAQ question
      const faqButton = page.getByRole('button', { name: /is my bank data secure/i });
      await faqButton.click();

      // Answer should be visible
      await expect(page.getByText(/plaid.*trusted service/i)).toBeVisible();
    });

    test('should collapse FAQ answer when clicked again', async ({ page }) => {
      const faqButton = page.getByRole('button', { name: /is my bank data secure/i });

      // Expand
      await faqButton.click();
      await expect(page.getByText(/plaid.*trusted service/i)).toBeVisible();

      // Collapse
      await faqButton.click();

      // Answer should be hidden (max-height: 0)
      const answer = page.locator('#faq-answer-0');
      await expect(answer).toHaveCSS('max-height', '0px');
    });
  });

  test.describe('Final CTA Section', () => {
    test('should display final CTA heading', async ({ page }) => {
      const ctaHeading = page.getByRole('heading', { name: /ready to take control/i });
      await expect(ctaHeading).toBeVisible();
    });

    test('should display final CTA button', async ({ page }) => {
      // Should have at least 2 "Start Free Today" buttons
      const ctaButtons = page.getByRole('button', { name: /start free today/i });
      // Below-fold content needs extra time in CI under resource contention
      await expect(ctaButtons).toHaveCount(2, { timeout: 10000 });
    });

    test('should display risk reversal messaging', async ({ page }) => {
      await expect(page.getByText(/2-month free trial/i).first()).toBeVisible({ timeout: 10000 });
      await expect(page.getByText(/cancel anytime/i).first()).toBeVisible({ timeout: 10000 });
    });
  });

  test.describe('Accessibility', () => {
    test('should have only one h1 heading', async ({ page }) => {
      const h1Elements = page.getByRole('heading', { level: 1 });
      await expect(h1Elements).toHaveCount(1);
    });

    test('should have main landmark', async ({ page }) => {
      await expect(page.getByRole('main')).toBeVisible();
    });

    test('should have accessible buttons', async ({ page }) => {
      // Wait for page to be fully settled before counting buttons (avoids flakiness
      // from dynamic elements like cookie banner appearing/disappearing mid-iteration)
      await page.waitForLoadState('domcontentloaded');
      const buttons = page.getByRole('button');
      const count = await buttons.count();

      for (let i = 0; i < count; i++) {
        const button = buttons.nth(i);
        // Each button should have accessible name
        const name = await button.getAttribute('aria-label') || await button.textContent();
        expect(name).toBeTruthy();
      }
    });
  });

  test.describe('Responsive Design', () => {
    test('should display correctly on mobile', async ({ page }) => {
      await page.setViewportSize({ width: 375, height: 667 });

      // Hero should still be visible
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

      // CTAs should be visible
      await expect(page.getByRole('button', { name: /start free today/i }).first()).toBeVisible();
    });

    test('should display correctly on tablet', async ({ page }) => {
      await page.setViewportSize({ width: 768, height: 1024 });

      // All sections should be visible
      await expect(page.getByRole('heading', { name: /why choose boostedbudget/i })).toBeVisible();
      await expect(page.getByRole('heading', { name: /how it works/i })).toBeVisible();
    });

    test('should display correctly on desktop', async ({ page }) => {
      // Mock subscription tiers API for pricing section
      await page.route('**/api/v1/subscription/tiers', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            tiers: {
              premium: { display_name: 'Premium', description: 'Test', pricing: { price_monthly: 9.99 } },
              household_3: { display_name: 'Household 3', description: 'Test', pricing: { price_monthly: 14.99 } },
              household_5: { display_name: 'Household 5', description: 'Test', pricing: { price_monthly: 24.99 } },
            },
          }),
        });
      });
      // Re-navigate with mock
      await page.goto('/');
      await page.locator('header[role="banner"]').waitFor({ state: 'visible', timeout: 50000 });
      await acceptCookieConsent(page);
      await page.setViewportSize({ width: 1280, height: 720 });

      // Full layout should be visible
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await expect(page.getByRole('heading', { name: /simple.*pricing/i })).toBeVisible();
    });
  });

  test.describe('Navigation', () => {
    test('should show landing page header with proper accessibility', async ({ page }) => {
      // Landing page has its own header with role="banner"
      await expect(page.locator('header[role="banner"]')).toBeVisible();
      // Should show the BoostedBudget logo
      await expect(page.locator('header').getByText('BoostedBudget')).toBeVisible();
    });
  });
});
