/**
 * CustomCategoryFilter — toggle chips that scope the spending report to specific custom
 * categories (Issue #1203). Selecting a parent rolls up its children server-side. Renders
 * nothing when the user has no custom categories.
 */
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { CustomCategory } from '@boostedbudget/api-client/models'
import { cn } from '@/lib/utils'
import { CategoryIcon } from '@/shared/components/IconPicker/CategoryIcon'
import { buildCategoryHierarchy } from '@/features/categories/utils/buildCategoryHierarchy'

interface CustomCategoryFilterProps {
  selectedIds: string[]
  onChange: (ids: string[]) => void
  className?: string
}

export function CustomCategoryFilter({ selectedIds, onChange, className }: CustomCategoryFilterProps) {
  const query = useGetUserCategories()
  const cats = (Array.isArray(query?.data) ? query.data : []) as CustomCategory[]
  if (cats.length === 0) return null

  // Order as parents (depth 0) each followed by their custom children, then PFC-nested
  // customs — the same hierarchy order used elsewhere (#1203).
  const { topLevel, pfcGroups } = buildCategoryHierarchy(cats as Parameters<typeof buildCategoryHierarchy>[0])
  const ordered: { cat: CustomCategory; depth: number }[] = []
  for (const g of topLevel) {
    ordered.push({ cat: g.parent as unknown as CustomCategory, depth: 0 })
    for (const c of g.children) ordered.push({ cat: c as unknown as CustomCategory, depth: 1 })
  }
  for (const pg of pfcGroups) {
    for (const c of pg.categories) ordered.push({ cat: c as unknown as CustomCategory, depth: 0 })
  }

  const toggle = (id: string) => {
    onChange(selectedIds.includes(id) ? selectedIds.filter((x) => x !== id) : [...selectedIds, id])
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-2', className)} role="group" aria-label="Filter by custom category">
      <span className="text-sm font-medium text-muted-foreground">Custom categories:</span>
      {ordered.map(({ cat, depth }) => {
        const active = selectedIds.includes(cat.id)
        return (
          <button
            key={cat.id}
            type="button"
            aria-pressed={active}
            onClick={() => toggle(cat.id)}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm transition-colors',
              depth === 1 && 'ml-1',
              active
                ? 'border-primary bg-primary/10 text-primary'
                : 'border-border bg-card text-foreground hover:bg-accent'
            )}
          >
            <CategoryIcon name={cat.icon ?? ''} className="h-3.5 w-3.5" />
            <span data-ph-mask>{cat.name}</span>
          </button>
        )
      })}
      {selectedIds.length > 0 && (
        <button
          type="button"
          onClick={() => onChange([])}
          className="text-sm text-muted-foreground underline-offset-2 hover:underline"
        >
          Clear
        </button>
      )}
    </div>
  )
}
