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
 *   ⌘Z/Ctrl+Z  → undo last action (when onUndo provided)
 *
 * Disabled when `enabled=false` (e.g. when a modal is open).
 */

import { useEffect } from 'react'

export interface UseTriageKeyboardOptions {
  onApprove: () => void
  onSkip: () => void
  onRecategorize: () => void
  onToggleSelect: () => void
  onNext: () => void
  onPrev: () => void
  onToggleHelp: () => void
  onClose: () => void
  onUndo?: () => void
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
  onUndo,
  enabled = true,
}: UseTriageKeyboardOptions): void {
  useEffect(() => {
    if (!enabled) return
    const keymap: Record<string, (() => void) | undefined> = {
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
      ...(onUndo ? { 'Meta+z': onUndo, 'Control+z': onUndo } : {}),
    }

    const handler = (event: KeyboardEvent) => {
      // Typing targets own all keys. Buttons retain native activation while letter
      // shortcuts still work after an editor restores focus to its opener.
      const targets = [event.target instanceof Element ? event.target : null, document.activeElement]
      const typing = 'input, textarea, select, [role="combobox"], [contenteditable]:not([contenteditable="false"])'
      if (targets.some(target => target?.closest(typing))) return
      if ((event.key === 'Enter' || event.key === ' ') && targets.some(target => target?.closest('button, a, [role="button"]'))) return
      if (event.defaultPrevented || event.repeat || event.altKey) return
      const key = event.metaKey ? `Meta+${event.key}` : event.ctrlKey ? `Control+${event.key}` : event.key
      const action = keymap[key]
      if (action) { event.preventDefault(); action() }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [enabled, onApprove, onSkip, onRecategorize, onToggleSelect, onNext, onPrev, onToggleHelp, onClose, onUndo])
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
  { key: '⌘Z / Ctrl+Z', description: 'Undo last action' },
] as const
