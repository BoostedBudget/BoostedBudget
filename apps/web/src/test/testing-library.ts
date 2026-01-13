// Issue #575: Testing Library setup for Bun tests (load AFTER happydom.ts)
import { afterEach, afterAll, expect } from 'bun:test'
import { cleanup } from '@testing-library/react'
import * as matchers from '@testing-library/jest-dom/matchers'

expect.extend(matchers)

// Cleanup after each test: unmount React trees and reset URL state
// Prevents pollution between tests and memory exhaustion
afterEach(() => {
  cleanup()
  if (typeof window !== 'undefined' && window.history) {
    window.history.replaceState({}, '', 'http://localhost:3000/')
  }
})

// Force cleanup of any pending handles after all tests complete
// Prevents hanging processes (Issues #180, #219, #422)
afterAll(async () => {
  await new Promise((resolve) => setTimeout(resolve, 100))
})
