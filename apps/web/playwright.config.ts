import { defineConfig, devices } from '@playwright/test';
import { existsSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

// ES module equivalent of __dirname
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Ensure VITE_CLERK_PUBLISHABLE_KEY is available for test files
// Test files check this env var at module load time for conditional skip logic
// Without this, authenticated test suites skip even when credentials are available
if (!process.env.VITE_CLERK_PUBLISHABLE_KEY && process.env.CLERK_PUBLISHABLE_KEY) {
  process.env.VITE_CLERK_PUBLISHABLE_KEY = process.env.CLERK_PUBLISHABLE_KEY;
}

/**
 * Playwright E2E Test Configuration
 * See https://playwright.dev/docs/test-configuration
 *
 * Dynamic Port Allocation:
 * - E2E_FRONTEND_PORT: Override frontend port (default: 3000)
 * - E2E_BACKEND_PORT: Override backend port (default: 8080)
 * - E2E_POSTGRES_PORT: Override PostgreSQL port (dynamic, set by run-e2e-with-ports.sh)
 * - DATABASE_URL: PostgreSQL connection string (set by run-e2e-with-ports.sh)
 * - Use scripts/find-available-ports.js in CI to avoid port conflicts
 *
 * Authentication:
 * - TEST_CLERK_EMAIL: Email of Clerk test user (for authenticated tests)
 * - TEST_CLERK_PASSWORD: Password for test user
 * - Auth session saved to e2e/.auth/user.json, reused by chromium project
 */

// Get dynamic ports and database URL from environment, fallback to defaults
const frontendPort = process.env.E2E_FRONTEND_PORT || '3000';
const backendPort = process.env.E2E_BACKEND_PORT || '8080';
const databaseUrl = process.env.DATABASE_URL || '';

// Auth session file path
const authFile = join(__dirname, 'e2e/.auth/user.json');

export default defineConfig({
  // Test directory
  testDir: './e2e',

  // Maximum time one test can run
  // CI runs on a shared Mac Mini (4 runners) with heavy job contention — 30s gives headroom
  timeout: process.env.CI ? 30 * 1000 : 15 * 1000,

  // Global assertion timeout (default is 5s — too tight under CI resource contention)
  // Raises all expect() calls to 10s so below-fold content and slow renders don't time out
  expect: {
    timeout: 10 * 1000,
  },

  // Run tests in files in parallel
  fullyParallel: true,

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,

  // Retry on CI only
  retries: process.env.CI ? 2 : 0,

  // Stop after 5 failures in CI — prevents 12+ min of timeout waste when PostgreSQL dies
  maxFailures: process.env.CI ? 5 : undefined,

  // Opt out of parallel tests on CI
  // Limit to 2 workers locally to prevent memory exhaustion (was 8 on 8-core system)
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

  // Configure projects for major browsers
  projects: [
    // Clerk setup project - must run first to obtain testing token
    {
      name: 'setup',
      testMatch: /global-setup\.ts/,
    },
    // Authentication setup - logs in once and saves session state
    // Requires TEST_CLERK_EMAIL and TEST_CLERK_PASSWORD env vars
    // Session is saved to e2e/.auth/user.json and reused by chromium project
    {
      name: 'auth',
      testMatch: /auth\.setup\.ts/,
      dependencies: ['setup'],
    },
    // Main test project - uses saved authentication state if available
    // Only runs *.auth.spec.ts files (authenticated tests)
    {
      name: 'chromium',
      testMatch: /\.auth\.spec\.ts$/,
      use: {
        ...devices['Desktop Chrome'],
        // Use saved auth state from auth.setup.ts if it exists
        // When auth setup is skipped (no TEST_CLERK_* env vars), tests run unauthenticated
        ...(existsSync(authFile) ? { storageState: authFile } : {}),
      },
      dependencies: ['auth'],
    },
    // Unauthenticated project for tests that don't require login
    // Only runs *.noauth.spec.ts files (public/unauthenticated tests)
    {
      name: 'chromium-no-auth',
      testMatch: /\.noauth\.spec\.ts$/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: { cookies: [], origins: [] },
      },
      dependencies: ['setup'],
    },
    // Safari/webkit — scoped to the virtualized infinite-scroll spec only.
    // Catches sticky-thead-inside-table bugs specific to Safari rendering.
    // If regressions appear, see the display:grid follow-up issue documented in the plan.
    {
      name: 'webkit-virtualized',
      testMatch: /transactions-infinite-scroll\.auth\.spec\.ts$/,
      use: {
        ...devices['Desktop Safari'],
        ...(existsSync(authFile) ? { storageState: authFile } : {}),
      },
      dependencies: ['auth'],
    },
  ],

  // Run your local dev server before starting the tests
  // Comment this out if you want to run tests against already running servers
  webServer: [
    {
      // Pass PORT, E2E_BACKEND_PORT, and VITE_CLERK_PUBLISHABLE_KEY env vars to Vite
      // E2E_BACKEND_PORT is needed for the Vite proxy configuration
      // VITE_CLERK_PUBLISHABLE_KEY enables Clerk authentication (Vite only exposes VITE_* vars)
      command: `PORT=${frontendPort} E2E_BACKEND_PORT=${backendPort} VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || ''} bun run dev`,
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
    },
    {
      // Pass env vars to backend for E2E testing:
      // - PORT: Dynamic port allocation
      // - ENVIRONMENT: Set to "qa" to enable admin endpoints
      // - DATABASE_URL: PostgreSQL connection (set by run-e2e-with-ports.sh)
      // - CLERK_SECRET_KEY: For Clerk API calls (admin role verification)
      // - CLERK_PUBLISHABLE_KEY: For JWT validation
      command: `cd ../../backend && PORT=${backendPort} ENVIRONMENT=qa DATABASE_URL="${databaseUrl}" CLERK_SECRET_KEY="${process.env.CLERK_SECRET_KEY || ''}" CLERK_PUBLISHABLE_KEY="${process.env.CLERK_PUBLISHABLE_KEY || ''}" ./bin/server`,
      url: `http://localhost:${backendPort}/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
});
