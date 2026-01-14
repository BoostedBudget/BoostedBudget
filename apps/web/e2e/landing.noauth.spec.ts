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
  test.beforeEach(async ({ page }) => {
    // Navigate to home page as unauthenticated user
    await page.goto('/');
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
      // All steps should be visible
      await expect(page.getByText('Connect')).toBeVisible();
      await expect(page.getByText('Organize')).toBeVisible();
      await expect(page.getByText('Automate')).toBeVisible();
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
      await expect(page.getByText(/your privacy is our priority/i)).toBeVisible();
    });

    test('should display trust badges', async ({ page }) => {
      await expect(page.getByText(/powered by plaid/i)).toBeVisible();
    });
  });

  test.describe('Pricing Preview Section', () => {
    test('should display pricing section', async ({ page }) => {
      const pricingHeading = page.getByRole('heading', { name: /simple.*pricing/i });
      await expect(pricingHeading).toBeVisible();
    });

    test('should display pricing cards', async ({ page }) => {
      // At least one tier should be visible
      await expect(page.getByText('Premium')).toBeVisible();
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
      await expect(ctaButtons).toHaveCount(2);
    });

    test('should display risk reversal messaging', async ({ page }) => {
      await expect(page.getByText(/2-month free trial/i).first()).toBeVisible();
      await expect(page.getByText(/cancel anytime/i).first()).toBeVisible();
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
      await page.setViewportSize({ width: 1280, height: 720 });

      // Full layout should be visible
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await expect(page.getByRole('heading', { name: /simple.*pricing/i })).toBeVisible();
    });
  });

  test.describe('Navigation', () => {
    test('should not show main navigation header on landing page', async ({ page }) => {
      // Landing page uses its own layout without the standard header
      await expect(page.locator('header[role="banner"]')).not.toBeVisible();
    });
  });
});
