/**
 * useTriageKeyboard — wires keyboard shortcuts for the one-at-a-time triage view.
 *
 * Keys:
 *   e / Enter  → approve (mark reviewed, advance)
 *   s          → skip (advance without marking)
 *   c          → open recategorize panel
 *   x          → toggle row selection for bulk actions
 *   ArrowRight / n → next transaction
 *   ArrowLeft  / p → previous transaction
 *   ?          → toggle keyboard shortcuts help dialog
 *   Escape     → close triage view / help dialog
 *
 * Disabled when `enabled=false` (e.g. when a modal is open).
 */

import { useKeyboardShortcuts } from '@/shared/hooks/useKeyboardShortcuts'

export interface UseTriageKeyboardOptions {
  onApprove: () => void
  onSkip: () => void
  onRecategorize: () => void
  onToggleSelect: () => void
  onNext: () => void
  onPrev: () => void
  onToggleHelp: () => void
  onClose: () => void
  enabled?: boolean
}

export function useTriageKeyboard({
  onApprove,
  onSkip,
  onRecategorize,
  onToggleSelect,
  onNext,
  onPrev,
  onToggleHelp,
  onClose,
  enabled = true,
}: UseTriageKeyboardOptions): void {
  const keymap = {
    e: onApprove,
    Enter: onApprove,
    s: onSkip,
    c: onRecategorize,
    x: onToggleSelect,
    ArrowRight: onNext,
    n: onNext,
    ArrowLeft: onPrev,
    p: onPrev,
    '?': onToggleHelp,
    Escape: onClose,
  }

  useKeyboardShortcuts(keymap, enabled)
}

/** Canonical shortcut definitions for the help dialog */
export const TRIAGE_SHORTCUTS = [
  { key: 'e / Enter', description: 'Approve (mark reviewed, advance)' },
  { key: 's', description: 'Skip (advance without marking)' },
  { key: 'c', description: 'Recategorize' },
  { key: 'x', description: 'Toggle selection' },
  { key: '→ / n', description: 'Next transaction' },
  { key: '← / p', description: 'Previous transaction' },
  { key: '?', description: 'Toggle keyboard shortcuts' },
  { key: 'Esc', description: 'Close triage / help' },
] as const
