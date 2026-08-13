import { defineConfig, devices } from '@playwright/test'

/**
 * Issue #1514 (item 4): dedicated, minimal Playwright config for the rrweb `<option>`
 * masking proof — deliberately isolated from the main `apps/web/playwright.config.ts`.
 *
 * That main config's projects only match `*.auth.spec.ts`/`*.noauth.spec.ts` and every
 * project depends on a `setup`/`auth` chain that spins up the real Vite dev server, the
 * real backend, and a Clerk login — none of which this test needs (it serves a two-line
 * static fixture from a throwaway `http.createServer` in the spec file itself). Reusing
 * the main config here would mean this one test drags in the entire E2E harness.
 *
 * Run with:
 *   cd apps/web && npx playwright test --config=e2e/rrweb-mask-proof/playwright.config.ts
 */
export default defineConfig({
  testDir: '.',
  timeout: 15_000,
  fullyParallel: false,
  reporter: [['list']],
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
