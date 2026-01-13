// Issue #575: Centralized sonner mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

// Configurable mock functions that tests can customize
export const mockToastSuccess = mock(() => {})
export const mockToastError = mock(() => {})
export const mockToastWarning = mock(() => {})
export const mockToastInfo = mock(() => {})
export const mockToastLoading = mock(() => {})
export const mockToastDismiss = mock(() => {})
export const mockToastCustom = mock(() => {})
export const mockToastMessage = mock(() => {})
export const mockToastPromise = mock(() => Promise.resolve())

// Main toast function (can be called directly or as toast.success, etc.)
export const mockToast = Object.assign(mockToastCustom, {
  success: mockToastSuccess,
  error: mockToastError,
  warning: mockToastWarning,
  info: mockToastInfo,
  loading: mockToastLoading,
  dismiss: mockToastDismiss,
  custom: mockToastCustom,
  message: mockToastMessage,
  promise: mockToastPromise,
})

// Single mock.module() call - called once at preload time
mock.module('sonner', () => ({
  toast: mockToast,
  Toaster: () => null,
}))
