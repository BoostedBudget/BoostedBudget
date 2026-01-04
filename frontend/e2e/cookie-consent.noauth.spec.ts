import { test, expect } from '@playwright/test';

/**
 * GDPR Cookie Consent Banner E2E Tests
 * Verifies cookie consent banner appears and functions correctly
 */

test.describe('Cookie Consent Banner', () => {
  test.beforeEach(async ({ context }) => {
    // Clear cookies before each test to ensure banner appears
    await context.clearCookies();
  });

  test('should display cookie consent banner on first visit', async ({ page }) => {
    // Navigate to the homepage (no auth needed - cookie banner is public)
    await page.goto('/');

    // Wait for the page to load
    await page.waitForLoadState('networkidle');

    // Verify cookie consent banner is visible
    const banner = page.locator('.cookie-consent-banner');
    await expect(banner).toBeVisible();

    // Verify banner has the expected heading
    await expect(page.locator('h2:has-text("Cookie Preferences")')).toBeVisible();

    // Verify description mentions cookies
    await expect(page.locator('.cookie-consent-description')).toContainText('cookies');
  });

  test('should have Accept All, Necessary Only, and Customize buttons', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify all three primary buttons are visible
    await expect(page.locator('button:has-text("Accept All")')).toBeVisible();
    await expect(page.locator('button:has-text("Necessary Only")')).toBeVisible();
    await expect(page.locator('button:has-text("Customize")')).toBeVisible();
  });

  test('should hide banner after clicking Accept All', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Click Accept All button
    await page.click('button:has-text("Accept All")');

    // Wait for banner to disappear
    await page.waitForTimeout(500);

    // Verify banner is hidden
    const banner = page.locator('.cookie-consent-banner');
    await expect(banner).not.toBeVisible();
  });

  test('should hide banner after clicking Necessary Only', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Click Necessary Only button
    await page.click('button:has-text("Necessary Only")');

    // Wait for banner to disappear
    await page.waitForTimeout(500);

    // Verify banner is hidden
    const banner = page.locator('.cookie-consent-banner');
    await expect(banner).not.toBeVisible();
  });

  test('should show detailed preferences when clicking Customize', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Click Customize button
    await page.click('button:has-text("Customize")');

    // Wait for details to appear
    await page.waitForTimeout(300);

    // Verify detailed options are visible
    await expect(page.locator('label:has-text("Necessary Cookies")')).toBeVisible();
    await expect(page.locator('label:has-text("Analytics Cookies")')).toBeVisible();
    await expect(page.locator('label:has-text("Advertising Cookies")')).toBeVisible();

    // Verify Save Preferences button is visible
    await expect(page.locator('button:has-text("Save Preferences")')).toBeVisible();
  });

  test('should have necessary cookies checkbox checked and disabled', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Open customize view
    await page.click('button:has-text("Customize")');
    await page.waitForTimeout(300);

    // Verify necessary cookies checkbox is checked and disabled
    const necessaryCheckbox = page.locator('#consent-necessary');
    await expect(necessaryCheckbox).toBeChecked();
    await expect(necessaryCheckbox).toBeDisabled();
  });

  test('should allow toggling analytics and advertising cookies', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Open customize view
    await page.click('button:has-text("Customize")');
    await page.waitForTimeout(300);

    // Get analytics checkbox
    const analyticsCheckbox = page.locator('#consent-analytics');
    const initialAnalyticsState = await analyticsCheckbox.isChecked();

    // Toggle analytics checkbox
    await analyticsCheckbox.click();
    await page.waitForTimeout(100);

    // Verify state changed
    const newAnalyticsState = await analyticsCheckbox.isChecked();
    expect(newAnalyticsState).toBe(!initialAnalyticsState);

    // Get advertising checkbox
    const advertisingCheckbox = page.locator('#consent-advertising');
    const initialAdvertisingState = await advertisingCheckbox.isChecked();

    // Toggle advertising checkbox
    await advertisingCheckbox.click();
    await page.waitForTimeout(100);

    // Verify state changed
    const newAdvertisingState = await advertisingCheckbox.isChecked();
    expect(newAdvertisingState).toBe(!initialAdvertisingState);
  });

  test('should save custom preferences and hide banner', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Open customize view
    await page.click('button:has-text("Customize")');
    await page.waitForTimeout(300);

    // Toggle analytics on
    await page.locator('#consent-analytics').click();

    // Click Save Preferences
    await page.click('button:has-text("Save Preferences")');

    // Wait for banner to disappear
    await page.waitForTimeout(500);

    // Verify banner is hidden
    const banner = page.locator('.cookie-consent-banner');
    await expect(banner).not.toBeVisible();
  });

  test('should not show banner on subsequent visit after accepting', async ({ page, context: _context }) => {
    // First visit - accept cookies
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.click('button:has-text("Accept All")');
    await page.waitForTimeout(500);

    // Navigate away and back
    await page.goto('/about'); // Assuming an about page exists, or use '/'
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify banner does not appear
    const banner = page.locator('.cookie-consent-banner');
    await expect(banner).not.toBeVisible();
  });

  test('should have links to Privacy and Cookie policies', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify privacy policy link exists
    const privacyLink = page.locator('a:has-text("Privacy Policy")');
    await expect(privacyLink).toBeVisible();
    await expect(privacyLink).toHaveAttribute('href', '/legal/privacy-policy');

    // Verify cookie policy link exists
    const cookieLink = page.locator('a:has-text("Cookie Policy")');
    await expect(cookieLink).toBeVisible();
    await expect(cookieLink).toHaveAttribute('href', '/legal/cookie-policy');
  });

  test('should have proper ARIA labels for accessibility', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Verify banner has role dialog
    const banner = page.locator('[role="dialog"]');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute('aria-label', 'Cookie Consent');

    // Verify buttons have aria-labels
    await expect(page.locator('button[aria-label="Accept all cookies"]')).toBeVisible();
    await expect(page.locator('button[aria-label="Accept only necessary cookies"]')).toBeVisible();
    await expect(page.locator('button[aria-label="Customize cookie preferences"]')).toBeVisible();
  });
});
