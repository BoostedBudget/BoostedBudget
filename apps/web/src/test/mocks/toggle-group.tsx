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
    'aria-label': ariaLabel,
  }: {
    children: React.ReactNode
    onValueChange?: (v: string) => void
    type?: 'single' | 'multiple'
    'aria-label'?: string
  }) => (
    <div
      // Mirror real Radix's roles: "radiogroup" for type="single", "toolbar" for
      // type="multiple" (verified against @radix-ui/react-toggle-group 1.1.19's dist).
      // 1.1.19 corrected the single-select container from role="group", which was invalid
      // ARIA over its role="radio" children. This mock previously hardcoded "group" for
      // both, so any role-based query would have asserted against markup production no
      // longer emits.
      role={type === 'multiple' ? 'toolbar' : 'radiogroup'}
      // aria-label must be forwarded: without it a "does this group have an accessible
      // name?" test can only assert the container exists, which passes even if production
      // drops the label entirely.
      aria-label={ariaLabel}
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
