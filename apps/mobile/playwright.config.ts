import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright configuration for the exported mobile web build.
 *
 * The suite drives the STATIC export (`apps/mobile/dist/web`) served by the committed
 * zero-dependency Bun server (`scripts/serve-web.ts`) — never `expo start`, which is unstable in
 * CI. `mobile-test` exports the web bundle before running `e2e:visual`, so `dist/web` is present
 * by the time the webServer starts.
 */
const PORT = Number(process.env.E2E_VISUAL_PORT ?? '8081')
const HOST = process.env.E2E_VISUAL_HOST ?? '127.0.0.1'
const BASE_URL = process.env.E2E_VISUAL_BASE_URL ?? `http://${HOST}:${PORT}`

export default defineConfig({
  testDir: './e2e/visual',
  timeout: 90_000,
  expect: { timeout: 15_000 },
  // One worker, no parallelism: the spec mutates the viewport and the emulated colour scheme, and
  // a bounded run is worth more here than wall-clock speed.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  // A ceiling below the `mobile-test` job's timeout so a hung run fails the suite and still writes
  // its report instead of having the whole job cancelled with no diagnostics.
  globalTimeout: process.env.CI ? 10 * 60 * 1000 : undefined,
  reporter: process.env.CI
    ? [
        ['list'],
        ['html', { outputFolder: 'playwright-report', open: 'never' }],
      ]
    : [['list']],
  use: {
    baseURL: BASE_URL,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'bun run serve:web',
    url: `${BASE_URL}/gallery`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
})
