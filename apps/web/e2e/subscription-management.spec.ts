import { test, expect } from '@playwright/test'

/**
 * E2E Tests for Subscription Management (Issue #768)
 *
 * Tests the subscription management flow for different user states:
 * - Trial users: Shows trial banner, days remaining, subscribe button
 * - Active users: Shows subscription details, billing portal, cancel button
 * - Handles auth failures gracefully
 *
 * Issue #1054: cancel-modal and active-subscription tests marked test.fixme
 * pending a real Paddle test subscription fixture.
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
    await expect(page).toHaveTitle(/BoostedBudget/)
    await expect(page.getByRole('heading', { name: /subscription management/i })).toBeVisible()
    await expect(page.getByText(/current subscription/i).first()).toBeVisible()
  })

  test('displays trial banner for trial users', async ({ page }) => {
    await page.waitForLoadState('networkidle')

    const trialBanner = page.locator('.trial-banner')
    await expect(trialBanner).toBeVisible()
    await expect(trialBanner).toContainText(/days left in your free trial/i)

    await expect(page.getByRole('link', { name: /subscribe to premium/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /cancel subscription/i })).not.toBeVisible()
  })

  test('displays subscription details', async ({ page }) => {
    await page.waitForLoadState('networkidle')

    const detailsSection = page.locator('.subscription-details')
    await expect(detailsSection).toBeVisible()

    await expect(detailsSection.getByText(/tier/i)).toBeVisible()
    await expect(detailsSection.getByText(/status/i)).toBeVisible()
    await expect(detailsSection.getByText(/amount/i)).toBeVisible()
  })

  test('shows appropriate actions for active subscriptions', async ({ page }) => {
    test.fixme(true, 'Requires Paddle test fixture — see #1054')
    await page.waitForLoadState('networkidle')

    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    await expect(cancelButton).toBeVisible()

    const portalLink = page.getByRole('link', { name: /manage payment methods/i })
    await expect(portalLink).toHaveAttribute('target', '_blank')
  })

  test('cancel subscription opens confirmation modal', async ({ page }) => {
    test.fixme(true, 'Requires Paddle test fixture — see #1054')
    await page.waitForLoadState('networkidle')

    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    await cancelButton.click()

    const modal = page.locator('.modal-overlay')
    await expect(modal).toBeVisible()
    await expect(modal).toContainText(/cancel subscription/i)
    await expect(modal).toContainText(/are you sure/i)

    const yesButton = modal.getByRole('button', { name: /yes.*cancel/i })
    const keepButton = modal.getByRole('button', { name: /keep subscription/i })
    await expect(yesButton).toBeVisible()
    await expect(keepButton).toBeVisible()

    await keepButton.click()
    await expect(modal).not.toBeVisible()
  })

  test('close cancel modal by clicking overlay', async ({ page }) => {
    test.fixme(true, 'Requires Paddle test fixture — see #1054')
    await page.waitForLoadState('networkidle')

    const cancelButton = page.getByRole('button', { name: /cancel subscription/i })
    await cancelButton.click()

    const modalOverlay = page.locator('.modal-overlay')
    await expect(modalOverlay).toBeVisible()

    await modalOverlay.click({ position: { x: 10, y: 10 } })
    await expect(modalOverlay).not.toBeVisible()
  })

  test('displays error state gracefully', async ({ page }) => {
    await page.goto('/billing/manage')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('heading', { name: /subscription management/i })).toBeVisible()
  })

  test('trial users can navigate to pricing page', async ({ page }) => {
    test.fixme(true, 'Requires Paddle test fixture — see #1054')
    await page.waitForLoadState('networkidle')

    const subscribeButton = page.getByRole('link', { name: /subscribe to premium/i })
    const href = await subscribeButton.getAttribute('href')
    expect(href).toContain('/pricing')
  })

  test('displays correct billing date label for user state', async ({ page }) => {
    await page.waitForLoadState('networkidle')

    const detailsSection = page.locator('.subscription-details')
    await expect(detailsSection).toBeVisible()

    await expect(detailsSection.getByText(/trial ends/i)).toBeVisible()
  })
})
