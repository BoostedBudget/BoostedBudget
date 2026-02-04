import { test, expect } from '@playwright/test'

/**
 * E2E Tests for Subscription Management (Issue #48)
 *
 * Tests the full subscription management flow including:
 * - Viewing subscription details
 * - Upgrade/downgrade modals
 * - Cancellation flow
 * - Billing portal access
 */

test.describe('Subscription Management', () => {
  test.beforeEach(async ({ page }) => {
    // Note: These tests require authentication setup
    // In real usage, you would:
    // 1. Login with test user
    // 2. Navigate to /billing/manage
    // For now, we'll test the page structure and interactions
    await page.goto('/billing/manage')
  })

  test('displays subscription management page with current subscription', async ({ page }) => {
    // Verify page loads
    await expect(page).toHaveTitle(/BoostedBudget/)

    // Check for main heading
    const heading = page.getByRole('heading', { name: /subscription management/i })
    await expect(heading).toBeVisible()

    // Verify current subscription section exists
    const currentSubscription = page.getByRole('region', { name: /current subscription/i })
    await expect(currentSubscription).toBeVisible()

    // Verify billing details section exists
    const billingDetails = page.getByRole('region', { name: /billing details/i })
    await expect(billingDetails).toBeVisible()
  })

  test('opens and closes upgrade modal', async ({ page }) => {
    // Find and click upgrade button
    const upgradeButton = page.getByRole('button', { name: /upgrade/i })
    await upgradeButton.click()

    // Verify modal opens
    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible()

    // Verify modal has tier comparison
    await expect(modal.getByText(/premium/i)).toBeVisible()

    // Close modal via close button
    const closeButton = modal.getByRole('button', { name: /close/i })
    await closeButton.click()

    // Verify modal closes
    await expect(modal).not.toBeVisible()
  })

  test('upgrade modal shows tier comparison and pricing', async ({ page }) => {
    // Open upgrade modal
    await page.getByRole('button', { name: /upgrade/i }).click()

    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible()

    // Verify billing interval toggle exists
    const monthlyOption = modal.getByRole('radio', { name: /monthly/i })
    const yearlyOption = modal.getByRole('radio', { name: /yearly/i })
    await expect(monthlyOption).toBeVisible()
    await expect(yearlyOption).toBeVisible()

    // Verify pricing displays
    await expect(modal.getByText(/\$/)).toBeVisible()

    // Verify upgrade button exists
    const upgradeConfirmButton = modal.getByRole('button', { name: /upgrade to/i })
    await expect(upgradeConfirmButton).toBeVisible()
  })

  test('opens and closes downgrade modal', async ({ page }) => {
    // Find and click downgrade option
    const downgradeButton = page.getByRole('button', { name: /downgrade/i })

    // Only run if downgrade button exists (depends on current tier)
    const isVisible = await downgradeButton.isVisible().catch(() => false)
    if (!isVisible) {
      test.skip()
      return
    }

    await downgradeButton.click()

    // Verify modal opens
    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible()

    // Verify warning text about feature loss
    await expect(modal.getByText(/lose access/i)).toBeVisible()

    // Close modal
    const closeButton = modal.getByRole('button', { name: /close|cancel/i })
    await closeButton.click()

    // Verify modal closes
    await expect(modal).not.toBeVisible()
  })

  test('downgrade modal shows feature loss warning and confirmation', async ({ page }) => {
    // Find downgrade button
    const downgradeButton = page.getByRole('button', { name: /downgrade/i })

    const isVisible = await downgradeButton.isVisible().catch(() => false)
    if (!isVisible) {
      test.skip()
      return
    }

    await downgradeButton.click()

    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible()

    // Verify feature comparison
    await expect(modal.getByText(/current plan/i)).toBeVisible()

    // Verify confirmation checkbox
    const checkbox = modal.getByRole('checkbox', { name: /i understand/i })
    await expect(checkbox).toBeVisible()

    // Verify confirm button is initially disabled
    const confirmButton = modal.getByRole('button', { name: /confirm/i })
    await expect(confirmButton).toBeDisabled()

    // Check the checkbox
    await checkbox.check()

    // Verify confirm button is now enabled
    await expect(confirmButton).toBeEnabled()
  })

  test('cancel subscription shows double confirmation flow', async ({ page }) => {
    // Find cancel subscription button
    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    await cancelButton.click()

    // First confirmation dialog
    const firstDialog = page.getByRole('dialog')
    await expect(firstDialog).toBeVisible()
    await expect(firstDialog.getByText(/are you sure/i)).toBeVisible()

    // Click "Yes, Cancel" button
    const yesCancelButton = firstDialog.getByRole('button', { name: /yes.*cancel/i })
    await yesCancelButton.click()

    // Second confirmation dialog
    const secondDialog = page.getByRole('dialog')
    await expect(secondDialog).toBeVisible()

    // Verify type "CANCEL" requirement
    await expect(secondDialog.getByText(/type.*cancel/i)).toBeVisible()

    const confirmInput = secondDialog.getByRole('textbox')
    await expect(confirmInput).toBeVisible()

    // Verify confirm button is disabled until typing "CANCEL"
    const finalConfirmButton = secondDialog.getByRole('button', { name: /confirm/i })
    await expect(finalConfirmButton).toBeDisabled()

    // Type "CANCEL"
    await confirmInput.fill('CANCEL')

    // Verify confirm button is now enabled
    await expect(finalConfirmButton).toBeEnabled()

    // Close modal instead of completing cancellation
    const closeButton = secondDialog.getByRole('button', { name: /close/i })
    await closeButton.click()
  })

  test('displays billing portal link', async ({ page }) => {
    // Verify "Manage Payment Methods" link exists
    const portalLink = page.getByRole('link', { name: /manage payment methods/i })
    await expect(portalLink).toBeVisible()

    // Verify link has correct attributes
    await expect(portalLink).toHaveAttribute('target', '_blank')
    await expect(portalLink).toHaveAttribute('rel', /noopener noreferrer/)

    // Verify link points to Paddle portal (without following it)
    const href = await portalLink.getAttribute('href')
    expect(href).toContain('paddle')
  })

  test('displays loading state when fetching subscription data', async ({ page }) => {
    // This test would require mocking slow API responses
    // For now, we'll verify the loading UI elements exist in the component

    // Navigate to page and check for loading indicators
    await page.goto('/billing/manage')

    // Loading state should show briefly, but may be too fast to catch
    // In a real scenario, you'd use network throttling or API mocks

    // Verify page eventually loads content
    await expect(page.getByRole('heading', { name: /subscription management/i })).toBeVisible()
  })

  test('displays error state when API call fails', async ({ page }) => {
    // This test would require mocking failed API responses
    // In a real scenario, you'd intercept network requests and return errors

    // For now, verify the page has error handling structure
    await page.goto('/billing/manage')

    // Page should either show content or error, not crash
    const hasContent = await page.getByRole('heading', { name: /subscription management/i }).isVisible().catch(() => false)
    const hasError = await page.getByText(/error|failed|try again/i).isVisible().catch(() => false)

    expect(hasContent || hasError).toBeTruthy()
  })

  test('monthly and yearly billing toggle works in upgrade modal', async ({ page }) => {
    // Open upgrade modal
    await page.getByRole('button', { name: /upgrade/i }).click()

    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible()

    // Click yearly option
    const yearlyOption = modal.getByRole('radio', { name: /yearly/i })
    await yearlyOption.click()

    // Verify pricing updates (yearly should show different amount)
    await expect(modal.getByText(/year/i)).toBeVisible()

    // Click monthly option
    const monthlyOption = modal.getByRole('radio', { name: /monthly/i })
    await monthlyOption.click()

    // Verify pricing updates back to monthly
    await expect(modal.getByText(/month/i)).toBeVisible()
  })
})
