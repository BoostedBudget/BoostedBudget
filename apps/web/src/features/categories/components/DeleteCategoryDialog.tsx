// Delete confirmation for a custom category with a live impact warning (Issue #1203).
//
// #1569 Task 12 gap 3. Task 9 replaced the five-step destructive delete with
// customCategoryRepository.DeleteWithReassignment, and this copy was left describing the old
// behaviour. Every sentence below now corresponds to a numbered step in that function:
//
//   1. the category is tombstoned                    -> "removed from your lists"
//   2. rules targeting it are set enabled = false    -> "turned off (not deleted)"
//   3. children get parent_id = NULL                 -> "move to the top level"
//   4. budget_categories rows are tombstoned         -> "budget lines ... are removed"
//   -- `transactions` is touched in NO step          -> "your transactions keep this category"
//
// That last line is the one that matters most. The previous copy said transactions "will revert
// to their bank category and be marked unreviewed", which was the most alarming claim in the
// dialog and, after task 9, simply untrue: the read at transaction_repository.go:526 resolves the
// name with no deleted_at filter, so a historical transaction keeps both its assignment and the
// label the user sees.
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

/**
 * The consequences that actually apply, in the order the user cares about. A count of zero
 * produces no sentence at all: telling someone their rules were turned off when they have no
 * rules is the same class of error as the stale copy this replaces.
 */
function sideEffectSentences(usage: {
  budget_count: number
  rule_count: number
  child_count: number
}): string[] {
  const out: string[] = []
  if (usage.rule_count > 0) {
    out.push(`${plural(usage.rule_count, 'rule')} that assigned it are turned off, not deleted — you can point them somewhere else and switch them back on.`)
  }
  if (usage.budget_count > 0) {
    out.push(
      `${plural(usage.budget_count, 'budget line')} using it ${usage.budget_count === 1 ? 'is' : 'are'} removed from your budgets.`,
    )
  }
  if (usage.child_count > 0) {
    out.push(`${plural(usage.child_count, 'sub-category')} move to the top level.`)
  }
  return out
}

// True whatever the counts turn out to be, so the fallback can say it too.
const HISTORY_IS_SAFE =
  'Your transactions keep this category and stay as you reviewed them — nothing in your history changes.'

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
          Delete <strong data-ph-mask>{category.name}</strong>? {HISTORY_IS_SAFE} Any rules that
          assigned it are turned off rather than deleted, and any budget lines using it are
          removed.
        </span>
      )
    }
    const effects = sideEffectSentences(usage)
    return (
      <span>
        Deleting <strong data-ph-mask>{category.name}</strong> affects{' '}
        {plural(usage.transaction_count, 'transaction')}, {plural(usage.budget_count, 'budget')},{' '}
        {plural(usage.rule_count, 'rule')}
        {usage.child_count > 0 ? `, and ${plural(usage.child_count, 'sub-category')}` : ''}.
        <br />
        {HISTORY_IS_SAFE}
        {/*
          Spans, not a <ul>. Radix's DialogDescription renders a <p>, and only phrasing content is
          legal inside one — a <ul> there is invalid HTML and logs a validateDOMNesting error every
          time the dialog opens. `display: block` on a span is a CSS concern and does not affect
          validity, so the lines still read as a list without claiming to be one. The bullet is a
          literal character and is aria-hidden so it is not announced.
        */}
        {effects.map((line) => (
          <span key={line} className="mt-1 block pl-4 -indent-4">
            <span aria-hidden="true">• </span>
            {line}
          </span>
        ))}
        <span className="mt-2 block">
          The category itself is removed from your lists and can&apos;t be restored.
        </span>
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
