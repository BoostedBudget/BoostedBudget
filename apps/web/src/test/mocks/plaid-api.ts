// Issue #839: Centralized Plaid API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealPlaidModule from '@boostedbudget/api-client/endpoints/plaid/plaid'

// === useCreateLinkToken ===
export const mockCreateLinkTokenMutateAsync = mock(() =>
  Promise.resolve({ link_token: 'test-link-token' })
)
export const mockUseCreateLinkToken = mock(() => ({
  mutateAsync: mockCreateLinkTokenMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useExchangePublicToken ===
export const mockExchangePublicTokenMutateAsync = mock(() =>
  Promise.resolve({ access_token: 'test-access-token' })
)
export const mockUseExchangePublicToken = mock(() => ({
  mutateAsync: mockExchangePublicTokenMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useCreateReconnectLinkToken (Issue #1609) ===
export const mockCreateReconnectLinkTokenMutateAsync = mock(() =>
  Promise.resolve({ link_token: 'test-reconnect-link-token' })
)
export const mockUseCreateReconnectLinkToken = mock(() => ({
  mutateAsync: mockCreateReconnectLinkTokenMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useRepairLinkedItem (Issue #1609) ===
export const mockRepairLinkedItemMutateAsync = mock(() =>
  Promise.resolve({ success: true })
)
export const mockUseRepairLinkedItem = mock(() => ({
  mutateAsync: mockRepairLinkedItemMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/plaid/plaid', () => ({
  ...RealPlaidModule,
  useCreateLinkToken: mockUseCreateLinkToken,
  useExchangePublicToken: mockUseExchangePublicToken,
  useCreateReconnectLinkToken: mockUseCreateReconnectLinkToken,
  useRepairLinkedItem: mockUseRepairLinkedItem,
}))

// Mock react-plaid-link (browser API - not available in happy-dom)
// #1609: capture the options usePlaidLink is called with (a plain mock(), not
// a bare arrow function) so tests can retrieve the real onExit/onSuccess
// callbacks a component wired up and invoke them directly.
export const mockUsePlaidLink = mock((_options: Record<string, unknown>) => ({
  open: mock(() => {}),
  ready: true,
}))

mock.module('react-plaid-link', () => ({
  usePlaidLink: mockUsePlaidLink,
}))
