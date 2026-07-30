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

    // Verify all 5 tiers are displayed. Display names come from
    // config/subscription_tiers.yaml: "Household (N)" (household_5 is "Household (5+)"),
    // parenthesized — a bare "Household 2" substring never matches that text.
    await expect(page.getByRole('heading', { name: 'Premium' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Household (2)' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Household (3)' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Household (4)' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Household (5+)' })).toBeVisible()
  })

  test('should show monthly and yearly pricing options', async ({ page }) => {
    await page.goto('/pricing')
    // Tier cards render only after the async tiers fetch resolves. page.textContent() (unlike
    // expect(locator).toBeVisible()) does not auto-retry, so it was racing the fetch and
    // reading the pre-render DOM — anchor on a real heading first.
    await expect(page.getByRole('heading', { name: 'Choose Your Plan' })).toBeVisible()

    // Assert on the actual toggle UI rather than a raw case-sensitive body-text substring.
    // The original `toContain('month')` / `toContain('year')` pair only ever passed by
    // accident: the default (monthly) view's price suffix literally renders lowercase
    // "/month", but its "/year" counterpart only renders after switching to the yearly view
    // — and the always-visible toggle button reads "Yearly" (capital Y), which a lowercase
    // 'year' substring check can never match. Querying the two radio options directly tests
    // the thing the test name actually promises: both billing periods are selectable.
    await expect(page.getByRole('radio', { name: 'Monthly' })).toBeVisible()
    await expect(page.getByRole('radio', { name: /yearly/i })).toBeVisible()
  })

  test('should have subscribe buttons for each tier', async ({ page }) => {
    await page.goto('/pricing')
    // Same race as above: wait for the tier grid to render before counting buttons —
    // Locator.count() does not retry the way expect(locator).toBeVisible() does.
    await expect(page.getByRole('heading', { name: 'Choose Your Plan' })).toBeVisible()

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

    // Verify tier names are displayed. Scoped to the heading role: a bare 'text=Premium'
    // locator is a strict-mode violation here — "Premium" also appears in every other
    // tier's description ("Premium features for N family members") and in FAQ copy.
    await expect(page.getByRole('heading', { name: 'Premium' })).toBeVisible()

    // Verify page has pricing content
    const hasContent = await page.locator('body').textContent()
    expect(hasContent).toBeTruthy()
  })

  test('should display pricing in USD currency', async ({ page }) => {
    await page.goto('/pricing')
    // Same fetch-race as the other raw-textContent tests above — anchor on a real heading.
    await expect(page.getByRole('heading', { name: 'Choose Your Plan' })).toBeVisible()

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
