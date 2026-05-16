import type { Page } from '@playwright/test';

/**
 * Collects all row IDs seen in a virtualized table across scroll events.
 * Virtualized tables only render visible rows in the DOM, so we must scroll
 * and collect IDs incrementally to verify no duplicates and growing set.
 *
 * The scroll sentinel (`data-testid="virtual-sentinel"`) is watched to detect
 * when the list is exhausted — once it appears and isFetchingMore is false
 * (sentinel has no text content), we stop scrolling.
 */
export async function collectVirtualizedRowIds(
  page: Page,
  options: {
    scrollContainerSelector: string;  // e.g. '[data-testid="virtual-scroll-container"]'
    rowSelector: string;               // e.g. '[data-row-id]'
    rowIdAttribute: string;            // e.g. 'data-row-id'
    scrollStepPx?: number;             // default 2000
    maxScrolls?: number;               // default 20
    requestUrlPattern?: string;        // e.g. '**/transactions**' — waited between scrolls
  }
): Promise<string[]> {
  const {
    scrollContainerSelector,
    rowSelector,
    rowIdAttribute,
    scrollStepPx = 2000,
    maxScrolls = 20,
    requestUrlPattern,
  } = options;

  const seenIds = new Set<string>();

  // Collect all currently-visible row IDs from the DOM.
  const collectVisible = async () => {
    const ids = await page.evaluate(
      ({ sel, attr }) =>
        Array.from(document.querySelectorAll(sel)).map(
          (el) => (el as HTMLElement).getAttribute(attr) ?? ''
        ),
      { sel: rowSelector, attr: rowIdAttribute }
    );
    ids.forEach((id) => { if (id) seenIds.add(id); });
  };

  await collectVisible();

  for (let i = 0; i < maxScrolls; i++) {
    const container = page.locator(scrollContainerSelector);
    await container.evaluate((el, step) => { el.scrollTop += step; }, scrollStepPx);

    if (requestUrlPattern) {
      // Wait for a new network request if one is expected — deflakes the gap
      // between a scroll event triggering a fetch and the response arriving.
      try {
        await page.waitForRequest(requestUrlPattern, { timeout: 2000 });
      } catch {
        // No new request — scroll didn't trigger a fetch (still on same page).
      }
    }

    // Brief settle for virtual-window recalculation.
    await page.waitForTimeout(300);
    await collectVisible();

    // Stop when the sentinel is present and not showing a loading spinner
    // (i.e. `hasMore` is false and `isFetchingMore` is false — sentinel cell
    // is empty or absent).
    const sentinel = page.locator('[data-testid="virtual-sentinel"]');
    if (await sentinel.count() > 0) {
      const text = (await sentinel.textContent()) ?? '';
      if (text.trim() === '') {
        // Sentinel present but empty — no more pages to load.
        break;
      }
    }
  }

  return Array.from(seenIds);
}
