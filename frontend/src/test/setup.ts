import '@testing-library/jest-dom'
import { cleanup } from '@testing-library/react'
import { afterEach, afterAll } from 'vitest'

// Ensure cleanup runs after each test (critical for memory management)
// This unmounts React trees from render() and renderHook() calls
// Without this, 110+ renderHook calls in dashboard tests accumulate without cleanup
// See: Issue #422 memory optimization
afterEach(() => {
  cleanup()
})

// Force cleanup of any pending handles after all tests complete
// Prevents zombie Vitest worker processes from persisting
// See: Issue #422 zombie process prevention
afterAll(async () => {
  await new Promise((resolve) => setTimeout(resolve, 100))
})
