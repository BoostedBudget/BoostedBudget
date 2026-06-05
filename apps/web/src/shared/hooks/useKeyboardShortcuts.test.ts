import { describe, it, expect, mock, afterEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { act } from '@testing-library/react'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'

// Simulate keyboard events on document
function fireKeydown(key: string, target?: Element) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true })
  if (target) {
    Object.defineProperty(event, 'target', { value: target })
    // Also override document.activeElement to simulate focus
    Object.defineProperty(document, 'activeElement', { value: target, configurable: true })
  }
  document.dispatchEvent(event)
}

describe('useKeyboardShortcuts', () => {
  afterEach(() => {
    // Remove any instance-level shadow on document.activeElement to restore native getter.
    // Object.defineProperty(document, 'activeElement', ...) replaces the native getter
    // with a static data property — deleting it restores the prototype getter.
    try {
      delete (document as unknown as Record<string, unknown>).activeElement
    } catch {
      // Not configurable; nothing to clean up
    }
  })

  it('calls the handler when the mapped key is pressed', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    act(() => fireKeydown('e'))

    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('does not call handler for unmapped key', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    act(() => fireKeydown('x'))

    expect(handler).not.toHaveBeenCalled()
  })

  it('calls the correct handler for each key', () => {
    const onE = mock(() => {})
    const onC = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: onE, c: onC }))

    act(() => fireKeydown('e'))
    act(() => fireKeydown('c'))

    expect(onE).toHaveBeenCalledTimes(1)
    expect(onC).toHaveBeenCalledTimes(1)
  })

  it('ignores keydown when an input element is focused', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    input.blur()
    document.body.removeChild(input)
  })

  it('ignores keydown when a textarea element is focused', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    const textarea = document.createElement('textarea')
    document.body.appendChild(textarea)
    textarea.focus()

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    textarea.blur()
    document.body.removeChild(textarea)
  })

  it('ignores keydown when a select element is focused', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    const select = document.createElement('select')
    document.body.appendChild(select)
    select.focus()

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    select.blur()
    document.body.removeChild(select)
  })

  it('does not call handler when enabled=false', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }, false))

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
  })

  it('does not fire shortcuts when altKey is held without meta/ctrl', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ ArrowLeft: handler }))

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowLeft', altKey: true, bubbles: true })
      )
    })

    expect(handler).not.toHaveBeenCalled()
  })

  it('fires Meta+z shortcut even when altKey is also held', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ 'Meta+z': handler }))

    act(() => {
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'z', altKey: true, metaKey: true, bubbles: true })
      )
    })

    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('removes event listener on unmount', () => {
    const handler = mock(() => {})
    const { unmount } = renderHook(() => useKeyboardShortcuts({ e: handler }))

    unmount()
    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
  })
})
