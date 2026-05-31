// Delete confirmation for a custom category with a live impact warning (Issue #1203).
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { useCategoryUsage } from '../hooks/useCategoryUsage'
import type { CustomCategory } from '../types'

interface DeleteCategoryDialogProps {
  category: CustomCategory | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  loading?: boolean
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

export function DeleteCategoryDialog({
  category,
  open,
  onOpenChange,
  onConfirm,
  loading = false,
}: DeleteCategoryDialogProps) {
  const { usage, isLoading, error } = useCategoryUsage(category?.id ?? null, open)

  const description = (() => {
    if (!category) return null
    if (isLoading) return 'Checking what uses this category…'
    if (error || !usage) {
      return (
        <span>
          Delete <strong data-ph-mask>{category.name}</strong>? Any transactions using it will
          revert to their bank category and be marked unreviewed.
        </span>
      )
    }
    return (
      <span>
        Deleting <strong data-ph-mask>{category.name}</strong> affects{' '}
        {plural(usage.transaction_count, 'transaction')}, {plural(usage.budget_count, 'budget')},{' '}
        {plural(usage.rule_count, 'rule')}
        {usage.child_count > 0 ? `, and ${plural(usage.child_count, 'sub-category')}` : ''}.
        <br />
        Those transactions revert to their bank category and become unreviewed; sub-categories
        move to top-level. This can&apos;t be undone.
      </span>
    )
  })()

  return (
    <ConfirmationDialog
      open={open}
      onOpenChange={onOpenChange}
      onConfirm={onConfirm}
      title="Delete category"
      description={description}
      variant="destructive"
      confirmLabel="Delete"
      loading={loading}
    />
  )
}
