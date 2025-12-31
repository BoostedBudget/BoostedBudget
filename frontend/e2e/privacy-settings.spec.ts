import { test, expect } from '@playwright/test'

/**
 * Privacy Settings E2E Tests
 *
 * Tests the complete privacy management flow including:
 * - Privacy Settings tab navigation
 * - Privacy preference toggles
 * - Backend synchronization
 * - Cookie consent integration
 *
 * NOTE: Most tests require authentication with Clerk, which is currently
 * skipped pending test credentials. See e2e/README.md for setup instructions.
 */

test.describe('Privacy Settings', () => {
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
    await page.waitForLoadState('networkidle')

    // Verify Privacy Policy page loads
    await expect(page.locator('h1:has-text("Privacy Policy")')).toBeVisible()

    // Verify key GDPR sections present
    await expect(page.locator('h2:has-text("Information We Collect")')).toBeVisible()
    await expect(page.locator('h2:has-text("Your Privacy Rights")')).toBeVisible()
    await expect(page.locator('h2:has-text("Cookie Policy")')).toBeVisible()

    // Verify US region mentioned (get first match to avoid strict mode violation)
    await expect(page.getByText(/us-east-1|Virginia, USA/i).first()).toBeVisible()
  })

  // ============================================================================
  // SKIPPED TESTS - Require Clerk Test Credentials
  // ============================================================================
  //
  // To enable these tests:
  // 1. Set up Clerk test instance
  // 2. Create test user
  // 3. Add credentials to e2e/.env.test:
  //    TEST_CLERK_EMAIL=test-user@example.com
  //    TEST_CLERK_PASSWORD=TestPassword123!
  // 4. Update e2e/helpers/auth.ts with test login flow
  // 5. Remove .skip from tests below
  //
  // See e2e/README.md for detailed setup instructions
  // ============================================================================

  test.skip('should display Privacy Settings tab for authenticated users', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should load and display current privacy settings', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should toggle analytics cookie consent', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should toggle advertising cookie consent', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should toggle email preference settings', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should toggle data sharing preference', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    await page.goto('/profile')
    await page.click('button:has-text("Privacy Settings")')

    // Toggle data sharing
    const dataSharingCheckbox = page.getByLabel('Data sharing')
    const initialState = await dataSharingCheckbox.isChecked()
    await dataSharingCheckbox.click()
    expect(await dataSharingCheckbox.isChecked()).toBe(!initialState)
  })

  test.skip('should save privacy settings and show success message', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should persist privacy settings after page reload', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should update cookie consent when Privacy Settings saved', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should sync Cookie Consent banner with Privacy Settings', async ({ page }) => {
    // TODO: Requires Clerk test credentials
    // This test verifies bi-directional sync:
    // 1. Change cookie consent → Privacy Settings update
    // 2. Change Privacy Settings → cookie consent update

    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

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

  test.skip('should handle save errors gracefully', async ({ page }) => {
    // TODO: Requires Clerk test credentials + network interception
    // This test would intercept the API call and force an error response

    // await loginWithClerk(page, { email: TEST_EMAIL, password: TEST_PASSWORD })

    // Intercept API call to force error
    // await page.route('**/api/v1/user/privacy', route => route.abort())

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
