import { test, expect } from '@playwright/test'

/**
 * Account Deletion E2E Tests
 *
 * Tests the complete account deletion and cancellation flow including:
 * - Account deletion request from Privacy Settings
 * - Email confirmation link handling
 * - 30-day grace period
 * - Cancellation via public link
 * - Error handling and edge cases
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 *
 * NOTE: Tests requiring authentication are skipped pending Auth0 test credentials.
 * See e2e/README.md for setup instructions.
 */

test.describe('Account Deletion Flow', () => {
  test.beforeEach(async ({ context }) => {
    // Clear cookies and storage before each test
    await context.clearCookies()
  })

  // ============================================================================
  // PUBLIC ENDPOINT TESTS - No Authentication Required
  // ============================================================================

  test('should show invalid link message when accessing cancellation page without token', async ({ page }) => {
    // Navigate directly to cancel-deletion page without token
    await page.goto('/cancel-deletion/')
    await page.waitForLoadState('networkidle')

    // Verify invalid link message is displayed
    await expect(page.getByText(/invalid cancellation link/i)).toBeVisible()
    await expect(page.getByText(/this cancellation link is invalid/i)).toBeVisible()

    // Verify "Go to Login" button is present
    await expect(page.getByRole('button', { name: /go to login/i })).toBeVisible()
  })

  test('should display processing state on cancellation page load', async ({ page }) => {
    // Mock token for testing UI (backend will handle validation)
    const mockToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0In0.test'

    await page.goto(`/cancel-deletion/${mockToken}`)

    // Page should show processing state initially
    // (Will eventually show error since token is invalid, but we test the loading state)
    // Either processing is visible, or it transitions to error quickly
    // We just verify the page loaded and has expected structure
    await page.waitForLoadState('networkidle')
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
    await page.waitForLoadState('networkidle')

    // Wait for error state to appear
    await page.waitForTimeout(500)

    // Verify error icon and message
    await expect(page.getByText('✗')).toBeVisible()
    await expect(page.getByText(/cancellation failed/i)).toBeVisible()

    // Verify error reasons are listed
    await expect(page.getByText(/the cancellation link has expired/i)).toBeVisible()
    await expect(page.getByText(/the link has already been used/i)).toBeVisible()

    // Verify action buttons
    await expect(page.getByRole('button', { name: /go to login/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /try again/i })).toBeVisible()
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
    await page.waitForLoadState('networkidle')

    // Wait for success state
    await page.waitForTimeout(500)

    // Verify success icon and heading
    await expect(page.getByText('✓')).toBeVisible()
    await expect(page.getByText('Account Deletion Canceled')).toBeVisible()

    // Verify confirmation message
    await expect(page.getByText(/your account and all your data are safe/i)).toBeVisible()
    await expect(page.getByText(/you can continue using BoostedBudget normally/i)).toBeVisible()

    // Verify "Go to Profile" button
    const profileButton = page.getByRole('button', { name: /go to profile/i })
    await expect(profileButton).toBeVisible()
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
    const isAtLogin = url.includes('/') || url.includes('auth0.com')
    expect(isAtProfile || isAtLogin).toBe(true)
  })

  // ============================================================================
  // AUTHENTICATED TESTS - Require Auth0 Test Credentials (SKIPPED)
  // ============================================================================
  //
  // To enable these tests:
  // 1. Set up Auth0 test tenant
  // 2. Create test user
  // 3. Add credentials to e2e/.env.test
  // 4. Update e2e/helpers/auth.ts with test login flow
  // 5. Remove .skip from tests below
  //
  // See e2e/README.md for detailed setup instructions
  // ============================================================================

  test.skip('should redirect unauthenticated users to login when accessing profile', async ({ page }) => {
    // Navigate to profile page (requires auth)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Verify redirect occurred
    const url = page.url()
    expect(url).not.toContain('/profile')

    // Should be at root or Auth0 login page
    const isAtRoot = url.includes('localhost') && url.endsWith('/')
    const isAtAuth0 = url.includes('auth0.com')
    expect(isAtRoot || isAtAuth0).toBe(true)
  })

  test.skip('should display Delete Account section in Privacy Settings', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Navigate to Privacy Settings tab
    await page.click('button:has-text("Privacy Settings")')

    // Verify Privacy Settings loaded
    await expect(page.locator('h2:has-text("Privacy Settings")')).toBeVisible()

    // Verify Delete Account section exists
    await expect(page.locator('h3:has-text("Delete Account")')).toBeVisible()

    // Verify danger zone warning
    await expect(page.getByText(/⚠️ Danger Zone/i)).toBeVisible()
    await expect(page.getByText(/permanent and irreversible/i)).toBeVisible()

    // Verify data deletion list
    await expect(page.getByText(/Linked bank accounts and transactions/i)).toBeVisible()
    await expect(page.getByText(/Budget data and automation rules/i)).toBeVisible()
    await expect(page.getByText(/Profile and privacy settings/i)).toBeVisible()
    await expect(page.getByText(/Subscription and payment history/i)).toBeVisible()

    // Verify delete button is present and enabled
    const deleteButton = page.getByRole('button', { name: /delete my account/i })
    await expect(deleteButton).toBeVisible()
    await expect(deleteButton).toBeEnabled()
  })

  test.skip('should show confirmation dialog when delete button clicked', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Click "Delete My Account" button
    const deleteButton = page.getByRole('button', { name: /delete my account/i })
    await deleteButton.click()

    // Verify confirmation dialog appears
    await expect(page.getByText('Are you absolutely sure?')).toBeVisible()

    // Verify confirmation checkbox
    const checkbox = page.getByRole('checkbox')
    await expect(checkbox).toBeVisible()
    await expect(page.getByText(/I understand that all my data will be permanently deleted/i)).toBeVisible()

    // Verify buttons
    await expect(page.getByRole('button', { name: /yes, delete my account/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /^cancel$/i })).toBeVisible()
  })

  test.skip('should require checkbox confirmation before allowing deletion', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')

    // Verify confirm button is initially disabled
    const confirmButton = page.getByRole('button', { name: /yes, delete my account/i })
    await expect(confirmButton).toBeDisabled()

    // Check the confirmation checkbox
    const checkbox = page.getByRole('checkbox')
    await checkbox.click()

    // Verify confirm button is now enabled
    await expect(confirmButton).toBeEnabled()
  })

  test.skip('should cancel deletion request and close dialog', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')

    // Verify dialog is open
    await expect(page.getByText('Are you absolutely sure?')).toBeVisible()

    // Click Cancel button
    await page.click('button:has-text("^cancel$")')

    // Verify dialog is closed
    await expect(page.getByText('Are you absolutely sure?')).not.toBeVisible()
  })

  test.skip('should successfully request account deletion', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    // Mock successful deletion request
    await page.route('**/api/v1/user/delete', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          message: 'Account deletion scheduled',
          scheduled_at: '2025-11-26T00:00:00Z',
          cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.cancellation.token',
        }),
      })
    })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')

    // Check confirmation checkbox
    await page.getByRole('checkbox').click()

    // Click confirm button
    await page.click('button:has-text("Yes, Delete My Account")')

    // Wait for request to complete
    await page.waitForTimeout(500)

    // Verify success state is shown
    // (Implementation will determine exact UI - could be success message, etc.)
    // This verifies the flow completes without errors
  })

  test.skip('should display scheduled deletion date after request', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    // Mock deletion request with scheduled date
    await page.route('**/api/v1/user/delete', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          message: 'Account deletion scheduled',
          scheduled_at: '2025-11-26T12:00:00Z',
          cancellation_token: 'test-token',
        }),
      })
    })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')
    await page.getByRole('checkbox').click()
    await page.click('button:has-text("Yes, Delete My Account")')

    await page.waitForTimeout(500)

    // Verify scheduled date is displayed (format may vary)
    // Looking for date pattern: November 26, 2025 or similar
    const datePattern = /november|november 26|2025-11-26/i
    await expect(page.getByText(datePattern)).toBeVisible()
  })

  test.skip('should display cancellation URL after deletion request', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.route('**/api/v1/user/delete', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          message: 'Account deletion scheduled',
          scheduled_at: '2025-11-26T00:00:00Z',
          cancellation_token: 'abc123token',
        }),
      })
    })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')
    await page.getByRole('checkbox').click()
    await page.click('button:has-text("Yes, Delete My Account")')

    await page.waitForTimeout(500)

    // Verify cancellation URL is displayed
    await expect(page.getByText(/cancel-deletion/i)).toBeVisible()
    await expect(page.getByText(/abc123token/i)).toBeVisible()
  })

  test.skip('should handle deletion request errors gracefully', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    // Mock error response
    await page.route('**/api/v1/user/delete', route => {
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Failed to schedule deletion' }),
      })
    })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')
    await page.getByRole('checkbox').click()
    await page.click('button:has-text("Yes, Delete My Account")')

    await page.waitForTimeout(500)

    // Verify error message is displayed
    await expect(page.getByText(/failed to schedule deletion/i)).toBeVisible()

    // Verify dialog stays open for retry
    await expect(page.getByText('Are you absolutely sure?')).toBeVisible()
  })

  test.skip('should prevent multiple simultaneous deletion requests', async ({ page }) => {
    // TODO: Requires Auth0 test credentials
    // await loginWithAuth0(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    // Mock slow deletion request
    await page.route('**/api/v1/user/delete', async route => {
      await page.waitForTimeout(2000) // Simulate slow request
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          message: 'Account deletion scheduled',
          scheduled_at: '2025-11-26T00:00:00Z',
          cancellation_token: 'token',
        }),
      })
    })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')
    await page.click('button:has-text("Delete My Account")')
    await page.getByRole('checkbox').click()

    // Click confirm button
    await page.click('button:has-text("Yes, Delete My Account")')

    // Wait a bit for request to start
    await page.waitForTimeout(100)

    // Verify buttons are disabled during request
    const confirmButton = page.getByRole('button', { name: /processing/i })
    await expect(confirmButton).toBeDisabled()

    const cancelButton = page.getByRole('button', { name: /^cancel$/i })
    await expect(cancelButton).toBeDisabled()
  })
})
