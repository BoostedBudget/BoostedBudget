// Issue #575: Testing Library setup for Bun tests (load AFTER happydom.ts)
import { afterEach, afterAll, expect } from 'bun:test'
import { cleanup } from '@testing-library/react'
import * as matchers from '@testing-library/jest-dom/matchers'

expect.extend(matchers)

// Ensure cleanup runs after each test (critical for memory management)
// This unmounts React trees from render() and renderHook() calls
// Also resets URL state to prevent pollution between tests
afterEach(() => {
  cleanup()
  // Reset URL state to prevent pollution between test files (Issue #575)
  // Tests using useSearchParams or window.location need consistent initial state
  if (typeof window !== 'undefined' && window.history) {
    window.history.replaceState({}, '', 'http://localhost:3000/')
  }
})

// Force cleanup of any pending handles after all tests complete
// Prevents hanging processes (matches vitest setup.ts pattern from Issues #180, #219, #422)
afterAll(async () => {
  await new Promise((resolve) => setTimeout(resolve, 100))
})
