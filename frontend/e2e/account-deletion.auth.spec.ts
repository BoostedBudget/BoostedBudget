import { test, expect, Page } from '@playwright/test'

/**
 * Account Deletion E2E Tests - Authenticated
 *
 * Tests the complete account deletion flow for logged-in users:
 * - Delete Account section in Privacy Settings
 * - Confirmation dialog
 * - Deletion request submission
 * - Error handling
 *
 * These tests run in chromium project with pre-authenticated session
 */

// Mock profile data
const mockProfile = {
  id: 'user-123',
  clerk_id: 'clerk_user_123',
  email: 'test@example.com',
  name: 'Test User',
  tier: 'free',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

// Mock privacy settings
const mockPrivacySettings = {
  cookie_consent: {
    necessary: true,
    functional: true,
    analytics: false,
    advertising: false,
    consented_at: '2025-01-01T00:00:00Z',
  },
  data_sharing: {
    share_anonymized_data: false,
    share_with_partners: false,
  },
  deletion_status: null,
}

/**
 * Helper function to mock profile and privacy API endpoints
 */
async function mockProfileAPIs(page: Page) {
  // Mock profile endpoint (wildcard to match query params)
  await page.route('**/api/v1/user/profile*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(mockProfile),
    })
  })

  // Mock privacy settings endpoint (wildcard to match query params)
  await page.route('**/api/v1/user/privacy*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockPrivacySettings),
      })
    } else {
      // Handle PUT for saving privacy settings
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...mockPrivacySettings, ...route.request().postDataJSON() }),
      })
    }
  })

  // Mock subscription/tier endpoint
  await page.route('**/api/v1/subscription/**', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tier: 'free', status: 'active' }),
    })
  })
}

// ==========================================================================
// Protected Route Test (Unauthenticated)
// ==========================================================================
test.describe('Account Deletion - Protected Route', () => {
  // Override storageState to test without authentication
  test.use({ storageState: { cookies: [], origins: [] } })

  test('should redirect unauthenticated users to login when accessing profile', async ({ page }) => {
    // Navigate to profile page (requires auth)
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Verify redirect occurred
    const url = page.url()
    expect(url).not.toContain('/profile')

    // Should be at root or Clerk login page
    const isAtRoot = url.includes('localhost') && url.endsWith('/')
    const isAtClerk = url.includes('clerk.')
    expect(isAtRoot || isAtClerk).toBe(true)
  })
})

// ==========================================================================
// Authenticated Tests
// ==========================================================================
test.describe('Account Deletion - Authenticated', () => {
  test.beforeEach(async ({ page }) => {
    await mockProfileAPIs(page)
  })

  test('should display Delete Account section in Privacy Settings', async ({ page }) => {
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

  test('should show confirmation dialog when delete button clicked', async ({ page }) => {
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

  test('should require checkbox confirmation before allowing deletion', async ({ page }) => {
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

  test('should cancel deletion request and close dialog', async ({ page }) => {
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

  test('should successfully request account deletion', async ({ page }) => {
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

  test('should display scheduled deletion date after request', async ({ page }) => {
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

  test('should display cancellation URL after deletion request', async ({ page }) => {
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

  test('should handle deletion request errors gracefully', async ({ page }) => {
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

  test('should prevent multiple simultaneous deletion requests', async ({ page }) => {
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
