/**
 * ColorPicker Component
 *
 * A color picker component for selecting account custom colors.
 * Uses react-colorful for the picker and includes preset color swatches.
 *
 * Issue #307: Account Customization
 */

import { useState, useCallback } from 'react'
import { HexColorPicker } from 'react-colorful'
import { cn } from '@/lib/utils'
import { X } from 'lucide-react'

// Preset colors for quick selection
const PRESET_COLORS = [
  '#EF4444', // Red
  '#F97316', // Orange
  '#F59E0B', // Amber
  '#EAB308', // Yellow
  '#84CC16', // Lime
  '#22C55E', // Green
  '#10B981', // Emerald
  '#14B8A6', // Teal
  '#06B6D4', // Cyan
  '#0EA5E9', // Sky
  '#3B82F6', // Blue
  '#6366F1', // Indigo
  '#8B5CF6', // Violet
  '#A855F7', // Purple
  '#D946EF', // Fuchsia
  '#EC4899', // Pink
  '#F43F5E', // Rose
  '#64748B', // Slate
]

export interface ColorPickerProps {
  /** Current color value (hex format #RRGGBB) */
  value?: string | null
  /** Callback when color changes */
  onChange: (color: string | null) => void
  /** Additional class name for the container */
  className?: string
  /** Label for accessibility */
  'aria-label'?: string
}

/**
 * ColorPicker component for selecting colors
 *
 * @example
 * ```tsx
 * <ColorPicker
 *   value={account.custom_color}
 *   onChange={(color) => setColor(color)}
 * />
 * ```
 */
export function ColorPicker({
  value,
  onChange,
  className,
  'aria-label': ariaLabel = 'Pick a color',
}: ColorPickerProps) {
  // Use a default color for the picker when value is null/undefined
  const [internalColor, setInternalColor] = useState(value || '#3B82F6')

  const handleColorChange = useCallback((color: string) => {
    setInternalColor(color)
    onChange(color)
  }, [onChange])

  const handleClear = useCallback(() => {
    onChange(null)
  }, [onChange])

  const handlePresetClick = useCallback((color: string) => {
    setInternalColor(color)
    onChange(color)
  }, [onChange])

  return (
    <div className={cn('space-y-3', className)} aria-label={ariaLabel}>
      {/* Color picker */}
      <div className="flex justify-center">
        <HexColorPicker
          color={value || internalColor}
          onChange={handleColorChange}
          style={{ width: '100%', maxWidth: '200px', height: '150px' }}
        />
      </div>

      {/* Current color preview and hex input */}
      <div className="flex items-center gap-2">
        <div
          className="w-8 h-8 rounded-md border border-border shrink-0"
          style={{ backgroundColor: value || internalColor }}
          aria-label="Current color preview"
        />
        <input
          type="text"
          value={(value || internalColor).toUpperCase()}
          onChange={(e) => {
            const hex = e.target.value
            if (/^#[0-9A-Fa-f]{6}$/.test(hex)) {
              handleColorChange(hex.toUpperCase())
            }
          }}
          className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 font-mono"
          placeholder="#RRGGBB"
          maxLength={7}
          aria-label="Hex color input"
        />
        {value && (
          <button
            type="button"
            onClick={handleClear}
            className="inline-flex items-center justify-center h-9 w-9 rounded-md border border-input bg-transparent hover:bg-accent hover:text-accent-foreground shrink-0"
            aria-label="Clear color"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Preset color swatches */}
      <div className="space-y-1.5">
        <span className="text-xs text-muted-foreground">Presets</span>
        <div className="grid grid-cols-9 gap-1.5">
          {PRESET_COLORS.map((color) => (
            <button
              key={color}
              type="button"
              onClick={() => handlePresetClick(color)}
              className={cn(
                'w-6 h-6 rounded-md border-2 transition-transform hover:scale-110 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-1',
                (value || internalColor).toUpperCase() === color.toUpperCase()
                  ? 'border-foreground ring-2 ring-ring ring-offset-1'
                  : 'border-transparent'
              )}
              style={{ backgroundColor: color }}
              aria-label={`Select color ${color}`}
              aria-pressed={(value || internalColor).toUpperCase() === color.toUpperCase()}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
