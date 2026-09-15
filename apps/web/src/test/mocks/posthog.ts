// Issue #924: Centralized PostHog mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import { PostHog } from 'posthog-js'

export const mockPosthogCapture = mock((_event?: string, _properties?: Record<string, unknown>) => {})
export const mockPosthogIdentify = mock(() => {})
export const mockPosthogReset = mock(() => {})
export const mockPosthogInit = mock(() => {})
export const mockPosthogSetConfig = mock(() => {})
export const mockPosthogClearOptInOutCapturing = mock(() => {})
// Issue #1413: server-authoritative tier/comp person-property sync (usePostHogIdentify)
export const mockPosthogSetPersonProperties = mock(() => {})
export const mockPosthogStopSessionRecording = mock(() => {})
export const mockGrantProductAnalytics = mock((
  _scope: string,
  _distinctId?: string,
  _properties?: Record<string, unknown>,
) => Promise.resolve())
export const mockApplyProductAnalyticsAuthority = mock((
  _scope: string,
  _distinctId?: string,
  _properties?: Record<string, unknown>,
) => Promise.resolve())
export const mockDenyProductAnalytics = mock(() => {})
export const mockWithdrawProductAnalytics = mock((_scope: string) => {})
export const mockSetAnalyticsLifecyclePending = mock(() => {})
export const mockSetProductAnalyticsScope = mock((_scope: string) => {})
export const mockCapturePageView = mock(() => {})
export const mockCaptureProductEvent = mock((event: string, properties?: Record<string, unknown>) => {
  mockPosthogCapture(event, properties)
})
export const mockListenForAnalyticsWithdrawal = mock(() => () => {})

const mockRetryQueue = {
  _queue: [] as unknown[],
  _enqueue(request: unknown) { this._queue.push(request) },
  retriableRequest(request: unknown) { this._enqueue(request) },
}

export const mockPosthogInstance = {
  _retryQueue: mockRetryQueue,
  config: { persistence_name: 'test', token: 'phc_test' },
  init: mockPosthogInit,
  capture: mockPosthogCapture,
  clear_opt_in_out_capturing: mockPosthogClearOptInOutCapturing,
  identify: mockPosthogIdentify,
  reset: mockPosthogReset,
  set_config: mockPosthogSetConfig,
  setPersonProperties: mockPosthogSetPersonProperties,
  stopSessionRecording: mockPosthogStopSessionRecording,
}

// Mock posthog-js default export.
//
// PostHog is listed explicitly because bun fills a named import missing from a
// mock factory from the package's CJS main (posthog-js/dist/main.js), which has
// no PostHog export — shared/posthog/engagement.ts imports that class, and the
// fallback otherwise fails module linking with "Export named 'PostHog' not
// found". The real class is required by the tests that drive the SDK directly.
mock.module('posthog-js', () => ({
  default: mockPosthogInstance,
  PostHog,
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
