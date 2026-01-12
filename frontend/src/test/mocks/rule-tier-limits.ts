// Issue #575: Centralized useRuleTierLimits wrapper hook mock
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

// Default mock values - can be configured per test via mockReturnValue
export const mockUseRuleTierLimits = mock(() => ({
  ruleLimit: 5,
  isUnlimited: false,
  currentRuleCount: 1,
  isAtLimit: false,
  canUseEventTriggers: true,
  shouldShowUpgradeBanner: false,
  rulesCountDisplay: '1/5 rules used',
}))

// Single mock.module() call - called once at preload time
mock.module('@/features/rules/hooks/useRuleTierLimits', () => ({
  useRuleTierLimits: (...args: unknown[]) => mockUseRuleTierLimits(...args),
}))
