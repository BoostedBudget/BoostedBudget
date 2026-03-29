// Issue #575: Centralized tags wrapper hook mock
// This mocks the WRAPPER hook (useTags) for component tests
// that need to test in isolation from the actual hook implementation.
//
// NOTE: This file is NOT preloaded because useTags.test.tsx needs
// the real hook with API-level mocks. Component tests that need
// to mock useTags should import this file directly.

import { mock } from 'bun:test'

// === useTags wrapper hook mock ===
export const mockUseTagsRefetch = mock(() => {})
export const mockUseTags = mock(() => ({
  tags: [],
  loading: false,
  error: null,
  refetch: mockUseTagsRefetch,
}))

// Mock both absolute and relative paths
mock.module('@/features/tags/hooks/useTags', () => ({
  useTags: () => mockUseTags(),
}))

mock.module('../../tags/hooks/useTags', () => ({
  useTags: () => mockUseTags(),
}))
