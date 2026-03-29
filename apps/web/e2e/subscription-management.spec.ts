import { test, expect } from '@playwright/test'

/**
 * E2E Tests for Subscription Management (Issue #768)
 *
 * Tests the subscription management flow for different user states:
 * - Trial users: Shows trial banner, days remaining, subscribe button
 * - Active users: Shows subscription details, billing portal, cancel button
 * - Handles auth failures gracefully
 */

test.describe('Subscription Management', () => {
  test.beforeEach(async ({ page }) => {
    // Note: These tests require authentication setup
    // In real usage, you would:
    // 1. Login with test user
    // 2. Navigate to /billing/manage
    await page.goto('/billing/manage')
  })

  test('displays subscription management page structure', async ({ page }) => {
    // Verify page loads
    await expect(page).toHaveTitle(/BoostedBudget/)

    // Check for main heading
    const heading = page.getByRole('heading', { name: /subscription management/i })
    await expect(heading).toBeVisible()

    // Verify current subscription section exists
    const currentSubscription = page.getByText(/current subscription/i).first()
    await expect(currentSubscription).toBeVisible()
  })

  test('displays trial banner for trial users', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    // Check if trial banner exists (only for trial users)
    const trialBanner = page.locator('.trial-banner')
    const isVisible = await trialBanner.isVisible().catch(() => false)

    if (isVisible) {
      // If trial banner exists, verify it shows days remaining
      await expect(trialBanner).toContainText(/days left in your free trial/i)

      // Trial users should see "Subscribe to Premium" button
      const subscribeButton = page.getByRole('link', { name: /subscribe to premium/i })
      await expect(subscribeButton).toBeVisible()

      // Trial users should NOT see cancel button
      const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
      await expect(cancelButton).not.toBeVisible()
    }
  })

  test('displays subscription details', async ({ page }) => {
    // Wait for content to load
    await page.waitForLoadState('networkidle')

    // Verify subscription details are shown
    const detailsSection = page.locator('.subscription-details')
    await expect(detailsSection).toBeVisible()

    // Verify key fields exist
    await expect(detailsSection.getByText(/tier/i)).toBeVisible()
    await expect(detailsSection.getByText(/status/i)).toBeVisible()
    await expect(detailsSection.getByText(/amount/i)).toBeVisible()
  })

  test('shows appropriate actions for active subscriptions', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    // Check if this is an active subscription (not trial)
    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    const isCancelVisible = await cancelButton.isVisible().catch(() => false)

    if (isCancelVisible) {
      // Active users should have access to:
      // 1. Cancel button
      await expect(cancelButton).toBeVisible()

      // 2. Billing portal link (may not be visible for all states)
      const portalLink = page.getByRole('link', { name: /manage payment methods/i })
      const isPortalVisible = await portalLink.isVisible().catch(() => false)
      if (isPortalVisible) {
        await expect(portalLink).toHaveAttribute('target', '_blank')
      }
    }
  })

  test('cancel subscription opens confirmation modal', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    // Find cancel subscription button (only for active users)
    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    const isVisible = await cancelButton.isVisible().catch(() => false)

    if (!isVisible) {
      test.skip()
      return
    }

    await cancelButton.click()

    // Verify modal appears
    const modal = page.locator('.modal-overlay')
    await expect(modal).toBeVisible()

    // Verify modal content
    await expect(modal).toContainText(/cancel subscription/i)
    await expect(modal).toContainText(/are you sure/i)

    // Verify action buttons
    const yesButton = modal.getByRole('button', { name: /yes.*cancel/i })
    const keepButton = modal.getByRole('button', { name: /keep subscription/i })
    await expect(yesButton).toBeVisible()
    await expect(keepButton).toBeVisible()

    // Close modal by clicking keep button
    await keepButton.click()

    // Verify modal closes
    await expect(modal).not.toBeVisible()
  })

  test('close cancel modal by clicking overlay', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    const isVisible = await cancelButton.isVisible().catch(() => false)

    if (!isVisible) {
      test.skip()
      return
    }

    await cancelButton.click()

    // Wait for modal to appear
    const modalOverlay = page.locator('.modal-overlay')
    await expect(modalOverlay).toBeVisible()

    // Click overlay (outside modal)
    await modalOverlay.click({ position: { x: 10, y: 10 } })

    // Verify modal closes
    await expect(modalOverlay).not.toBeVisible()
  })

  test('displays error state gracefully', async ({ page }) => {
    // Page should handle errors without crashing
    await page.goto('/billing/manage')

    // Wait for page to finish loading
    await page.waitForLoadState('networkidle')

    // Page should either show content or error message, not a blank screen
    const hasHeading = await page.getByRole('heading', { name: /subscription management/i }).isVisible().catch(() => false)
    const hasError = await page.getByText(/failed to load/i).isVisible().catch(() => false)
    const hasLoading = await page.getByText(/loading/i).isVisible().catch(() => false)

    // At least one of these should be true
    expect(hasHeading || hasError || hasLoading).toBeTruthy()
  })

  test('trial users can navigate to pricing page', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    // Check if subscribe button exists (for trial users)
    const subscribeButton = page.getByRole('link', { name: /subscribe to premium/i })
    const isVisible = await subscribeButton.isVisible().catch(() => false)

    if (!isVisible) {
      test.skip()
      return
    }

    // Verify button links to pricing page
    const href = await subscribeButton.getAttribute('href')
    expect(href).toContain('/pricing')
  })

  test('displays correct billing date label for user state', async ({ page }) => {
    // Wait for page to load
    await page.waitForLoadState('networkidle')

    const detailsSection = page.locator('.subscription-details')
    await expect(detailsSection).toBeVisible()

    // Check for either "Trial Ends" or "Next Billing Date"
    const hasTrialEnds = await detailsSection.getByText(/trial ends/i).isVisible().catch(() => false)
    const hasNextBilling = await detailsSection.getByText(/next billing date/i).isVisible().catch(() => false)

    // One of these should be present
    expect(hasTrialEnds || hasNextBilling).toBeTruthy()
  })
})
