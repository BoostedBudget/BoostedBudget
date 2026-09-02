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
    // #1569 T8-FIX, finding F. The browser context inherits the RUNNER's timezone and locale unless
    // they are pinned, and specs assert rendered dates and currency: `formatDeletedOn` (and
    // `formatHistoryLabel`) render with `toLocaleDateString(undefined, ...)`, so a fixture at
    // 2026-03-20T12:00:00Z reads "March 20, 2026" in America/New_York and "March 21, 2026" in
    // Pacific/Kiritimati — reproduced with Node's Intl, not reasoned about. That is a green suite
    // that fails on a valid runner. Pinned rather than loosened: an assertion weakened to match any
    // timezone stops testing the thing it was written for, and the fixture is already midday-UTC
    // precisely so ONE pinned zone is enough.
    timezoneId: 'UTC',
    locale: 'en-US',

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
      // Pass PORT, E2E_BACKEND_PORT, VITE_CLERK_PUBLISHABLE_KEY, and VITE_MCP_URL env vars to Vite.
      // E2E_BACKEND_PORT is needed for the Vite proxy configuration.
      // VITE_CLERK_PUBLISHABLE_KEY enables Clerk authentication (Vite only exposes VITE_* vars).
      // VITE_MCP_URL points at the staging MCP Worker so /mcp e2e tests exercise the
      // configured branch, not just the fail-closed ('development') branch — see mcpUrl.ts.
      command: `PORT=${frontendPort} E2E_BACKEND_PORT=${backendPort} VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || ''} VITE_MCP_URL=https://staging-mcp.boostedbudget.com bun run dev`,
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
    },
    // Production bundle smoke server (only added when E2E_PROD_PORT is set).
    // Builds the Rolldown production bundle and serves via `vite preview` — the only way
    // to exercise rolldownOptions.codeSplitting. `vite dev` bypasses chunking entirely.
    ...(prodPort ? [{
      command: `VITE_CLERK_PUBLISHABLE_KEY=${process.env.CLERK_PUBLISHABLE_KEY || 'pk_test_e2e_placeholder'} VITE_MCP_URL=https://staging-mcp.boostedbudget.com bunx vite build && bunx vite preview --port ${prodPort} --strictPort`,
      url: `http://localhost:${prodPort}`,
      reuseExistingServer: false, // always rebuild: command includes the build step
      // #1489: this budget is what the E2E job actually fails on under host contention — the job
      // timed out at 180s while two runs for the same branch built concurrently on the self-hosted
      // runner, with migrations and seeding already green.
      //
      // Two changes, and the first is the real one: the command was `bun run build`, which is
      // `tsc --noEmit && vite build` (package.json). CI ALREADY typechecks the frontend in its own
      // jobs (.github/workflows/test.yml, two `bun run typecheck` steps), so the typecheck was
      // duplicated work inside the budget it was blowing. `bunx vite build` drops it and keeps the
      // bundle this server exists to serve. Second, the budget goes to 300s so a contended runner
      // has headroom rather than a 3x-optimistic estimate.
      timeout: 300 * 1000, // rolldown build + preview startup, with headroom for a loaded runner
    }] : []),
    {
      // Pass env vars to backend for E2E testing:
      // - PORT: Dynamic port allocation
      // - ENVIRONMENT: Set to "qa". DO NOT REMOVE — since #1499 admin endpoints are
      //   reachable in every environment, so this is no longer what "enables" them; but
      //   since #1498 it IS what grants this suite's user admin. "qa" is on the
      //   non-production allowlist (backend/internal/domain/admin.go), which is the only
      //   reason e2e+clerk_test@boostedbudget.com resolves as an admin — it lives in
      //   config/admin_emails.yaml's non_production_admin_emails list, which is excluded
      //   under production AND whenever ENVIRONMENT is unset. Dropping this var makes every
      //   admin spec 403.
      // - DATABASE_URL: PostgreSQL connection (set by run-e2e-with-ports.sh)
      // - CLERK_SECRET_KEY: For Clerk API calls (admin role verification)
      // - CLERK_PUBLISHABLE_KEY: For JWT validation
      // - RATE_LIMIT_IP: Issue #1441 — every Playwright worker's traffic arrives from the
      //   same client IP (localhost). The backend's per-IP rate limiter defaults to 100
      //   req/min (backend/internal/middleware/rate_limit.go), which a single page load
      //   alone can approach (profile, notifications x2, check-role, households x2, tags,
      //   budgets, ...); 2 local workers running many spec files easily blows past it,
      //   producing floods of 429s that manifest as assorted "element not found" timeouts
      //   with no direct signal that rate limiting was the cause. This is a test-only
      //   override (only reaches this backend process, started fresh per E2E run) — it does
      //   not touch the production default of 100.
      // - CACHE_ENABLED / CACHE_REDIS_URL / RATE_LIMIT_REDIS_URL (#1531 Task 11e): passed
      //   through from this Node process's own env, empty by default (matching prior
      //   behaviour — CACHE_ENABLED="" resolves to config.go's default of true via
      //   getEnvBool, and an empty CACHE_REDIS_URL leaves cacheInstance a no-op cache, exactly
      //   as before this task). apps/web/e2e/cache-freshness.auth.spec.ts and
      //   cache-household-visibility.auth.spec.ts read `cache_enabled` off
      //   /api/v1/admin/system/health and hard-fail (no skip path) if it isn't true — CI's
      //   e2e-test job (.github/workflows/test.yml) now provisions real Redis containers and
      //   sets these three vars so that guard's precondition genuinely holds there; locally,
      //   set them yourself (see run-e2e-tests skill) to exercise the same path.
      command: `cd ../../backend && PORT=${backendPort} ENVIRONMENT=qa DATABASE_URL="${databaseUrl}" CLERK_SECRET_KEY="${process.env.CLERK_SECRET_KEY || ''}" CLERK_PUBLISHABLE_KEY="${process.env.CLERK_PUBLISHABLE_KEY || ''}" RATE_LIMIT_IP=100000 CACHE_ENABLED="${process.env.CACHE_ENABLED || ''}" CACHE_REDIS_URL="${process.env.CACHE_REDIS_URL || ''}" RATE_LIMIT_REDIS_URL="${process.env.RATE_LIMIT_REDIS_URL || ''}" ./bin/server`,
      url: `http://localhost:${backendPort}/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 120 * 1000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
});
