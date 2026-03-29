import { test, expect } from './fixtures';

/**
 * Security Headers E2E Tests
 * Validates that nginx serves proper security headers on all responses.
 *
 * Issue #883: Add Content-Security-Policy header to frontend nginx config.
 *
 * NOTE: These tests only run against the nginx production build (not the Vite dev server).
 * Set NGINX_ENV=true when running against the Docker/QA environment.
 * Example: NGINX_ENV=true bun run test:e2e -- security-headers
 */

const isNginxEnv = !!process.env.NGINX_ENV;

test.describe('Security Headers (nginx only)', () => {
  test.skip(!isNginxEnv, 'Requires nginx environment (set NGINX_ENV=true)');

  test('should include CSP-Report-Only header on page responses', async ({ page }) => {
    const response = await page.goto('/');
    expect(response).not.toBeNull();

    const headers = response!.headers();
    const csp = headers['content-security-policy-report-only'];

    expect(csp).toBeDefined();
    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain('script-src');
    expect(csp).toContain('cdn.paddle.com');
    expect(csp).toContain('cdn.plaid.com');
    expect(csp).toContain("frame-ancestors 'self'");
    expect(csp).toContain("object-src 'none'");
  });

  test('should include CSP-Report-Only header on static asset responses', async ({ page }) => {
    // Capture a JS asset response while loading the page
    const jsResponsePromise = page.waitForResponse(
      resp => resp.url().endsWith('.js') && resp.status() === 200,
    );
    await page.goto('/');
    const jsResponse = await jsResponsePromise;

    const csp = jsResponse.headers()['content-security-policy-report-only'];
    expect(csp).toBeDefined();
    expect(csp).toContain("default-src 'self'");
  });

  test('should include all standard security headers on page responses', async ({ page }) => {
    const response = await page.goto('/');
    expect(response).not.toBeNull();

    const headers = response!.headers();
    expect(headers['x-frame-options']).toBe('SAMEORIGIN');
    expect(headers['x-content-type-options']).toBe('nosniff');
    expect(headers['referrer-policy']).toBe('strict-origin-when-cross-origin');
    expect(headers['strict-transport-security']).toContain('max-age=31536000');
  });

  test('should include all standard security headers on static asset responses', async ({
    page,
  }) => {
    const jsResponsePromise = page.waitForResponse(
      resp => resp.url().endsWith('.js') && resp.status() === 200,
    );
    await page.goto('/');
    const jsResponse = await jsResponsePromise;

    const headers = jsResponse.headers();
    expect(headers['x-content-type-options']).toBe('nosniff');
    expect(headers['x-frame-options']).toBe('SAMEORIGIN');
    expect(headers['cache-control']).toContain('immutable');
  });
});
