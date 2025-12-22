import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useCopyToClipboard } from './useCopyToClipboard'

// Mock sonner toast
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

import { toast } from 'sonner'

describe('useCopyToClipboard', () => {
  const originalClipboard = navigator.clipboard

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    // Restore original clipboard
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
    })
  })

  describe('when clipboard API is available', () => {
    beforeEach(() => {
      const mockWriteText = vi.fn().mockResolvedValue(undefined)
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

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test text')
      expect(result.current.copied).toBe(true)
      expect(result.current.error).toBe(null)
    })

    it('shows success toast with default message', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(toast.success).toHaveBeenCalledWith('Copied to clipboard!')
    })

    it('shows success toast with custom message', async () => {
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text', 'User ID copied!')
      })

      expect(toast.success).toHaveBeenCalledWith('User ID copied!')
    })

    it('resets copied state after timeout', async () => {
      vi.useFakeTimers()
      const { result } = renderHook(() => useCopyToClipboard())

      await act(async () => {
        await result.current.copy('test text')
      })

      expect(result.current.copied).toBe(true)

      act(() => {
        vi.advanceTimersByTime(2000)
      })

      expect(result.current.copied).toBe(false)
      vi.useRealTimers()
    })
  })

  describe('when clipboard API fails', () => {
    beforeEach(() => {
      const mockWriteText = vi
        .fn()
        .mockRejectedValue(new Error('Clipboard error'))
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

      expect(toast.error).toHaveBeenCalledWith('Failed to copy to clipboard')
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

      expect(toast.error).toHaveBeenCalledWith('Failed to copy to clipboard')
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
