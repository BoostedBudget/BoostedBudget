// Issue #575: Centralized accounts API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealAccountsModule from '@boostedbudget/api-client/endpoints/accounts/accounts'

// === useGetAccounts ===
export const mockRefetchAccounts = mock(() => Promise.resolve())
export const mockUseGetAccounts = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchAccounts,
}))

// === useReorderAccounts ===
export const mockReorderAccountsMutateAsync = mock(() => Promise.resolve({ success: true }))
export const mockReorderAccountsReset = mock(() => {})
export const mockUseReorderAccounts = mock(() => ({
  mutateAsync: mockReorderAccountsMutateAsync,
  isPending: false,
  isError: false,
  error: null,
  reset: mockReorderAccountsReset,
}))

// === useDisconnectAccount (Issue #1569, Task 6: primary, non-destructive) ===
export const mockDisconnectAccountMutateAsync = mock(() => Promise.resolve())
export const mockUseDisconnectAccount = mock(() => ({
  mutateAsync: mockDisconnectAccountMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === usePurgeAccount (Issue #1569, Task 6: destructive, renamed from useDeleteAccount) ===
export const mockPurgeAccountMutateAsync = mock(() => Promise.resolve())
export const mockUsePurgeAccount = mock(() => ({
  mutateAsync: mockPurgeAccountMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useUpdateAccount ===
export const mockUpdateAccountMutateAsync = mock(() =>
  Promise.resolve({ account: { id: 'acc-123', nickname: 'Updated' } })
)
export const mockUpdateAccountReset = mock(() => {})
export const mockUseUpdateAccount = mock(() => ({
  mutateAsync: mockUpdateAccountMutateAsync,
  isPending: false,
  isError: false,
  error: null,
  reset: mockUpdateAccountReset,
}))

// === useDisconnectInstitution ===
export const mockDisconnectInstitutionMutateAsync = mock(() => Promise.resolve())
export const mockUseDisconnectInstitution = mock(() => ({
  mutateAsync: mockDisconnectInstitutionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === usePurgeInstitution (Issue #1569, Task 6: destructive, renamed from useDisconnectInstitution's
// old DELETE-backed behavior) ===
export const mockPurgeInstitutionMutateAsync = mock(() => Promise.resolve())
export const mockUsePurgeInstitution = mock(() => ({
  mutateAsync: mockPurgeInstitutionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  ...RealAccountsModule,
  useGetAccounts: mockUseGetAccounts,
  useReorderAccounts: mockUseReorderAccounts,
  useDisconnectAccount: mockUseDisconnectAccount,
  usePurgeAccount: mockUsePurgeAccount,
  useUpdateAccount: mockUseUpdateAccount,
  useDisconnectInstitution: mockUseDisconnectInstitution,
  usePurgeInstitution: mockUsePurgeInstitution,
}))
