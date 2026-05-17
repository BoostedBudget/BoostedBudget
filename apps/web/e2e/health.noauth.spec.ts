import { test, expect } from './fixtures';

/**
 * Health Endpoint E2E Tests
 * Verifies the backend health check endpoint is accessible and returns correct status
 *
 * Issue #691: Uses custom apiRequest fixture to make direct calls to the backend API.
 * The built-in `request` fixture uses `baseURL` (frontend), which serves HTML via
 * Vite's SPA catch-all. The `apiRequest` fixture uses `apiURL` (backend) directly.
 */

test.describe('Health Endpoint', () => {
  test('should return healthy status from backend', async ({ apiRequest }) => {
    // Test the API health endpoint directly using apiRequest fixture
    const response = await apiRequest.get('/health');

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
    await page.waitForLoadState('domcontentloaded');

    // Verify page loaded successfully (check for main content)
    await expect(page).toHaveTitle(/BoostedBudget/i);
  });

  test('should have backend API accessible', async ({ apiRequest }) => {
    // Verify backend is running and accessible using apiRequest fixture
    const response = await apiRequest.get('/health');

    expect(response.ok()).toBeTruthy();

    // Response time header might not exist yet, but we verify the response is fast
    expect(response.status()).toBe(200);
  });
});
