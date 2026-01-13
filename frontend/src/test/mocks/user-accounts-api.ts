// Issue #575: Centralized user-accounts API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealUserAccountsModule from '@/api/generated/endpoints/user-accounts/user-accounts'

// === useUpdateUserAccount ===
export const mockUpdateUserAccountMutateAsync = mock(() =>
  Promise.resolve({
    id: 'ua-789',
    nickname: 'Updated Name',
    custom_color: '#FF0000',
  })
)
export const mockUseUpdateUserAccount = mock(() => ({
  mutateAsync: mockUpdateUserAccountMutateAsync,
  isPending: false,
  isError: false,
  error: null,
  reset: mock(() => {}),
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/user-accounts/user-accounts', () => ({
  ...RealUserAccountsModule,
  useUpdateUserAccount: mockUseUpdateUserAccount,
}))
