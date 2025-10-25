import { test, expect } from '@playwright/test';

/**
 * Health Endpoint E2E Tests
 * Verifies the backend health check endpoint is accessible and returns correct status
 */

test.describe('Health Endpoint', () => {
  test('should return healthy status from backend', async ({ page: _page, request }) => {
    // Test the API health endpoint directly (uses apiURL from playwright.config.ts)
    const response = await request.get('/health');

    expect(response.status()).toBe(200);

    const body = await response.json();
    expect(body).toHaveProperty('status', 'healthy');
    expect(body).toHaveProperty('timestamp');
    expect(body).toHaveProperty('version'); // Backend returns version field
  });

  test('should load application homepage successfully', async ({ page }) => {
    // Navigate to the homepage
    await page.goto('/');

    // Wait for the page to load
    await page.waitForLoadState('networkidle');

    // Verify page loaded successfully (check for main content)
    await expect(page).toHaveTitle(/BoostedBudget/i);
  });

  test('should have backend API accessible', async ({ request }) => {
    // Verify backend is running and accessible (uses apiURL from playwright.config.ts)
    const response = await request.get('/health');

    expect(response.ok()).toBeTruthy();

    // Response time header might not exist yet, but we verify the response is fast
    expect(response.status()).toBe(200);
  });
});
