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
 * Port allocation: E2E_FRONTEND_PORT, E2E_BACKEND_PORT, E2E_POSTGRES_PORT, E2E_PROD_PORT
 * (set via scripts/find-available-ports.js in CI)
 *
 * Auth: TEST_CLERK_EMAIL + TEST_CLERK_PASSWORD → e2e/.auth/user.json (reused by chromium)
 */

// Get dynamic ports and database URL from environment, fallback to defaults
const frontendPort = process.env.E2E_FRONTEND_PORT || '3000';
const backendPort = process.env.E2E_BACKEND_PORT || '8080';
const databaseUrl = process.env.DATABASE_URL || '';

// Dedicated port for the production-bundle smoke server.
// Set by find-available-ports.js (via run-e2e-with-ports.sh). When unset (e.g. running
// `bunx playwright test` directly without the shell wrapper), the prod webServer and
// chromium-prod-smoke project are skipped — avoids defaulting to a hardcoded port with
// reuseExistingServer:false, which would kill the entire test run if the port is taken.
const prodPort = process.env.E2E_PROD_PORT || '';

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
    // Production bundle smoke project — regression guard for prod-only Rolldown bundler bugs.
    // Requires E2E_PROD_PORT (set by run-e2e-with-ports.sh). Skipped when running
    // `bunx playwright test` directly without the shell wrapper (port would be unset).
    ...(prodPort ? [{
      name: 'chromium-prod-smoke',
      testMatch: /prod-bundle-smoke\.noauth\.spec\.ts$/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: `http://localhost:${prodPort}`,
        storageState: { cookies: [], origins: [] },
      },
      // No 'setup' dependency: smoke tests don't touch Clerk or the backend.
    }] : []),
    // Safari/webkit — sticky-table specs that catch Safari-specific rendering bugs:
    // - position:sticky + thead + border-collapse background painting
    // - virtualized scroll container stacking context
    {
      name: 'webkit-sticky',
      testMatch: /(transactions-infinite-scroll|data-table-sticky-header)\.auth\.spec\.ts$/,
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
      // Pass PORT, E2E_BACKEND_PORT, and VITE_CLERK_PUBLISHABLE_KEY env vars to Vite.
      // E2E_BACKEND_PORT is needed for the Vite proxy configuration.
      // VITE_CLERK_PUBLISHABLE_KEY enables Clerk authentication (Vite only exposes VITE_* vars).
      command: `PORT=${frontendPort} E2E_BACKEND_PORT=${backendPort} VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || ''} bun run dev`,
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
    },
    // Production bundle smoke server (only added when E2E_PROD_PORT is set).
    // Builds the Rolldown production bundle and serves via `vite preview` — the only way
    // to exercise rolldownOptions.codeSplitting. `vite dev` bypasses chunking entirely.
    ...(prodPort ? [{
      command: `VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || 'pk_test_e2e_placeholder'} bun run build && bunx vite preview --port ${prodPort} --strictPort`,
      url: `http://localhost:${prodPort}`,
      reuseExistingServer: false, // always rebuild: command includes the build step
      timeout: 180 * 1000, // ~60s for tsc+rolldown build + preview startup
    }] : []),
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
