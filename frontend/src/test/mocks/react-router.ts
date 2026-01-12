// Issue #575: Centralized react-router-dom mock for useNavigate
// This file is preloaded via bunfig.toml before all tests
//
// NOTE: We only mock useNavigate. Other react-router-dom exports are used as-is.
// The spread approach with dynamic import doesn't work well with bun's mock.module.

import { mock } from 'bun:test'
import * as ReactRouterDom from 'react-router-dom'

// Export mockNavigate for tests to configure and assert
export const mockNavigate = mock(() => {})

// Mock only useNavigate, re-export everything else from the real module
mock.module('react-router-dom', () => ({
  ...ReactRouterDom,
  useNavigate: () => mockNavigate,
}))
