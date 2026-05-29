// Create/edit form for a custom category (Issue #1203).
// A category is parented in exactly one way: none, under a PFC primary, or under a
// custom primary — modeled as a single select to keep the modes mutually exclusive.
import { useMemo, useState, type FormEvent } from 'react'
import { useCuratedPfcDetailed } from '@/shared/categories/pfcOptions'
import { IconPicker } from '@/shared/components/IconPicker'
import { eligibleCustomParents } from '../utils/buildCategoryHierarchy'
import type { CreateCategoryRequest, CustomCategory, UpdateCategoryRequest } from '../types'

interface CustomCategoryFormProps {
  /** When present, the form edits this category; otherwise it creates a new one. */
  category?: CustomCategory
  /** All of the user's custom categories, used to populate the custom-parent options. */
  allCategories: CustomCategory[]
  onSubmit: (req: CreateCategoryRequest | UpdateCategoryRequest) => Promise<void>
  onCancel: () => void
  isPending?: boolean
}

const PARENT_NONE = 'none'

export function CustomCategoryForm({
  category,
  allCategories,
  onSubmit,
  onCancel,
  isPending = false,
}: CustomCategoryFormProps) {
  const isEdit = !!category
  const [name, setName] = useState(category?.name ?? '')
  const [color, setColor] = useState(category?.color ?? '#6B7280')
  const [icon, setIcon] = useState<string | null>(category?.icon ?? null)
  const [error, setError] = useState<string | null>(null)

  // Initial parent selection encoded as "none" | "pfc:<ENUM>" | "custom:<ID>".
  const initialParent = category?.parent_id
    ? `custom:${category.parent_id}`
    : category?.parent_pfc_primary
      ? `pfc:${category.parent_pfc_primary}`
      : PARENT_NONE
  const [parent, setParent] = useState(initialParent)

  const { data: pfcOptions } = useCuratedPfcDetailed()
  const pfcPrimaries = useMemo(() => {
    const seen = new Map<string, string>()
    for (const opt of pfcOptions ?? []) {
      if (!seen.has(opt.primary)) seen.set(opt.primary, opt.primaryLabel)
    }
    return Array.from(seen.entries()).sort((a, b) => a[1].localeCompare(b[1]))
  }, [pfcOptions])

  const customParents = useMemo(
    () => eligibleCustomParents(allCategories, category?.id),
    [allCategories, category?.id]
  )
  // A category that itself has children cannot be nested (would create a grandchild).
  const hasChildren = useMemo(
    () => !!category && allCategories.some((c) => c.parent_id === category.id),
    [allCategories, category]
  )

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError(null)
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Name is required')
      return
    }

    const req: CreateCategoryRequest & UpdateCategoryRequest = {
      name: trimmed,
      color,
      icon: icon ?? undefined,
      // Send empty strings so the API clears the opposite parent dimension.
      parent_pfc_primary: parent.startsWith('pfc:') ? parent.slice(4) : '',
      parent_id: parent.startsWith('custom:') ? parent.slice(7) : '',
    }

    try {
      await onSubmit(req)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save category')
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" aria-label={isEdit ? 'Edit category' : 'Create category'}>
      <div className="space-y-1">
        <label htmlFor="category-name" className="text-sm font-medium">
          Name
        </label>
        <input
          id="category-name"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
          autoFocus
          className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          data-ph-mask
        />
      </div>

      <div className="flex items-end gap-3">
        <div className="space-y-1">
          <label htmlFor="category-color" className="text-sm font-medium">
            Color
          </label>
          <input
            id="category-color"
            type="color"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            aria-label="Category color"
            className="h-9 w-12 cursor-pointer rounded border border-input"
          />
        </div>
        <div className="space-y-1">
          <span className="block text-sm font-medium">Icon</span>
          <IconPicker value={icon} onChange={setIcon} />
        </div>
      </div>

      <div className="space-y-1">
        <label htmlFor="category-parent" className="text-sm font-medium">
          Nest under
        </label>
        <select
          id="category-parent"
          value={parent}
          onChange={(e) => setParent(e.target.value)}
          disabled={hasChildren}
          className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        >
          <option value={PARENT_NONE}>Top-level (no parent)</option>
          <optgroup label="Plaid category">
            {pfcPrimaries.map(([primary, label]) => (
              <option key={primary} value={`pfc:${primary}`}>
                {label}
              </option>
            ))}
          </optgroup>
          {customParents.length > 0 && (
            <optgroup label="Custom category">
              {customParents.map((c) => (
                <option key={c.id} value={`custom:${c.id}`}>
                  {c.name}
                </option>
              ))}
            </optgroup>
          )}
        </select>
        {hasChildren && (
          <p className="text-xs text-muted-foreground">
            This category has sub-categories, so it must stay top-level.
          </p>
        )}
      </div>

      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="rounded-md border border-input px-3 py-2 text-sm hover:bg-accent"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={isPending}
          className="rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {isEdit ? 'Save changes' : 'Create category'}
        </button>
      </div>
    </form>
  )
}
