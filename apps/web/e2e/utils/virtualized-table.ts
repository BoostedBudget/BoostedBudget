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

    // Attach listener BEFORE triggering scroll so the synchronous mocked
    // request cannot fire before Playwright is watching for it.
    const waitForLoad = requestUrlPattern
      ? page.waitForRequest(requestUrlPattern, { timeout: 5000 }).catch(() => null)
      : Promise.resolve(null);

    const scroll = container.evaluate(
      (el, step) => { el.scrollTop += step; },
      scrollStepPx,
    );

    await Promise.all([scroll, waitForLoad]);
    await page.waitForTimeout(150);
    await collectVisible();

    // Break only when the sentinel disappears entirely — that means hasMore
    // flipped to false (all pages loaded). While hasMore is still true the
    // sentinel is present (possibly with empty text between fetches); we must
    // keep scrolling to accumulate rows from subsequent pages.
    const sentinel = page.locator('[data-testid="virtual-sentinel"]');
    if (await sentinel.count() === 0) {
      break;
    }
  }

  return Array.from(seenIds);
}
