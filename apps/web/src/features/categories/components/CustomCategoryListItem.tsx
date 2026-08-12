// A single row in the custom category manager (Issue #1203).
import type { KeyboardEvent } from 'react'
import { cva } from 'class-variance-authority'
import { Pencil, Trash2 } from 'lucide-react'
import { CategoryIcon } from '@/shared/components/IconPicker'
import type { CustomCategory } from '../types'

const itemVariants = cva('flex items-center gap-3 rounded-md px-3 py-2', {
  variants: {
    variant: {
      primary: 'bg-card',
      child: 'ml-6 bg-muted/40',
    },
  },
  defaultVariants: { variant: 'primary' },
})

interface CustomCategoryListItemProps {
  category: CustomCategory
  variant?: 'primary' | 'child'
  onEdit: (category: CustomCategory) => void
  onDelete: (category: CustomCategory) => void
  /** Roving keyboard navigation (Issue #1203). */
  onKeyDown?: (e: KeyboardEvent, category: CustomCategory) => void
  itemRef?: (el: HTMLDivElement | null) => void
}

export function CustomCategoryListItem({
  category,
  variant = 'primary',
  onEdit,
  onDelete,
  onKeyDown,
  itemRef,
}: CustomCategoryListItemProps) {
  return (
    <div
      className={itemVariants({ variant })}
      role="listitem"
      tabIndex={0}
      ref={itemRef}
      onKeyDown={
        onKeyDown
          ? (e) => {
              // Only handle keys on the row itself — ignore events bubbling up from the
              // nested edit/delete buttons so Enter/Delete there don't also fire row actions.
              if (e.target !== e.currentTarget) return
              onKeyDown(e, category)
            }
          : undefined
      }
    >
      <span
        className="inline-flex h-7 w-7 items-center justify-center rounded-full"
        style={{ backgroundColor: category.color ?? '#6B7280', color: '#fff' }}
        aria-hidden
      >
        <CategoryIcon name={category.icon} className="h-4 w-4" />
      </span>
      <span className="flex-1 truncate text-sm" data-ph-mask>
        {category.name}
      </span>
      <button
        type="button"
        onClick={() => onEdit(category)}
        // PII: aria-label is recorded verbatim by session replay; data-ph-mask only covers
        // text content, not attributes. aria-labelledby stores only the element id in the
        // attribute; the accessible name ("Edit {name}") is computed at read time from the
        // sr-only span below, so the name never appears as attribute text.
        aria-labelledby={`category-edit-label-${category.id}`}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <span id={`category-edit-label-${category.id}`} className="sr-only" data-ph-mask>{`Edit ${category.name}`}</span>
        <Pencil className="h-4 w-4" aria-hidden />
      </button>
      <button
        type="button"
        onClick={() => onDelete(category)}
        aria-labelledby={`category-delete-label-${category.id}`}
        className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
      >
        <span id={`category-delete-label-${category.id}`} className="sr-only" data-ph-mask>{`Delete ${category.name}`}</span>
        <Trash2 className="h-4 w-4" aria-hidden />
      </button>
    </div>
  )
}
