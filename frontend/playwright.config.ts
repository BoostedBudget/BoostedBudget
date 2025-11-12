import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright E2E Test Configuration
 * See https://playwright.dev/docs/test-configuration
 *
 * Dynamic Port Allocation:
 * - E2E_FRONTEND_PORT: Override frontend port (default: 3000)
 * - E2E_BACKEND_PORT: Override backend port (default: 8080)
 * - Use scripts/find-available-ports.js in CI to avoid port conflicts
 */

// Get dynamic ports from environment, fallback to defaults
const frontendPort = process.env.E2E_FRONTEND_PORT || '3000';
const backendPort = process.env.E2E_BACKEND_PORT || '8080';

export default defineConfig({
  // Test directory
  testDir: './e2e',

  // Maximum time one test can run
  timeout: 30 * 1000,

  // Run tests in files in parallel
  fullyParallel: true,

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,

  // Retry on CI only
  retries: process.env.CI ? 2 : 0,

  // Parallel workers:
  // - CI: 1 worker for traditional E2E tests
  // - Local: 2 workers to prevent memory exhaustion
  // See: Memory leak investigation Oct 2025 - saves 1.5GB vs unlimited workers
  workers: process.env.CI ? 1 : 2,

  // Reporter to use
  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'playwright-report/results.json' }],
    ['list']
  ],

  // Shared settings for all the projects below
  use: {
    // Base URL to use in actions like `await page.goto('/')`
    baseURL: process.env.PLAYWRIGHT_BASE_URL || `http://localhost:${frontendPort}`,

    // Backend API URL
    apiURL: process.env.PLAYWRIGHT_API_URL || `http://localhost:${backendPort}`,

    // Collect trace when retrying the failed test
    // Disable locally to save memory (video buffering ~200MB), enable in CI for debugging
    trace: process.env.CI ? 'on-first-retry' : 'off',

    // Screenshot on failure
    // Disable locally to save memory, enable in CI for debugging
    screenshot: process.env.CI ? 'only-on-failure' : 'off',

    // Video on failure
    // Disable locally to save memory, enable in CI for debugging
    video: process.env.CI ? 'retain-on-failure' : 'off',
  },

  // Environment variables passed to test files
  env: {
    ADMIN_API_TOKEN: process.env.ADMIN_API_TOKEN || '',
  },

  // Configure projects for major browsers
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Run your local dev server before starting the tests
  // Comment this out if you want to run tests against already running servers
  webServer: [
    {
      // Start frontend dev server
      // Pass PORT and E2E_BACKEND_PORT env vars to Vite for dynamic port allocation
      // E2E_BACKEND_PORT is needed for the Vite proxy configuration
      // E2E_DISABLE_HTTPS disables HTTPS in CI environments with self-signed certs
      command: `PORT=${frontendPort} E2E_BACKEND_PORT=${backendPort} E2E_DISABLE_HTTPS=${process.env.E2E_DISABLE_HTTPS || 'false'} npm run dev`,
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
      ignoreHTTPSErrors: true,
    },
    {
      // Start backend server
      // Pass PORT env var to backend for dynamic port allocation
      command: `cd ../backend && PORT=${backendPort} ./bin/server`,
      url: `http://localhost:${backendPort}/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
      ignoreHTTPSErrors: true,
    },
  ],
});
