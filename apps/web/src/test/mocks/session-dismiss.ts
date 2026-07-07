// Issue #898: Centralized useSessionDismiss mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

export const mockDismiss = mock(() => {})

export const mockUseSessionDismiss = mock(() => ({
  isDismissed: false,
  dismiss: mockDismiss,
}))

mock.module('@/shared/hooks/useSessionDismiss', () => ({
  useSessionDismiss: mockUseSessionDismiss,
}))
