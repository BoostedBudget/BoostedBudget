import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
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
  let originalActiveElement: Element | null

  beforeEach(() => {
    originalActiveElement = document.activeElement
  })

  afterEach(() => {
    // Restore activeElement to body
    Object.defineProperty(document, 'activeElement', {
      value: originalActiveElement,
      configurable: true,
    })
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
    Object.defineProperty(document, 'activeElement', { value: input, configurable: true })

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    document.body.removeChild(input)
  })

  it('ignores keydown when a textarea element is focused', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    const textarea = document.createElement('textarea')
    document.body.appendChild(textarea)
    Object.defineProperty(document, 'activeElement', { value: textarea, configurable: true })

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    document.body.removeChild(textarea)
  })

  it('ignores keydown when a select element is focused', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }))

    const select = document.createElement('select')
    document.body.appendChild(select)
    Object.defineProperty(document, 'activeElement', { value: select, configurable: true })

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
    document.body.removeChild(select)
  })

  it('does not call handler when enabled=false', () => {
    const handler = mock(() => {})
    renderHook(() => useKeyboardShortcuts({ e: handler }, false))

    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
  })

  it('removes event listener on unmount', () => {
    const handler = mock(() => {})
    const { unmount } = renderHook(() => useKeyboardShortcuts({ e: handler }))

    unmount()
    act(() => fireKeydown('e'))

    expect(handler).not.toHaveBeenCalled()
  })
})
