// Issue #924: Centralized PostHog mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

export const mockPosthogCapture = mock(() => {})
export const mockPosthogIdentify = mock(() => {})
export const mockPosthogReset = mock(() => {})
export const mockPosthogOptIn = mock(() => {})
export const mockPosthogOptOut = mock(() => {})
export const mockPosthogInit = mock(() => {})

export const mockPosthogInstance = {
  init: mockPosthogInit,
  capture: mockPosthogCapture,
  identify: mockPosthogIdentify,
  reset: mockPosthogReset,
  opt_in_capturing: mockPosthogOptIn,
  opt_out_capturing: mockPosthogOptOut,
}

// Mock posthog-js default export
mock.module('posthog-js', () => ({
  default: mockPosthogInstance,
}))

// Mock our posthog lib so tests that import it get no-ops
mock.module('../../shared/lib/posthog', () => ({
  initPostHog: mock(() => {}),
  posthog: mockPosthogInstance,
}))
