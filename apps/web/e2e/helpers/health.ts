import type { Page, TestInfo } from '@playwright/test'

/**
 * Skip the current test immediately if the backend is unreachable.
 *
 * Use this in beforeEach for tests that intentionally depend on a live database
 * (i.e. tests that do NOT mock all API calls). When PostgreSQL crashes mid-run,
 * the test is skipped with a clear message instead of timing out for 30s.
 *
 * Tests that mock all API endpoints via page.route() do NOT need this — they are
 * already backend-independent and will pass regardless of database health.
 */
export async function skipIfBackendDown(page: Page, testInfo: TestInfo) {
  try {
    const apiURL = `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`
    const resp = await page.request.get(`${apiURL}/health`, { timeout: 2000 })
    if (!resp.ok()) {
      testInfo.skip(true, `Backend health check returned ${resp.status()} — PostgreSQL may be down`)
    }
  } catch {
    testInfo.skip(true, 'Backend unreachable — PostgreSQL container likely crashed or not started')
  }
}
