/**
 * IconPicker lets the user choose a curated icon for a custom category (Issue #1203).
 * Renders a searchable grid of icons inside a popover. Keyboard accessible: each icon
 * is a button with aria-pressed reflecting selection.
 */
import { useMemo, useState } from 'react'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { CategoryIcon } from './CategoryIcon'
import { CATEGORY_ICON_KEYS } from './icons'

interface IconPickerProps {
  value?: string | null
  onChange: (iconKey: string) => void
  disabled?: boolean
  'aria-label'?: string
  className?: string
}

/** Human-readable label for an icon key (e.g. "shopping-cart" → "shopping cart"). */
function iconLabel(key: string): string {
  return key.replace(/-/g, ' ')
}

export function IconPicker({
  value,
  onChange,
  disabled,
  'aria-label': ariaLabel = 'Choose an icon',
  className,
}: IconPickerProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')

  const visibleKeys = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return CATEGORY_ICON_KEYS
    return CATEGORY_ICON_KEYS.filter((k) => iconLabel(k).includes(q))
  }, [query])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        type="button"
        disabled={disabled}
        aria-label={ariaLabel}
        className={cn(
          'inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-foreground hover:bg-accent disabled:opacity-50',
          className
        )}
      >
        <CategoryIcon name={value} className="h-4 w-4" />
      </PopoverTrigger>
      <PopoverContent className="w-64 p-2" align="start">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search icons…"
          aria-label="Search icons"
          className="mb-2 w-full rounded-md border border-input bg-background px-2 py-1 text-sm"
        />
        <div role="grid" aria-label="Icons" className="grid grid-cols-6 gap-1">
          {visibleKeys.map((key) => {
            const selected = key === value
            return (
              <button
                key={key}
                type="button"
                role="gridcell"
                aria-pressed={selected}
                aria-label={iconLabel(key)}
                title={iconLabel(key)}
                onClick={() => {
                  onChange(key)
                  setOpen(false)
                }}
                className={cn(
                  'inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  selected && 'bg-primary text-primary-foreground hover:bg-primary/90'
                )}
              >
                <CategoryIcon name={key} className="h-4 w-4" />
              </button>
            )
          })}
          {visibleKeys.length === 0 && (
            <p className="col-span-6 px-1 py-2 text-sm text-muted-foreground">No icons match.</p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
