import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useCopyToClipboard } from './useCopyToClipboard'

const mockToastSuccess = mock(() => {})
const mockToastError = mock(() => {})

mock.module('sonner', () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}))


describe('useCopyToClipboard', () => {
  const originalClipboard = navigator.clipboard
  let mockWriteText: ReturnType<typeof mock>

  beforeEach(() => {
    mockToastSuccess.mockClear()
    mockToastError.mockClear()
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
    })
  })

  describe('when clipboard API is available', () => {
    beforeEach(() => {
      mockWriteText = mock(() => Promise.resolve(undefined))
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: mockWriteText },
        writable: true,
      })
    })

    it('copies text to clipboard successfully', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(mockWriteText).toHaveBeenCalledWith('test text')
      expect(result.current.copied).toBe(true)
      expect(result.current.error).toBe(null)
    })

    it('shows success toast with default message', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(mockToastSuccess).toHaveBeenCalledWith('Copied to clipboard!')
    })

    it('shows success toast with custom message', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text', 'User ID copied!')
      })

      expect(mockToastSuccess).toHaveBeenCalledWith('User ID copied!')
    })
  })

  describe('when clipboard API fails', () => {
    beforeEach(() => {
      mockWriteText = mock(() => Promise.reject(new Error('Clipboard error')))
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: mockWriteText },
        writable: true,
      })
    })

    it('handles clipboard error gracefully', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(result.current.copied).toBe(false)
      expect(result.current.error).toBe('Clipboard error')
    })

    it('shows error toast on failure', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(mockToastError).toHaveBeenCalledWith('Failed to copy to clipboard')
    })
  })

  describe('when clipboard API is not available', () => {
    beforeEach(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: undefined,
        writable: true,
      })
    })

    it('handles missing clipboard API', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(result.current.copied).toBe(false)
      expect(result.current.error).toBe('Clipboard API not available')
    })

    it('shows error toast when clipboard unavailable', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(mockToastError).toHaveBeenCalledWith('Failed to copy to clipboard')
    })
  })

  it('returns stable function references', () => {
    const { result, rerender } = renderHook(() => useCopyToClipboard())

    const firstCopy = result.current.copy
    rerender()
    const secondCopy = result.current.copy

    expect(firstCopy).toBe(secondCopy)
  })
})
