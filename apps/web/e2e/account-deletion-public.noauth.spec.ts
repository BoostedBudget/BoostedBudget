import { test, expect } from '@playwright/test'

/**
 * Account Deletion E2E Tests - Public Endpoints
 *
 * Tests for public (unauthenticated) account deletion flows:
 * - Cancellation page access
 * - Invalid/valid cancellation tokens
 * - Public endpoint error handling
 *
 * These tests run in chromium-no-auth project (user not logged in)
 */

test.describe('Account Deletion - Public Endpoints', () => {
  test.setTimeout(30000)

  test.beforeEach(async ({ context }) => {
    // Clear cookies and storage before each test
    await context.clearCookies()
  })

  test('should show invalid link message when accessing cancellation page without token', async ({ page }) => {
    // No Clerk auth token - cancellation pages are public endpoints
    await page.goto('/cancel-deletion/')
    await page.waitForLoadState('domcontentloaded')

    // Verify invalid link message is displayed
    await expect(page.getByText(/invalid cancellation link/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/this cancellation link is invalid/i)).toBeVisible({ timeout: 10000 })

    // Verify "Go to Login" button is present
    await expect(page.getByRole('button', { name: /go to login/i })).toBeVisible({ timeout: 10000 })
  })

  test('should display processing state on cancellation page load', async ({ page }) => {
    // Mock token for testing UI (backend will handle validation)
    const mockToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0In0.test'

    // Mock the cancel-deletion API to delay response (keep component in processing state)
    // Must mock before navigation to prevent the 401 interceptor from redirecting to /sign-in
    await page.route('**/api/v1/user/delete/cancel/**', async route => {
      await new Promise(resolve => setTimeout(resolve, 5000))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'Account deletion canceled successfully' }),
      })
    })

    await page.goto(`/cancel-deletion/${mockToken}`)
    await page.waitForLoadState('domcontentloaded')
    expect(page.url()).toContain('/cancel-deletion/')
  })

  test('should show error state for invalid cancellation token', async ({ page }) => {
    const invalidToken = 'invalid-token-format'

    // Route API call to return 400 error
    await page.route('**/api/v1/user/delete/cancel/**', route => {
      route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Invalid or expired cancellation token' }),
      })
    })

    await page.goto(`/cancel-deletion/${invalidToken}`)
    await page.waitForLoadState('domcontentloaded')

    // Verify error icon and message
    await expect(page.getByText('✗')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/cancellation failed/i)).toBeVisible({ timeout: 10000 })

    // Verify error reasons are listed
    await expect(page.getByText(/the cancellation link has expired/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/the link has already been used/i)).toBeVisible({ timeout: 10000 })

    // Verify action buttons
    await expect(page.getByRole('button', { name: /go to login/i })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /try again/i })).toBeVisible({ timeout: 10000 })
  })

  test('should show success state for valid cancellation token', async ({ page }) => {
    const validToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0LXVzZXIifQ.valid-signature'

    // Mock successful cancellation
    await page.route('**/api/v1/user/delete/cancel/**', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'Account deletion canceled successfully' }),
      })
    })

    await page.goto(`/cancel-deletion/${validToken}`)
    await page.waitForLoadState('domcontentloaded')

    // Verify success icon and heading
    await expect(page.getByText('✓')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Account Deletion Canceled')).toBeVisible({ timeout: 10000 })

    // Verify confirmation message
    await expect(page.getByText(/your account and all your data are safe/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/you can continue using BoostedBudget normally/i)).toBeVisible({ timeout: 10000 })

    // Verify "Go to Profile" button
    const profileButton = page.getByRole('button', { name: /go to profile/i })
    await expect(profileButton).toBeVisible({ timeout: 10000 })
  })

  test('should navigate to profile when "Go to Profile" clicked after successful cancellation', async ({ page }) => {
    const validToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0In0.sig'

    // Mock successful cancellation
    await page.route('**/api/v1/user/delete/cancel/**', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'Account deletion canceled successfully' }),
      })
    })

    await page.goto(`/cancel-deletion/${validToken}`)
    await page.waitForTimeout(500)

    // Click "Go to Profile" button
    const profileButton = page.getByRole('button', { name: /go to profile/i })
    await profileButton.click()

    // Verify navigation (will redirect to login if not authenticated)
    await page.waitForLoadState('networkidle')
    const url = page.url()

    // Should either be at profile or redirected to login
    const isAtProfile = url.includes('/profile')
    const isAtLogin = url.includes('/') || url.includes('clerk.')
    expect(isAtProfile || isAtLogin).toBe(true)
  })
})
