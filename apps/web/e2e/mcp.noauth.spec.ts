import { test, expect } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * MCP Page E2E Tests - Unauthenticated
 *
 * Coverage for the public `/mcp` marketing + docs page (Issue #1411): the landing-page
 * band that links to it, direct deep-linking while signed out, heading structure, the
 * sticky TOC rail, client tab switching, and the app-shell `<main>` contract.
 *
 * These tests run in the chromium-no-auth project (user not logged in). The dev server
 * is started with VITE_MCP_URL set (see playwright.config.ts), so the page renders its
 * "configured" state rather than the fail-closed placeholder.
 */

test.describe('MCP Page - Unauthenticated', () => {
  // Clerk SDK initialization under parallel worker load is non-deterministic.
  // A single retry is enough to distinguish infrastructure flakiness (Clerk
  // slow to initialize under load) from genuine test failures (wrong content).
  test.describe.configure({ retries: 1 });

  test.describe('Landing page band', () => {
    test.beforeEach(async ({ page }) => {
      test.setTimeout(60000);
      await page.goto('/');
      await page.locator('header[role="banner"]').waitFor({ state: 'visible', timeout: 50000 });
      await acceptCookieConsent(page);
    });

    test('renders and links to /mcp, and clicking it navigates there', async ({ page }) => {
      // Scoped to the band's own section (aria-labelledby="mcp-band-heading") — the
      // footer links to /mcp on every page too, but under a different accessible name
      // ("MCP Server" vs. this band's "Explore the MCP server"), so this scoping isn't
      // strictly required for uniqueness, but keeps the locator resilient regardless.
      const band = page.locator('section[aria-labelledby="mcp-band-heading"]');
      await expect(band).toBeVisible();

      const bandLink = band.getByRole('link', { name: 'Explore the MCP server' });
      await expect(bandLink).toHaveAttribute('href', '/mcp');

      await bandLink.click();
      await expect(page).toHaveURL(/\/mcp$/);
    });
  });

  test.describe('/mcp page', () => {
    test.beforeEach(async ({ page }) => {
      test.setTimeout(60000);
      await page.goto('/mcp');
      await page.locator('header[role="banner"]').waitFor({ state: 'visible', timeout: 50000 });
      await acceptCookieConsent(page);
    });

    // Single most important assertion in this file: the page owner explicitly requires
    // /mcp be readable without logging in — a signed-out visitor deep-linking here must
    // land on the real page, not get bounced to '/'.
    test('is reachable by direct deep link while signed out, with no redirect to /', async ({ page }) => {
      await expect(page).toHaveURL(/\/mcp$/);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    });

    test('has exactly one h1', async ({ page }) => {
      await expect(page.locator('h1')).toHaveCount(1);
    });

    test('shows the TOC rail at desktop width with anchors pointing at real sections', async ({ page }) => {
      await page.setViewportSize({ width: 1280, height: 900 });

      const tocNav = page.locator('nav[aria-label="On this page"]');
      await expect(tocNav).toBeVisible();

      const hrefs = await tocNav.locator('a').evaluateAll((anchors) =>
        anchors.map((a) => a.getAttribute('href'))
      );
      expect(hrefs.length).toBeGreaterThan(0);
      for (const href of hrefs) {
        expect(href).toBeTruthy();
        expect(href!.startsWith('#')).toBe(true);
        // Each TOC anchor's target id must exist exactly once on the page.
        await expect(page.locator(href!)).toHaveCount(1);
      }

      // At least one anchor actually navigates/scrolls to its section.
      await tocNav.getByRole('link', { name: 'Connect' }).click();
      await expect(page).toHaveURL(/#connect$/);
      await expect(page.locator('#connect')).toBeInViewport();
    });

    test('switching client tabs swaps the visible panel', async ({ page }) => {
      const claudeDesktopTab = page.getByRole('tab', { name: 'Claude Desktop' });
      const cursorTab = page.getByRole('tab', { name: 'Cursor' });
      // Each tab's panel content carries its own accessible `role="region"` — distinct
      // from the panel's role="tabpanel" wrapper — so we can pin exactly which panel is
      // mounted without asserting on marketing copy.
      const claudeDesktopUrlRegion = page.getByRole('region', { name: 'Claude Desktop server URL' });
      const cursorConfigRegion = page.getByRole('region', { name: 'Cursor configuration' });

      // Default tab (no #connect-<slug> hash, fresh localStorage) is Claude Desktop.
      await expect(claudeDesktopTab).toHaveAttribute('aria-selected', 'true');
      await expect(cursorTab).toHaveAttribute('aria-selected', 'false');
      await expect(claudeDesktopUrlRegion).toBeVisible();
      // Radix TabsContent unmounts inactive panels by default (no forceMount), so the
      // Cursor panel isn't just visually hidden — it isn't in the DOM at all yet.
      await expect(cursorConfigRegion).toBeHidden();

      await cursorTab.click();

      await expect(cursorTab).toHaveAttribute('aria-selected', 'true');
      await expect(claudeDesktopTab).toHaveAttribute('aria-selected', 'false');
      await expect(cursorConfigRegion).toBeVisible();
      await expect(claudeDesktopUrlRegion).toBeHidden();
    });

    test('renders no nested <main> — the app shell provides the only one', async ({ page }) => {
      await expect(page.locator('main')).toHaveCount(1);
    });
  });
});
