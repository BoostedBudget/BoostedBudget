import { test, expect, Page } from '@playwright/test'

/**
 * Privacy Settings E2E Tests - Authenticated
 *
 * Tests the complete privacy management flow for logged-in users:
 * - Privacy Settings tab navigation
 * - Privacy preference toggles
 * - Backend synchronization
 * - Cookie consent integration
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
  email_preferences: {
    marketing: false,
    product_updates: true,
    security_alerts: true,
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

// SKIPPED: Issue #615 - Privacy Settings UI selectors need verification
// Tests expect specific labels (Necessary cookies, Analytics cookies, etc.)
// that may not match actual implementation.
test.describe.skip('Privacy Settings - Authenticated', () => {
  test.beforeEach(async ({ page }) => {
    await mockProfileAPIs(page)
  })

  test('should display Privacy Settings tab for authenticated users', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Verify Profile page loads
    await expect(page.locator('h1:has-text("My Profile")')).toBeVisible()

    // Click Privacy Settings tab
    const privacyTab = page.locator('button:has-text("Privacy Settings")')
    await expect(privacyTab).toBeVisible()
    await privacyTab.click()

    // Verify Privacy Settings content loads
    await expect(page.locator('h2:has-text("Privacy Settings")')).toBeVisible()
  })

  test('should load and display current privacy settings', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    // Navigate to Privacy Settings tab
    await page.click('button:has-text("Privacy Settings")')

    // Verify all privacy sections are present
    await expect(page.locator('h3:has-text("GDPR Cookie Consent")')).toBeVisible()
    await expect(page.locator('h3:has-text("Email Preferences")')).toBeVisible()
    await expect(page.locator('h3:has-text("Data Sharing")')).toBeVisible()

    // Verify all toggles are present
    await expect(page.getByLabel('Necessary cookies')).toBeVisible()
    await expect(page.getByLabel('Analytics cookies')).toBeVisible()
    await expect(page.getByLabel('Advertising cookies')).toBeVisible()
    await expect(page.getByLabel('Marketing emails')).toBeVisible()
    await expect(page.getByLabel('Product updates')).toBeVisible()
    await expect(page.getByLabel('Security alerts')).toBeVisible()
    await expect(page.getByLabel('Data sharing')).toBeVisible()

    // Verify necessary cookies checkbox is disabled
    const necessaryCheckbox = page.getByLabel('Necessary cookies')
    await expect(necessaryCheckbox).toBeDisabled()
    await expect(necessaryCheckbox).toBeChecked()
  })

  test('should toggle analytics cookie consent', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Get analytics checkbox initial state
    const analyticsCheckbox = page.getByLabel('Analytics cookies')
    const initialState = await analyticsCheckbox.isChecked()

    // Toggle analytics checkbox
    await analyticsCheckbox.click()

    // Verify state changed
    const newState = await analyticsCheckbox.isChecked()
    expect(newState).toBe(!initialState)
  })

  test('should toggle advertising cookie consent', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Get advertising checkbox initial state
    const advertisingCheckbox = page.getByLabel('Advertising cookies')
    const initialState = await advertisingCheckbox.isChecked()

    // Toggle advertising checkbox
    await advertisingCheckbox.click()

    // Verify state changed
    const newState = await advertisingCheckbox.isChecked()
    expect(newState).toBe(!initialState)
  })

  test('should toggle email preference settings', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Test marketing emails toggle
    const marketingCheckbox = page.getByLabel('Marketing emails')
    const marketingInitial = await marketingCheckbox.isChecked()
    await marketingCheckbox.click()
    expect(await marketingCheckbox.isChecked()).toBe(!marketingInitial)

    // Test product updates toggle
    const updatesCheckbox = page.getByLabel('Product updates')
    const updatesInitial = await updatesCheckbox.isChecked()
    await updatesCheckbox.click()
    expect(await updatesCheckbox.isChecked()).toBe(!updatesInitial)
  })

  test('should toggle data sharing preference', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Toggle data sharing
    const dataSharingCheckbox = page.getByLabel('Data sharing')
    const initialState = await dataSharingCheckbox.isChecked()
    await dataSharingCheckbox.click()
    expect(await dataSharingCheckbox.isChecked()).toBe(!initialState)
  })

  test('should save privacy settings and show success message', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Make a change
    await page.getByLabel('Analytics cookies').click()

    // Click Save button
    await page.click('button:has-text("Save Privacy Settings")')

    // Wait for success message
    await page.waitForTimeout(500)
    await expect(page.getByText(/settings saved successfully/i)).toBeVisible()
  })

  test('should persist privacy settings after page reload', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Change analytics setting
    const analyticsCheckbox = page.getByLabel('Analytics cookies')
    await analyticsCheckbox.click()
    const newState = await analyticsCheckbox.isChecked()

    // Save changes
    await page.click('button:has-text("Save Privacy Settings")')
    await page.waitForTimeout(500)

    // Reload page
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Navigate back to Privacy Settings
    await page.click('button:has-text("Privacy Settings")')

    // Verify setting persisted
    const analyticsAfterReload = page.getByLabel('Analytics cookies')
    await expect(analyticsAfterReload).toBeChecked({ checked: newState })
  })

  // SKIPPED: Issue #615 - Cookie consent sync may not be wired
  test.skip('should update cookie consent when Privacy Settings saved', async ({ page }) => {
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Change analytics consent
    await page.getByLabel('Analytics cookies').click()

    // Save settings
    await page.click('button:has-text("Save Privacy Settings")')
    await page.waitForTimeout(500)

    // Check that cookie was updated
    const cookies = await page.context().cookies()
    const consentCookie = cookies.find(c => c.name === 'boostedbudget_cookie_consent')

    expect(consentCookie).toBeDefined()

    // Verify cookie has consents data
    const cookieData = JSON.parse(consentCookie!.value)
    expect(cookieData).toHaveProperty('consents')
    expect(cookieData).toHaveProperty('timestamp')
    expect(cookieData).toHaveProperty('version')
  })

  // SKIPPED: Issue #615 - Cookie consent sync may not be wired
  test.skip('should sync Cookie Consent banner with Privacy Settings', async ({ page }) => {
    // First, accept all cookies via banner
    await page.goto('/')
    await page.click('button:has-text("Accept All")')

    // Navigate to Privacy Settings
    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Verify analytics is checked (from Accept All)
    await expect(page.getByLabel('Analytics cookies')).toBeChecked()
    await expect(page.getByLabel('Advertising cookies')).toBeChecked()

    // Change analytics to false in Privacy Settings
    await page.getByLabel('Analytics cookies').click()
    await page.click('button:has-text("Save Privacy Settings")')
    await page.waitForTimeout(500)

    // Verify cookie was updated
    const cookies = await page.context().cookies()
    const consentCookie = cookies.find(c => c.name === 'boostedbudget_cookie_consent')
    const cookieData = JSON.parse(consentCookie!.value)

    expect(cookieData.consents.analytics).toBe(false)
    expect(cookieData.consents.advertising).toBe(true) // Should still be true
  })

  test('should handle save errors gracefully', async ({ page }) => {
    // Intercept API call to force error
    await page.route('**/api/v1/user/privacy', route => route.abort())

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Make a change
    await page.getByLabel('Analytics cookies').click()

    // Try to save
    await page.click('button:has-text("Save Privacy Settings")')

    // Should show error message
    await page.waitForTimeout(500)
    await expect(page.getByText(/failed to save/i)).toBeVisible()
  })
})
