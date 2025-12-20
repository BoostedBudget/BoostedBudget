import { test, expect } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Navigation E2E Tests
 * Tests for the redesigned header with:
 * - Sticky header positioning
 * - Mobile hamburger menu with Sheet drawer
 * - Desktop navigation links
 * - User profile dropdown (when authenticated)
 * - Accessibility features
 */

test.describe('Navigation - Unauthenticated', () => {
  test.beforeEach(async ({ page }) => {
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
      await expect(page.getByText('BoostedBudget')).toBeVisible();
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

      // Sheet should open (dialog role)
      await expect(page.getByRole('dialog')).toBeVisible();
    });

    test('should show logo in mobile drawer', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');

      // Logo should appear in the drawer
      await expect(page.locator('[role="dialog"]').getByText('BoostedBudget')).toBeVisible();
    });

    test('should show login button in mobile drawer when not authenticated', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');

      await expect(page.locator('[role="dialog"]').getByRole('button', { name: /log in/i })).toBeVisible();
    });

    test('should close drawer on close button click', async ({ page }) => {
      await page.click('button[aria-label="Open menu"]');
      await expect(page.getByRole('dialog')).toBeVisible();

      // Click close button (X) in the sheet
      await page.click('[role="dialog"] button[aria-label="Close"]');

      await expect(page.getByRole('dialog')).not.toBeVisible();
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

// Tests that require authentication would go here
// Skipped because they require Auth0 configuration
test.describe.skip('Navigation - Authenticated', () => {
  test('should show navigation links when authenticated', async ({ page: _page }) => {
    // This test requires authentication setup
    // Would verify: Dashboard, Accounts, Transactions, Budgets, Auto-Cat links
  });

  test('should show user dropdown when authenticated', async ({ page: _page }) => {
    // Would verify the user menu button is visible
  });

  test('should open user dropdown and show menu items', async ({ page: _page }) => {
    // Would verify: View Profile, Privacy Settings, Log out
  });

  test('should navigate to profile via dropdown', async ({ page: _page }) => {
    // Would click "View Profile" and verify navigation
  });

  test('should apply active styling to current page link', async ({ page: _page }) => {
    // Would verify font-semibold class on current route link
  });

  test('should logout via dropdown', async ({ page: _page }) => {
    // Would click Log out and verify logged out state
  });

  test('should show navigation links in mobile drawer', async ({ page: _page }) => {
    // Would verify all nav links appear in mobile sheet
  });

  test('should close mobile drawer when nav link clicked', async ({ page: _page }) => {
    // Would click a nav link and verify drawer closes
  });
});
