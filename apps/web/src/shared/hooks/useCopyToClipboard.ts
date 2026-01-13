import { useState, useCallback, useRef, useEffect } from 'react'
import { toast } from 'sonner'

interface UseCopyToClipboardReturn {
  /**
   * Copy text to clipboard
   * @param text - The text to copy
   * @param successMessage - Optional custom success message
   */
  copy: (text: string, successMessage?: string) => Promise<void>
  /** Whether the text was recently copied successfully */
  copied: boolean
  /** Error message if copy failed */
  error: string | null
}

/**
 * Hook for copying text to clipboard with toast notifications
 *
 * @returns Object with copy function, copied state, and error state
 *
 * @example
 * ```tsx
 * const { copy, copied } = useCopyToClipboard()
 *
 * <button onClick={() => copy(userId, 'User ID copied!')}>
 *   {copied ? 'Copied!' : 'Copy'}
 * </button>
 * ```
 */
export function useCopyToClipboard(): UseCopyToClipboardReturn {
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Clean up timeout on unmount
  useEffect(() => {
    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current)
      }
    }
  }, [])

  const copy = useCallback(
    async (text: string, successMessage = 'Copied to clipboard!') => {
      // Clear any existing timeout
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current)
      }

      // Check if clipboard API is available
      if (!navigator.clipboard) {
        setError('Clipboard API not available')
        setCopied(false)
        toast.error('Failed to copy to clipboard')
        return
      }

      try {
        await navigator.clipboard.writeText(text)
        setCopied(true)
        setError(null)
        toast.success(successMessage)

        // Reset copied state after 2 seconds
        timeoutRef.current = setTimeout(() => {
          setCopied(false)
        }, 2000)
      } catch (err) {
        const errorMessage =
          err instanceof Error ? err.message : 'Unknown error'
        setError(errorMessage)
        setCopied(false)
        toast.error('Failed to copy to clipboard')
      }
    },
    []
  )

  return { copy, copied, error }
}
