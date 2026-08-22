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
  // Matches the CI/local split in apps/web/playwright.config.ts rather than inventing a number:
  // that config's comment gives the reason — "CI runs on a shared Mac Mini (4 runners) with heavy
  // job contention". This config was added later and never inherited the CI allowance, so it kept a
  // flat 15s and duly failed on #1537's branch with "Test timeout of 15000ms exceeded while setting
  // up page" — during PAGE SETUP, before any masking assertion, at host load average 13.46, having
  // passed on the three previous commits.
  //
  // ⚠ This is a CONSISTENCY fix, not a flake being papered over, and the distinction matters because
  // this repo has a standing rule (commit 02baad81) that a budget firing on healthy runs is evidence
  // the host cannot carry the matrix, NOT an argument for a bigger number. That rule still holds for
  // the E2E job's `timeout-minutes` cap. It does not apply here: the sibling config for the same tool
  // already decided CI deserves 2x local, so aligning with it removes an outlier rather than raising
  // a budget. If this starts firing at 30s too, do NOT raise it again — that would be the host.
  timeout: process.env.CI ? 30_000 : 15_000,
  fullyParallel: false,
  reporter: [['list']],
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
