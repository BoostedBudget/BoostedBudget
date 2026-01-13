/**
 * Custom Playwright Test Fixtures
 *
 * Extends the base Playwright test with custom fixtures for API testing.
 * The apiRequest fixture provides a request context that uses the apiURL
 * from playwright.config.ts, enabling direct API calls to the backend.
 *
 * Issue #691: Fix E2E tests - Playwright's built-in `request` fixture uses
 * `baseURL` (frontend), not the custom `apiURL` (backend). This fixture
 * provides `apiRequest` that correctly targets the backend API.
 */

import { test as base, APIRequestContext } from '@playwright/test';

export type TestOptions = {
  /** Backend API URL from playwright.config.ts */
  apiURL: string;
  /** API request context configured with apiURL as baseURL */
  apiRequest: APIRequestContext;
};

export const test = base.extend<TestOptions>({
  // Custom option to receive apiURL from config
  apiURL: ['', { option: true }],

  // Custom fixture that creates a request context using the backend API URL
  apiRequest: async ({ playwright, apiURL }, use) => {
    const context = await playwright.request.newContext({
      baseURL: apiURL,
    });
    await use(context);
    await context.dispose();
  },
});

export { expect } from '@playwright/test';
