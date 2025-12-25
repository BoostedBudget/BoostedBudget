/**
 * Test setup for Bun test runner with happy-dom
 * Migrated from Vitest in Issue #575
 */

import { GlobalRegistrator } from "@happy-dom/global-registrator";
import "@testing-library/jest-dom";
import { cleanup } from "@testing-library/react";
import { afterEach, afterAll } from "bun:test";

// Register happy-dom globals (window, document, navigator, etc.)
// This must be called before any React rendering
GlobalRegistrator.register();

// Mock APIs not provided by happy-dom

// ResizeObserver - used by many UI libraries for responsive components
globalThis.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// IntersectionObserver - used for lazy loading and visibility detection
globalThis.IntersectionObserver = class IntersectionObserver {
  constructor() {}
  observe() {}
  unobserve() {}
  disconnect() {}
  root = null;
  rootMargin = "";
  thresholds = [];
  takeRecords() {
    return [];
  }
};

// matchMedia - used for responsive design media queries
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// scrollTo - used by scroll-to-element behaviors
Object.defineProperty(window, "scrollTo", {
  writable: true,
  value: () => {},
});

// getComputedStyle enhancement for CSS tests
const originalGetComputedStyle = window.getComputedStyle;
window.getComputedStyle = (element: Element, pseudoElement?: string | null) => {
  try {
    return originalGetComputedStyle(element, pseudoElement);
  } catch {
    // Return minimal computed style if element is not in DOM
    return {
      getPropertyValue: () => "",
    } as CSSStyleDeclaration;
  }
};

// Ensure cleanup runs after each test (critical for memory management)
// This unmounts React trees from render() and renderHook() calls
// Without this, 110+ renderHook calls in dashboard tests accumulate without cleanup
// See: Issue #422 memory optimization
afterEach(() => {
  cleanup();
});

// Force cleanup of any pending handles after all tests complete
// Prevents zombie processes from persisting
// See: Issue #422 zombie process prevention
afterAll(async () => {
  await new Promise((resolve) => setTimeout(resolve, 100));
});
