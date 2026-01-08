// Issue #575: Testing Library setup for Bun tests (load AFTER happydom.ts)
import { afterEach, expect } from 'bun:test'
import { cleanup } from '@testing-library/react'
import * as matchers from '@testing-library/jest-dom/matchers'

expect.extend(matchers)

afterEach(() => {
  cleanup()
})
