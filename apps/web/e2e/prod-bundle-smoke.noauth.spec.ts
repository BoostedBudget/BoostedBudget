/**
 * Production bundle smoke tests — catches Rolldown cross-chunk init crashes.
 *
 * Exercises rolldownOptions.codeSplitting via `vite preview` (not `vite dev`).
 * Regression: Rolldown 1.0.3 could leave query-core helpers undefined at QueryClient
 * construction → "TypeError: n is not a function" → blank page.
 */
import { test, expect } from '@playwright/test';
import { readdirSync } from 'node:fs';
import { resolve } from 'node:path';

test.describe('Production bundle smoke tests', () => {
  test('landing does not fetch the separately built RevenueCat checkout SDK', async ({ page }) => {
    const sdkChunks = readdirSync(resolve(import.meta.dirname, '../dist/assets'))
      .filter(name => /^revenuecat-.*\.js$/.test(name));
    expect(sdkChunks.length, 'The checkout SDK must have its own lazy chunk').toBeGreaterThan(0);
    const requestedChunks: string[] = [];
    page.on('request', request => {
      requestedChunks.push(new URL(request.url()).pathname.split('/').pop() ?? '');
    });
    await page.goto('/');
    await expect(page.locator('#root')).not.toBeEmpty({ timeout: 15_000 });
    expect(requestedChunks.filter(name => sdkChunks.includes(name))).toEqual([]);
  });

  test('app mounts without uncaught JS errors', async ({ page }) => {
    const uncaughtErrors: string[] = [];

    // Capture any uncaught exceptions (module init failures, chunk order crashes).
    // These fire before React's ErrorBoundary can intercept them.
    page.on('pageerror', (err) => {
      uncaughtErrors.push(err.message);
    });

    await page.goto('/');

    // Module-eval pageerrors (cross-chunk init crashes like "n is not a function") fire
    // synchronously during script evaluation, before page.goto() resolves for load event.
    // No additional wait is needed — they're already captured by this point.

    // Check uncaught errors first: gives a meaningful failure message (the actual error text)
    // rather than just "#root is empty" when the crash is the root cause.
    expect(uncaughtErrors, `Uncaught JS errors during app boot: ${uncaughtErrors.join('; ')}`).toHaveLength(0);

    // After confirming no crashes, assert React mounted.
    // The specific crash we guard against (#root stays empty) is already caught above, but
    // this assertion is the final proof that the full React boot cycle completed.
    await expect(page.locator('#root')).not.toBeEmpty({ timeout: 10_000 });
  });

  test('React root element is populated after navigation to /', async ({ page }) => {
    await page.goto('/');

    // #root should never remain empty. An empty root means the entry module threw during
    // evaluation (e.g. a Rolldown cross-chunk init crash) before ReactDOM.createRoot().render()
    // was reached. Use Playwright's auto-retrying assertion so we wait past Clerk's initial
    // null-render (AuthReadyGate returns null until isLoaded — root is briefly empty).
    await expect(page.locator('#root')).not.toBeEmpty({ timeout: 15_000 });
  });

  // #1666: the discovery files exist in public/ and must be served as themselves, not as
  // the SPA shell. This is the preview-time half of the served-content proof; the deployed
  // half (where `_redirects`' catch-all and `_headers` precedence actually interact) is
  // settled separately — see .omo/evidence/1666/llms-precedence-verdict.txt.
  test('discovery files are served as non-HTML text, not the SPA shell', async ({ request }) => {
    for (const path of ['/llms.txt', '/llms-full.txt']) {
      const response = await request.get(path);
      expect(response.status(), `${path} status`).toBe(200);
      expect(response.headers()['content-type'] ?? '', `${path} content-type`).toContain('text/plain');
      const body = await response.text();
      expect(body.slice(0, 1), `${path} first character`).toBe('#');
      expect(body, `${path} must not be the SPA shell`).not.toContain('<div id="root">');
    }

    for (const path of ['/robots.txt', '/sitemap.xml']) {
      const response = await request.get(path);
      expect(response.status(), `${path} status`).toBe(200);
      expect(response.headers()['content-type'] ?? '', `${path} must not be HTML`).not.toContain('text/html');
    }
  });
});
