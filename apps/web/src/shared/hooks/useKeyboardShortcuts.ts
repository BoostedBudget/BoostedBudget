import { useEffect } from 'react'

/**
 * Map of key strings to handler callbacks.
 * Keys match KeyboardEvent.key values (e.g. 'e', 'Enter', 'Escape', 'ArrowUp').
 */
export type KeyMap = Record<string, () => void>

/**
 * Returns true if the currently focused element is a text-input target
 * where key presses should not trigger shortcuts.
 */
function isTypingTarget(el: Element | null): boolean {
  if (!el) return false
  const tag = (el as HTMLElement).tagName
  return (
    tag === 'INPUT' ||
    tag === 'TEXTAREA' ||
    tag === 'SELECT' ||
    (el as HTMLElement).isContentEditable
  )
}

/**
 * useKeyboardShortcuts — register document-level keyboard shortcuts.
 *
 * Automatically ignores keystrokes when the user is typing in an input,
 * textarea, select, or contentEditable element.  Cleans up the event listener
 * on unmount or when `keymap`/`enabled` change.
 *
 * @param keymap  Object mapping KeyboardEvent.key strings to handler functions.
 * @param enabled Optional flag to temporarily disable all shortcuts (default true).
 *
 * @example
 * useKeyboardShortcuts({
 *   e: () => approve(current),
 *   c: () => openRecategorize(),
 *   Escape: () => close(),
 * })
 */
export function useKeyboardShortcuts(keymap: KeyMap, enabled = true): void {
  useEffect(() => {
    if (!enabled) return

    const handler = (event: KeyboardEvent) => {
      if (isTypingTarget(document.activeElement)) return
      const key = event.metaKey ? `Meta+${event.key}` : event.ctrlKey ? `Control+${event.key}` : event.key
      const action = keymap[key]
      if (action) {
        event.preventDefault()
        action()
      }
    }

    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [keymap, enabled])
}
