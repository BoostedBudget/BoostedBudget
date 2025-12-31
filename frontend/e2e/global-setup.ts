import { clerkSetup } from '@clerk/testing/playwright'
import { test as setup } from '@playwright/test'

// Setup must be run serially - necessary when Playwright is configured with fullyParallel: true
// See: https://playwright.dev/docs/test-parallel
setup.describe.configure({ mode: 'serial' })

/**
 * Global setup for Clerk testing.
 *
 * The clerkSetup() function obtains a Testing Token when the test suite starts,
 * making it available for all subsequent tests. This token allows tests to bypass
 * Clerk's bot detection mechanisms.
 *
 * Required environment variables:
 * - CLERK_PUBLISHABLE_KEY: Your Clerk publishable key
 * - CLERK_SECRET_KEY: Your Clerk secret key (for obtaining testing tokens)
 */
setup('global clerk setup', async ({}) => {
  await clerkSetup()
})
