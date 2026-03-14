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

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/plaid/plaid', () => ({
  ...RealPlaidModule,
  useCreateLinkToken: mockUseCreateLinkToken,
  useExchangePublicToken: mockUseExchangePublicToken,
}))

// Mock react-plaid-link (browser API - not available in happy-dom)
mock.module('react-plaid-link', () => ({
  usePlaidLink: () => ({ open: mock(() => {}), ready: true }),
}))
