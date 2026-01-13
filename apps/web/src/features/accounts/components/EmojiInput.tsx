/**
 * EmojiInput Component
 *
 * A text input for entering a single emoji for account customization.
 * Includes preview and clear functionality.
 *
 * Issue #307: Account Customization
 */

import { useState, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { X, Smile } from 'lucide-react'

// Suggested emojis for quick selection
const SUGGESTED_EMOJIS = [
  '💰', '💵', '💳', '🏦', '🏠',
  '🚗', '✈️', '🛒', '🎁', '☕',
  '🍽️', '🎓', '💼', '📈', '📉',
  '⭐', '❤️', '🔒', '🔓', '🐷',
]

export interface EmojiInputProps {
  /** Current emoji value */
  value?: string | null
  /** Callback when emoji changes */
  onChange: (emoji: string | null) => void
  /** Additional class name for the container */
  className?: string
  /** Placeholder text */
  placeholder?: string
}

/**
 * EmojiInput component for entering custom emojis
 *
 * @example
 * ```tsx
 * <EmojiInput
 *   value={account.custom_emoji}
 *   onChange={(emoji) => setEmoji(emoji)}
 * />
 * ```
 */
export function EmojiInput({
  value,
  onChange,
  className,
  placeholder = 'Enter emoji',
}: EmojiInputProps) {
  const [inputValue, setInputValue] = useState(value || '')

  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value
    // Limit to 10 characters (allowing for multi-codepoint emojis)
    if (newValue.length <= 10) {
      setInputValue(newValue)
      onChange(newValue || null)
    }
  }, [onChange])

  const handleClear = useCallback(() => {
    setInputValue('')
    onChange(null)
  }, [onChange])

  const handleSuggestedClick = useCallback((emoji: string) => {
    setInputValue(emoji)
    onChange(emoji)
  }, [onChange])

  return (
    <div className={cn('space-y-3', className)}>
      {/* Input with preview */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Smile className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
          <input
            type="text"
            value={inputValue}
            onChange={handleInputChange}
            className="flex h-10 w-full rounded-md border border-input bg-transparent pl-10 pr-10 py-2 text-base shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            placeholder={placeholder}
            aria-label="Emoji input"
          />
          {value && (
            <button
              type="button"
              onClick={handleClear}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded-sm hover:bg-accent transition-colors"
              aria-label="Clear emoji"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>

        {/* Preview */}
        {value && (
          <div
            className="flex items-center justify-center h-10 w-10 rounded-md border border-input bg-muted/50 text-2xl"
            aria-label="Emoji preview"
          >
            {value}
          </div>
        )}
      </div>

      {/* Suggested emojis */}
      <div className="space-y-1.5">
        <span className="text-xs text-muted-foreground">Suggestions</span>
        <div className="flex flex-wrap gap-1.5">
          {SUGGESTED_EMOJIS.map((emoji) => (
            <button
              key={emoji}
              type="button"
              onClick={() => handleSuggestedClick(emoji)}
              className={cn(
                'flex items-center justify-center w-8 h-8 rounded-md border text-lg transition-transform hover:scale-110 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-1',
                value === emoji
                  ? 'border-primary bg-primary/10'
                  : 'border-input hover:bg-accent'
              )}
              aria-label={`Select emoji ${emoji}`}
              aria-pressed={value === emoji}
            >
              {emoji}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
