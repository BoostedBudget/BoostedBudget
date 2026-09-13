// Issue #924: Centralized PostHog mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

export const mockPosthogCapture = mock((_event?: string, _properties?: Record<string, unknown>) => {})
export const mockPosthogIdentify = mock(() => {})
export const mockPosthogReset = mock(() => {})
export const mockPosthogOptIn = mock(() => {})
export const mockPosthogOptOut = mock(() => {})
export const mockPosthogInit = mock(() => {})
// Issue #1413: server-authoritative tier/comp person-property sync (usePostHogIdentify)
export const mockPosthogSetPersonProperties = mock(() => {})
export const mockPosthogStopSessionRecording = mock(() => {})
export const mockGrantProductAnalytics = mock((
  _scope: string,
  _distinctId?: string,
  _properties?: Record<string, unknown>,
) => {})
export const mockApplyProductAnalyticsAuthority = mock((
  _scope: string,
  _distinctId?: string,
  _properties?: Record<string, unknown>,
) => {})
export const mockDenyProductAnalytics = mock(() => {})
export const mockWithdrawProductAnalytics = mock((_scope: string) => {})
export const mockSetAnalyticsLifecyclePending = mock(() => {})
export const mockSetProductAnalyticsScope = mock((_scope: string) => {})
export const mockCapturePageView = mock(() => {})
export const mockCaptureProductEvent = mock((event: string, properties?: Record<string, unknown>) => {
  mockPosthogCapture(event, properties)
})
export const mockListenForAnalyticsWithdrawal = mock(() => () => {})

export const mockPosthogInstance = {
  init: mockPosthogInit,
  capture: mockPosthogCapture,
  identify: mockPosthogIdentify,
  reset: mockPosthogReset,
  opt_in_capturing: mockPosthogOptIn,
  opt_out_capturing: mockPosthogOptOut,
  setPersonProperties: mockPosthogSetPersonProperties,
  stopSessionRecording: mockPosthogStopSessionRecording,
}

// Mock posthog-js default export
mock.module('posthog-js', () => ({
  default: mockPosthogInstance,
}))

// Mock our posthog lib so tests that import it get no-ops.
// isPostHogConfigured uses the real window.ENV check so individual tests can control
// configured vs. non-configured state by setting window.ENV.POSTHOG_KEY.
mock.module('../../shared/lib/posthog', () => ({
  initPostHog: mock(() => {}),
  posthog: mockPosthogInstance,
  isPostHogConfigured: () => !!(window.ENV?.POSTHOG_KEY),
  captureProductEvent: mockCaptureProductEvent,
}))

// Mock posthog-js/react for PostHogProvider (used by apps/web/src/main.tsx)
mock.module('posthog-js/react', () => ({
  PostHogProvider: ({ children }: { children: unknown }) => children,
}))
