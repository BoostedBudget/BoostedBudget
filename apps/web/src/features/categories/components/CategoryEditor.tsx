import { useId, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { CustomCategory } from '../types'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { getCategoryPresentation } from '@/shared/categories/getCategoryPresentation'
import { categorySelectionEqual, type CategorySelection } from '@/shared/categories/categorySelection'
import type { EffectiveCategory } from '@/shared/categories/getEffectiveCategory'
import { getGetUserCategoriesQueryKey, useCreateCategory, useGetUserCategories, useGetPlaidPrimaryCategories, useGetPlaidDetailedCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import { ReadOnlyGuard } from '@/auth'

interface CategoryEditorProps {
  selectedCategory: CategorySelection
  availableCategories: CustomCategory[]
  plaidPrimaries?: Primary[]
  currentCategory?: EffectiveCategory | null
  bankCategory?: EffectiveCategory | null
  onChange: (category: CategorySelection) => void
  onCategoryCreated?: (category: CustomCategory) => void
  disabled?: boolean
}
interface Option {
  key: string
  label: string
  selection: CategorySelection
  parent?: string
  color?: string | null
}

function matchesCategorySearch(path: string[], term: string): boolean {
  return path.join(' ').toLocaleLowerCase().includes(term) ||
    path.join(' › ').toLocaleLowerCase().includes(term)
}

export function CategoryEditor({ selectedCategory, availableCategories, plaidPrimaries, currentCategory, bankCategory, onChange, onCategoryCreated, disabled = false }: CategoryEditorProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [active, setActive] = useState<number | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [newCategoryName, setNewCategoryName] = useState('')
  const [newCategoryColor, setNewCategoryColor] = useState('#6B7280')
  const [createError, setCreateError] = useState(false)
  const [createdCategories, setCreatedCategories] = useState<CustomCategory[]>([])
  const trigger = useRef<HTMLButtonElement>(null)
  const listId = useId()
  const queryClient = useQueryClient()
  const primaryQuery = useGetPlaidPrimaryCategories()
  const detailedQuery = useGetPlaidDetailedCategories()
  const customQuery = useGetUserCategories()
  const createCategoryMutation = useCreateCategory()
  const categories = useMemo(() => [...new Map([
    ...(customQuery.data ?? []), ...availableCategories, ...createdCategories,
  ].map(category => [category.id, category])).values()], [customQuery.data, availableCategories, createdCategories])
  const options = useMemo(() => {
    const map = new Map<string, Option>()
    for (const primary of [...(plaidPrimaries ?? []), ...(primaryQuery.data ?? [])]) {
      map.set(`pfc:${primary.enum}`, { key: `pfc:${primary.enum}`, label: primary.display, selection: { kind: 'pfc', pfc_primary: primary.enum } })
    }
    for (const detail of detailedQuery.data ?? []) {
      const parent = `pfc:${detail.primary_enum}`
      if (!map.has(parent)) map.set(parent, { key: parent, label: detail.primary_display, selection: { kind: 'pfc', pfc_primary: detail.primary_enum } })
      map.set(`detail:${detail.enum}`, { key: `detail:${detail.enum}`, label: detail.display, parent, selection: { kind: 'pfc', pfc_primary: detail.primary_enum, pfc_detailed: detail.enum } })
    }
    for (const category of categories) {
      let parent: string | undefined
      if (category.parent_id) {
        parent = `custom:${category.parent_id}`
      } else if (category.parent_pfc_primary) {
        parent = `pfc:${category.parent_pfc_primary}`
      }
      map.set(`custom:${category.id}`, { key: `custom:${category.id}`, label: category.name, color: category.color,
        parent,
        selection: { kind: 'custom', custom_category_id: category.id } })
    }
    const sorted = [...map.values()].sort((a, b) => a.label.localeCompare(b.label) || a.key.localeCompare(b.key))
    const result: (Option & { path: string[]; depth: number })[] = []
    const visited = new Set<string>()
    const visit = (option: Option, path: string[]) => {
      if (visited.has(option.key)) return
      visited.add(option.key)
      const nextPath = [...path, option.label]
      result.push({ ...option, path: nextPath, depth: path.length })
      for (const child of sorted.filter(candidate => candidate.parent === option.key)) visit(child, nextPath)
    }
    for (const root of sorted.filter(option => !option.parent || !map.has(option.parent))) visit(root, [])
    for (const option of sorted) visit(option, [])
    return result
  }, [categories, plaidPrimaries, primaryQuery.data, detailedQuery.data])
  const visible = useMemo(() => {
    const term = search.trim().toLocaleLowerCase()
    if (!term) return options
    const matches = options.filter(option => matchesCategorySearch(option.path, term))
    const keys = new Set(matches.map(option => option.key))
    const byKey = new Map(options.map(option => [option.key, option]))
    for (const option of matches) {
      let parent = option.parent
      const seen = new Set<string>()
      while (parent && !seen.has(parent)) {
        seen.add(parent)
        keys.add(parent)
        parent = byKey.get(parent)?.parent
      }
    }
    return options.filter(option => keys.has(option.key))
  }, [options, search])
  const selectedOption = options.find(option => categorySelectionEqual(option.selection, selectedCategory))
  const effective = selectedCategory.kind === 'clear' ? bankCategory ?? null : {
    ...(currentCategory && categorySelectionEqual(currentCategory, selectedCategory) ? currentCategory : {}), ...selectedCategory,
  } as EffectiveCategory
  const label = selectedCategory.kind === 'clear' && bankCategory === undefined
    ? 'Bank category'
    : selectedOption?.path.join(' › ') ?? getCategoryPresentation(effective, categories).breadcrumb
  const loading = primaryQuery.isLoading || detailedQuery.isLoading || customQuery.isLoading
  const metadataError = primaryQuery.error || detailedQuery.error || customQuery.error
  function close(): void {
    setOpen(false)
    trigger.current?.focus()
  }
  function select(selection: CategorySelection): void {
    if (disabled) return
    onChange(selection)
    close()
  }
  function resetCreateForm(): void {
    setIsCreating(false)
    setNewCategoryName('')
    setNewCategoryColor('#6B7280')
    setCreateError(false)
  }
  const handleCreateCategory = async () => {
    if (!newCategoryName.trim() || disabled || createCategoryMutation.isPending) return
    setCreateError(false)
    try {
      const category = await createCategoryMutation.mutateAsync({ data: { name: newCategoryName.trim(), color: newCategoryColor } })
      setCreatedCategories(previous => [...previous, category])
      void queryClient.invalidateQueries({ queryKey: getGetUserCategoriesQueryKey() })
      onCategoryCreated?.(category)
      select({ kind: 'custom', custom_category_id: category.id })
      resetCreateForm()
    } catch {
      setCreateError(true)
    }
  }
  const directMatch = visible.findIndex(option => matchesCategorySearch(option.path, search.trim().toLocaleLowerCase()))
  const focusedIndex = Math.min(active ?? Math.max(directMatch, 0), Math.max(visible.length - 1, 0))
  return (
    <div className="w-full space-y-3">
      <div className="flex items-center gap-3">
        <span data-testid="category-color" className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: selectedOption?.color || '#6B7280' }} />
        <span className="min-w-0 flex-1 text-sm" data-ph-mask>{label}</span>
        <ReadOnlyGuard message="Resubscribe to change categories">
          <button ref={trigger} type="button" aria-label="Change category" aria-expanded={open} aria-controls={listId} disabled={disabled}
            className="rounded-md border border-input px-3 py-1.5 text-sm hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            onClick={() => { setOpen(!open); setSearch(''); setActive(null) }}>Change</button>
        </ReadOnlyGuard>
      </div>
      {selectedCategory.kind !== 'clear' && !open && <button type="button" disabled={disabled} onClick={() => select({ kind: 'clear' })} className="text-xs underline focus-visible:ring-2 focus-visible:ring-ring">Reset category override</button>}
      {selectedCategory.kind === 'clear' && !open && <p className="text-xs text-muted-foreground">Uses the bank category when available.</p>}
      {open && (
        <div data-category-picker-panel className="w-full rounded-lg border border-border bg-card p-2" onKeyDown={event => {
          if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close() }
        }}>
          <input autoFocus role="combobox" aria-label="Search categories" aria-expanded="true" aria-controls={listId}
            aria-activedescendant={visible.length ? `${listId}-${focusedIndex}` : undefined} aria-autocomplete="list" data-ph-mask
            value={search} disabled={disabled} placeholder="Search categories"
            className="mb-2 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring"
            onChange={event => { setSearch(event.target.value); setActive(null) }}
            onKeyDown={event => {
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault()
                const index = event.key === 'ArrowDown' ? Math.min(focusedIndex + 1, visible.length - 1) : Math.max(focusedIndex - 1, 0)
                setActive(index)
                document.getElementById(`${listId}-${index}`)?.scrollIntoView?.({ block: 'nearest' })
              } else if (event.key === 'Enter') { event.preventDefault(); if (visible[focusedIndex]) select(visible[focusedIndex].selection) }
            }} />
          {loading && <p role="status" className="p-2 text-xs">Loading categories…</p>}
          {metadataError && <div role="alert" className="p-2 text-sm">Some categories could not be loaded. <button type="button" className="underline" onClick={() => { void primaryQuery.refetch(); void detailedQuery.refetch(); void customQuery.refetch() }}>Retry</button></div>}
          <div id={listId} role="listbox" aria-label="Categories" className="max-h-80 overflow-y-auto">
            {visible.map((option, index) => (
              <div key={option.key} id={`${listId}-${index}`} role="option" aria-selected={categorySelectionEqual(option.selection, selectedCategory)}
                className={`flex cursor-pointer items-center gap-2 rounded px-3 py-2 text-sm hover:bg-muted ${index === focusedIndex ? 'ring-2 ring-inset ring-ring' : ''} ${categorySelectionEqual(option.selection, selectedCategory) ? 'bg-primary/5 font-medium' : ''}`}
                style={{ paddingLeft: `${12 + option.depth * 16}px` }} onMouseMove={() => setActive(index)} onClick={() => select(option.selection)}>
                <span data-ph-mask>{option.label}</span>{option.selection.kind === 'custom' && <span className="text-xs text-muted-foreground">Custom</span>}
              </div>
            ))}
          </div>
          {!visible.length && !loading && <p className="p-3 text-sm text-muted-foreground">No categories found.</p>}
          <button type="button" disabled={disabled} onClick={() => select({ kind: 'clear' })} className="mt-2 rounded px-3 py-2 text-sm underline focus-visible:ring-2 focus-visible:ring-ring">Reset category override</button>
          <p className="px-3 text-xs text-muted-foreground">Uses the bank category when available.</p>
          {isCreating ? <div className="space-y-2 border-t border-border p-3">
            <input data-ph-mask aria-label="Category name" placeholder="Category name" value={newCategoryName} disabled={disabled || createCategoryMutation.isPending} onChange={event => setNewCategoryName(event.target.value)} className="w-full rounded border border-input bg-background px-2 py-1 text-sm" />
            <input type="color" aria-label="Color" value={newCategoryColor} onChange={event => setNewCategoryColor(event.target.value)} disabled={disabled || createCategoryMutation.isPending} />
            {createError && <p role="alert" className="text-sm text-destructive">Could not create category. Please try again.</p>}
            <div className="flex gap-2">
              <ReadOnlyGuard message="Resubscribe to create categories"><button type="button" disabled={disabled || !newCategoryName.trim() || createCategoryMutation.isPending} onClick={handleCreateCategory} className="rounded bg-primary px-3 py-1 text-primary-foreground disabled:opacity-50">Create</button></ReadOnlyGuard>
              <button type="button" onClick={resetCreateForm}>Cancel</button>
            </div>
          </div> : <button type="button" disabled={disabled} className="block w-full rounded px-3 py-2 text-left text-sm hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring" onClick={() => setIsCreating(true)}>+ Create new category</button>}
        </div>
      )}
    </div>
  )
}
