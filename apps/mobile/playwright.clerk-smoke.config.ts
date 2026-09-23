import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright configuration for the NON-GATING credentialed Clerk smoke test.
 *
 * This lane is deliberately separate from `playwright.config.ts` (the gating visual/accessibility
 * suite). Two things would silently invalidate the smoke if it shared them:
 *
 *   1. The export directory. `EXPO_PUBLIC_*` is inlined at build time, so an inert export can never
 *      sign in. The smoke serves its OWN keyed export (`dist/web-clerk-smoke`) — never `dist/web`,
 *      which is the inert artifact the gating `e2e:visual` step depends on. Building the keyed
 *      export into `dist/web` would clobber that artifact.
 *   2. The port and server lifecycle. A distinct port plus `reuseExistingServer: false` guarantees
 *      the smoke cannot attach to the visual suite's server (which serves the inert export) and
 *      then false-skip.
 *
 * The export is produced BEFORE Playwright starts, by the `test:clerk-smoke` package script, so the
 * `webServer` below only serves it. Keeping the export out of the `webServer.command` matters: the
 * Expo web export outruns the webServer timeout and is already the pattern the visual suite uses.
 *
 * The smoke is not authoritative until CI secrets exist — the `mobile-test` CI job has no Clerk
 * secrets, and this config is never wired into it. Run it locally with real credentials.
 */
const PORT = Number(process.env.E2E_CLERK_SMOKE_PORT ?? '8092')
const HOST = process.env.E2E_CLERK_SMOKE_HOST ?? '127.0.0.1'
const BASE_URL = process.env.E2E_CLERK_SMOKE_BASE_URL ?? `http://${HOST}:${PORT}`

export default defineConfig({
  testDir: './e2e/clerk-smoke',
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: BASE_URL,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `E2E_WEB_ROOT=dist/web-clerk-smoke E2E_VISUAL_PORT=${PORT} bun run serve:web`,
    url: `${BASE_URL}/sign-in`,
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
})
