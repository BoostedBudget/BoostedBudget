import { test, expect } from '@playwright/test'

/**
 * Privacy Settings E2E Tests - Public Pages
 *
 * Tests for unauthenticated access to privacy-related pages:
 * - Privacy Policy (public page)
 * - Redirect behavior for protected routes
 *
 * These tests run in chromium-no-auth project (user not logged in)
 */

test.describe('Privacy Settings - Public', () => {
  test.beforeEach(async ({ context }) => {
    // Clear cookies before each test
    await context.clearCookies()
  })

  test('should redirect unauthenticated users to login when accessing /profile', async ({ page }) => {
    // No Clerk auth token - this test verifies unauthenticated redirect behavior
    // Navigate to profile page (requires auth)
    await page.goto('/profile')

    // Wait for redirect to complete (to home or Clerk)
    // Using Playwright's auto-retry assertion to handle redirect timing
    await expect(page).not.toHaveURL(/\/profile/, { timeout: 10000 })

    // Verify landed somewhere valid (root or Clerk)
    const url = page.url()
    const isAtRoot = url.includes('localhost') && url.endsWith('/')
    const isAtClerk = url.includes('clerk.')
    expect(isAtRoot || isAtClerk).toBe(true)
  })

  test('should show Privacy Policy page without authentication', async ({ page }) => {
    // No Clerk auth token - Privacy Policy is a public page
    await page.goto('/legal/privacy-policy')
    await page.waitForLoadState('domcontentloaded')

    // Verify Privacy Policy page loads
    await expect(page.locator('h1:has-text("Privacy Policy")')).toBeVisible()

    // Verify key GDPR sections present
    await expect(page.locator('h2:has-text("Information We Collect")')).toBeVisible()
    await expect(page.locator('h2:has-text("Your Privacy Rights")')).toBeVisible()
    await expect(page.locator('h2:has-text("Cookie Policy")')).toBeVisible()

    // Verify US region mentioned (get first match to avoid strict mode violation)
    await expect(page.getByText(/us-east-1|Virginia, USA/i).first()).toBeVisible()
  })
})
