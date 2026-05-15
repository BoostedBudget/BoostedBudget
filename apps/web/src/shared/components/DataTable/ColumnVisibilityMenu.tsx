import { useState, useEffect, useRef } from 'react'
import { cn } from '@/lib/utils'
import type { ColumnDef } from './types'

interface ColumnVisibilityMenuProps<T> {
  columns: ColumnDef<T>[]
  visibility: Record<string, boolean>
  onChange: (visibility: Record<string, boolean>) => void
}

export function ColumnVisibilityMenu<T,>({
  columns,
  visibility,
  onChange,
}: ColumnVisibilityMenuProps<T>) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  const hideableColumns = columns.filter((col) => col.hideable)

  function isVisible(colId: string): boolean {
    return visibility[colId] !== false
  }

  function toggle(colId: string) {
    onChange({ ...visibility, [colId]: !isVisible(colId) })
  }

  // Close on outside click
  useEffect(() => {
    if (!open) return
    function handleClickOutside(e: MouseEvent) {
      if (
        menuRef.current &&
        !menuRef.current.contains(e.target as Node) &&
        triggerRef.current &&
        !triggerRef.current.contains(e.target as Node)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [open])

  // Keyboard: Escape closes
  function handleMenuKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      setOpen(false)
      triggerRef.current?.focus()
    }
  }

  return (
    <div className="relative inline-block">
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((o) => !o)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-3 py-1.5',
          'text-sm font-medium text-foreground transition-colors hover:bg-muted',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
        )}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M9 3H5a2 2 0 0 0-2 2v4m6-6h10a2 2 0 0 1 2 2v4M9 3v18m0 0h10a2 2 0 0 0 2-2V9M9 21H5a2 2 0 0 1-2-2V9m0 0h18" />
        </svg>
        Columns
      </button>
      {open && (
        <div
          ref={menuRef}
          role="menu"
          onKeyDown={handleMenuKeyDown}
          className="absolute right-0 z-50 mt-1 min-w-[160px] rounded-md border border-border bg-popover p-1 shadow-md"
        >
          {hideableColumns.map((col) => {
            const checked = isVisible(col.id)
            return (
              <button
                key={col.id}
                type="button"
                role="menuitemcheckbox"
                aria-checked={checked}
                onClick={() => toggle(col.id)}
                className={cn(
                  'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm',
                  'text-popover-foreground transition-colors hover:bg-accent hover:text-accent-foreground',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                )}
              >
                <span
                  className={cn(
                    'flex h-4 w-4 items-center justify-center rounded-sm border border-border',
                    checked && 'bg-primary border-primary text-primary-foreground'
                  )}
                  aria-hidden="true"
                >
                  {checked && (
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="10"
                      height="10"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="3"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  )}
                </span>
                {typeof col.header === 'string' ? col.header : col.id}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
