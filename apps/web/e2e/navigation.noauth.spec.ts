import { test, expect } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Navigation E2E Tests - Unauthenticated
 * Tests for the redesigned header with:
 * - Sticky header positioning
 * - Mobile hamburger menu with Sheet drawer
 * - Desktop navigation links (when not logged in)
 * - Accessibility features
 *
 * These tests run in chromium-no-auth project (user not logged in)
 */

test.describe('Navigation - Unauthenticated', () => {
  test.beforeEach(async ({ page }) => {
    // No Clerk auth token - these tests verify unauthenticated behavior
    await page.goto('/');
    await acceptCookieConsent(page);
  });

  test.describe('Header Structure', () => {
    test('should show sticky header', async ({ page }) => {
      const header = page.locator('header[role="banner"]');
      await expect(header).toBeVisible();

      // Verify sticky positioning via CSS
      await expect(header).toHaveCSS('position', 'sticky');
    });

    test('should show logo', async ({ page }) => {
      await expect(page.locator('header').getByText('BoostedBudget')).toBeVisible();
    });

    test('should show rocket emoji', async ({ page }) => {
      await expect(page.getByText('🚀')).toBeVisible();
    });

    test('should show login button on desktop', async ({ page }) => {
      // Set desktop viewport
      await page.setViewportSize({ width: 1280, height: 720 });

      await expect(page.getByRole('button', { name: /log in/i })).toBeVisible();
    });

    test('should not show navigation links when not authenticated', async ({ page }) => {
      // Navigation should not be visible to unauthenticated users
      await expect(page.locator('nav[aria-label="Main navigation"]')).not.toBeVisible();
    });
  });

  test.describe('Mobile Navigation', () => {
    test.beforeEach(async ({ page }) => {
      // Set mobile viewport
      await page.setViewportSize({ width: 375, height: 667 });
    });

    test('should show hamburger menu on mobile', async ({ page }) => {
      await expect(page.getByRole('button', { name: /open menu/i })).toBeVisible();
    });

    test('should open mobile nav drawer', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');

      // Sheet should open - use data-slot to distinguish from cookie consent dialog
      await expect(page.locator('[data-slot="sheet-content"]')).toBeVisible();
    });

    test('should show logo in mobile drawer', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');

      // Logo should appear in the drawer - use data-slot for Sheet component
      await expect(page.locator('[data-slot="sheet-content"]').getByText('BoostedBudget')).toBeVisible();
    });

    test('should show login button in mobile drawer when not authenticated', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');

      await expect(page.locator('[data-slot="sheet-content"]').getByRole('button', { name: /log in/i })).toBeVisible();
    });

    test('should close drawer on close button click', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');
      await expect(page.locator('[data-slot="sheet-content"]')).toBeVisible();

      // Click close button (X) in the sheet
      await page.locator('[data-slot="sheet-content"]').getByRole('button', { name: 'Close' }).click();

      await expect(page.locator('[data-slot="sheet-content"]')).not.toBeVisible();
    });
  });

  test.describe('Desktop Navigation', () => {
    test.beforeEach(async ({ page }) => {
      // Set desktop viewport
      await page.setViewportSize({ width: 1280, height: 720 });
    });

    test('should hide hamburger on desktop', async ({ page }) => {
      // Hamburger should be hidden (md:hidden class)
      await expect(page.getByRole('button', { name: /open menu/i })).not.toBeVisible();
    });

    test('should show login button on desktop', async ({ page }) => {
      await expect(page.getByRole('button', { name: /log in/i })).toBeVisible();
    });
  });

  test.describe('Accessibility', () => {
    test('should have proper ARIA landmarks', async ({ page }) => {
      // Header should have banner role
      await expect(page.locator('header[role="banner"]')).toBeVisible();
    });

    test('should have accessible mobile menu button', async ({ page }) => {
      await page.setViewportSize({ width: 375, height: 667 });

      const menuButton = page.getByRole('button', { name: /open menu/i });
      await expect(menuButton).toBeVisible();
      await expect(menuButton).toHaveAttribute('aria-label', 'Open menu');
    });
  });
});

test.describe('Navigation - Logo Links', () => {
  test('should link logo to home when not authenticated', async ({ page }) => {
    await page.goto('/');
    await acceptCookieConsent(page);

    const logoLink = page.locator('header a').first();
    await expect(logoLink).toHaveAttribute('href', '/');
  });
});
