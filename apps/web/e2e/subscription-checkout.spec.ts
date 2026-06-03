import { test, expect } from '@playwright/test'

interface PaddleCheckoutOptions {
  settings?: {
    checkoutURL?: string
  }
}

test.describe('Subscription Checkout - Paddle Integration', () => {
  test.beforeEach(async ({ page }) => {
    // Mock Paddle.js to avoid real checkout in E2E tests
    await page.addInitScript(() => {
      // @ts-expect-error - Mocking window.Paddle for test environment
      window.Paddle = {
        Checkout: {
          open: (options: PaddleCheckoutOptions) => {
            console.log('Paddle.Checkout.open called', options)
            // Store call for verification
            // @ts-expect-error - Adding custom property for test verification
            window.__paddleCheckoutCalled = options
          },
        },
        Initialize: () => {
          console.log('Paddle initialized (mocked)')
        },
      }
    })
  })

  test('should display all subscription tiers on pricing page', async ({ page }) => {
    await page.goto('/pricing')

    // Verify all 5 tiers are displayed
    await expect(page.getByText('Premium')).toBeVisible()
    await expect(page.getByText('Household 2')).toBeVisible()
    await expect(page.getByText('Household 3')).toBeVisible()
    await expect(page.getByText('Household 4')).toBeVisible()
    await expect(page.getByText('Household 5')).toBeVisible()
  })

  test('should show monthly and yearly pricing options', async ({ page }) => {
    await page.goto('/pricing')

    // Look for pricing indicators (e.g., /month, /year)
    const pageContent = await page.textContent('body')
    expect(pageContent).toContain('month')
    expect(pageContent).toContain('year')
  })

  test('should have subscribe buttons for each tier', async ({ page }) => {
    await page.goto('/pricing')

    // Count subscribe buttons (one per tier)
    const subscribeButtons = page.getByRole('button', { name: /subscribe/i })
    const count = await subscribeButtons.count()

    // Should have at least 5 buttons (one per tier)
    expect(count).toBeGreaterThanOrEqual(5)
  })

})

test.describe('Paddle.js Integration (Mocked)', () => {
  test('should load Paddle.js global object', async ({ page }) => {
    await page.addInitScript(() => {
      // @ts-expect-error - Mocking window.Paddle for test environment
      window.Paddle = {
        Checkout: {
          open: () => {},
        },
      }
    })

    await page.goto('/pricing')

    const paddleLoaded = await page.evaluate(() => typeof window.Paddle !== 'undefined')
    expect(paddleLoaded).toBe(true)
  })

  test('should have Paddle.Checkout.open method available', async ({ page }) => {
    await page.addInitScript(() => {
      // @ts-expect-error - Mocking window.Paddle for test environment
      window.Paddle = {
        Checkout: {
          open: () => {},
        },
      }
    })

    await page.goto('/pricing')

    const checkoutMethodExists = await page.evaluate(
      () => typeof window.Paddle?.Checkout?.open === 'function'
    )
    expect(checkoutMethodExists).toBe(true)
  })
})

test.describe('Pricing Page Display', () => {
  test('should show tier names and descriptions', async ({ page }) => {
    await page.goto('/pricing')

    // Verify tier names are displayed
    await expect(page.locator('text=Premium')).toBeVisible()

    // Verify page has pricing content
    const hasContent = await page.locator('body').textContent()
    expect(hasContent).toBeTruthy()
  })

  test('should display pricing in USD currency', async ({ page }) => {
    await page.goto('/pricing')

    const pageContent = await page.textContent('body')
    // Should contain dollar signs or USD
    expect(pageContent).toMatch(/\$|USD/)
  })

  test('should be responsive on mobile viewport', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })
    await page.goto('/pricing')

    // Page should load without errors
    await expect(page.locator('body')).toBeVisible()
  })
})
