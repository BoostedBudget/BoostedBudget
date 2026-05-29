// A single row in the custom category manager (Issue #1203).
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
}

export function CustomCategoryListItem({
  category,
  variant = 'primary',
  onEdit,
  onDelete,
}: CustomCategoryListItemProps) {
  return (
    <div className={itemVariants({ variant })} role="listitem">
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
        aria-label={`Edit ${category.name}`}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <Pencil className="h-4 w-4" aria-hidden />
      </button>
      <button
        type="button"
        onClick={() => onDelete(category)}
        aria-label={`Delete ${category.name}`}
        className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
      >
        <Trash2 className="h-4 w-4" aria-hidden />
      </button>
    </div>
  )
}
