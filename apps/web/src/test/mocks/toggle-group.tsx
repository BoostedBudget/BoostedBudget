/**
 * Centralized mock for @/components/ui/toggle-group
 *
 * Issue #1323: Centralized to prevent mock.module() pollution between test files.
 *
 * Uses event-delegation on [data-value] attributes so onValueChange fires when
 * a ToggleGroupItem is clicked. This matches the behavior of the real Radix
 * ToggleGroup for the subset of interactions needed by unit tests (fireEvent.click).
 *
 * NOTE: toggle-group.test.tsx tests the REAL ToggleGroup implementation and
 * runs via test-conflict-files.sh (from /tmp to bypass bunfig.toml preloads).
 */

import React from 'react'
import { mock } from 'bun:test'

mock.module('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({
    children,
    onValueChange,
    type,
  }: {
    children: React.ReactNode
    onValueChange?: (v: string) => void
    type?: 'single' | 'multiple'
  }) => (
    <div
      // Mirror real Radix: type="single" is a radiogroup, type="multiple" is a group.
      // @radix-ui/react-toggle-group 1.1.19 corrected the single-select container from
      // role="group" (invalid ARIA over role="radio" children) to role="radiogroup". This
      // mock hardcoded "group" for both, so it would have kept reporting the pre-1.1.19 DOM
      // to any role-based query. No consumer test queries this role today — they go through
      // data-testid / text — but leaving the mock stale would let a future role query silently
      // assert against markup that production no longer emits.
      role={type === 'multiple' ? 'group' : 'radiogroup'}
      data-testid="toggle-group"
      onClick={(e: React.MouseEvent<HTMLDivElement>) => {
        const btn = (e.target as HTMLElement).closest('[data-value]') as HTMLElement | null
        if (btn?.dataset.value) onValueChange?.(btn.dataset.value)
      }}
    >
      {children}
    </div>
  ),
  ToggleGroupItem: ({
    children,
    value,
  }: {
    children: React.ReactNode
    value: string
  }) => (
    <button data-value={value} type="button">
      {children}
    </button>
  ),
}))
