// Custom Category Manager — full CRUD over custom categories (Issue #1203).
// Rendered inline in the Settings "Categories" tab and (via a Dialog) from budgets.
import { useCallback, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { FolderOpen, Plus } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { getPrimaryLabel } from '@/shared/categories/pfcOptions'
import { useCustomCategories } from '../hooks/useCustomCategories'
import { useCategoryMutations } from '../hooks/useCategoryMutations'
import { CustomCategoryListItem } from './CustomCategoryListItem'
import { CustomCategoryForm } from './CustomCategoryForm'
import { DeleteCategoryDialog } from './DeleteCategoryDialog'
import type { CreateCategoryRequest, CustomCategory, UpdateCategoryRequest } from '../types'

export function CustomCategoryManager() {
  const { categories, hierarchy, isLoading } = useCustomCategories()
  const { createCategory, updateCategory, deleteCategory, isPending } = useCategoryMutations()

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CustomCategory | null>(null)
  const [deleting, setDeleting] = useState<CustomCategory | null>(null)

  const openCreate = () => {
    setEditing(null)
    setFormOpen(true)
  }
  const openEdit = (category: CustomCategory) => {
    setEditing(category)
    setFormOpen(true)
  }

  const handleSubmit = async (req: CreateCategoryRequest | UpdateCategoryRequest) => {
    if (editing) {
      await updateCategory(editing.id, req)
    } else {
      await createCategory(req as CreateCategoryRequest)
    }
    setFormOpen(false)
    setEditing(null)
  }

  const handleConfirmDelete = async () => {
    if (!deleting) return
    await deleteCategory(deleting.id)
    setDeleting(null)
  }

  // Flattened visible order (parents → their children → PFC-nested), matching the DOM,
  // so arrow keys move focus row-to-row with a roving tabindex (Issue #1203).
  const flatOrder = useMemo<CustomCategory[]>(() => {
    const out: CustomCategory[] = []
    for (const group of hierarchy.topLevel) {
      out.push(group.parent)
      out.push(...group.children)
    }
    for (const group of hierarchy.pfcGroups) {
      out.push(...group.categories)
    }
    return out
  }, [hierarchy])

  const rowRefs = useRef<(HTMLDivElement | null)[]>([])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent, category: CustomCategory) => {
      const index = flatOrder.findIndex((c) => c.id === category.id)
      if (index === -1) return
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          if (index < flatOrder.length - 1) rowRefs.current[index + 1]?.focus()
          break
        case 'ArrowUp':
          e.preventDefault()
          if (index > 0) rowRefs.current[index - 1]?.focus()
          break
        case 'Enter':
        case ' ':
          e.preventDefault()
          openEdit(category)
          break
        case 'Delete':
          e.preventDefault()
          setDeleting(category)
          break
      }
    },
    [flatOrder]
  )

  const rowIndex = (id: string) => flatOrder.findIndex((c) => c.id === id)

  const isEmpty = !isLoading && categories.length === 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Custom categories</h2>
        <button
          type="button"
          onClick={openCreate}
          className="inline-flex items-center gap-1 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus className="h-4 w-4" aria-hidden /> Add category
        </button>
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Loading categories…</p>}

      {isEmpty && (
        <EmptyState
          size="compact"
          icon={<FolderOpen />}
          title="No custom categories yet"
          description="Create one to group your spending your way."
        />
      )}

      {/* Top-level custom categories and their sub-categories */}
      {hierarchy.topLevel.length > 0 && (
        <div className="space-y-1" role="list" aria-label="Top-level custom categories">
          {hierarchy.topLevel.map((group) => (
            <div key={group.parent.id} className="space-y-1">
              <CustomCategoryListItem
                category={group.parent}
                onEdit={openEdit}
                onDelete={setDeleting}
                onKeyDown={handleKeyDown}
                itemRef={(el) => {
                  rowRefs.current[rowIndex(group.parent.id)] = el
                }}
              />
              {group.children.map((child) => (
                <CustomCategoryListItem
                  key={child.id}
                  category={child}
                  variant="child"
                  onEdit={openEdit}
                  onDelete={setDeleting}
                  onKeyDown={handleKeyDown}
                  itemRef={(el) => {
                    rowRefs.current[rowIndex(child.id)] = el
                  }}
                />
              ))}
            </div>
          ))}
        </div>
      )}

      {/* Custom categories nested under a Plaid primary */}
      {hierarchy.pfcGroups.map((group) => (
        <div key={group.pfcPrimary} className="space-y-1">
          <h3 className="px-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {getPrimaryLabel(group.pfcPrimary) ?? group.pfcPrimary}
          </h3>
          <div role="list" aria-label={`Custom categories under ${group.pfcPrimary}`} className="space-y-1">
            {group.categories.map((c) => (
              <CustomCategoryListItem
                key={c.id}
                category={c}
                onEdit={openEdit}
                onDelete={setDeleting}
                onKeyDown={handleKeyDown}
                itemRef={(el) => {
                  rowRefs.current[rowIndex(c.id)] = el
                }}
              />
            ))}
          </div>
        </div>
      ))}

      {/* Create / edit form */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit category' : 'New category'}</DialogTitle>
            <DialogDescription>
              Name your category, give it a color and icon, and optionally nest it under a parent.
            </DialogDescription>
          </DialogHeader>
          <CustomCategoryForm
            category={editing ?? undefined}
            allCategories={categories}
            onSubmit={handleSubmit}
            onCancel={() => setFormOpen(false)}
            isPending={isPending}
          />
        </DialogContent>
      </Dialog>

      {/* Delete confirmation with impact warning */}
      <DeleteCategoryDialog
        category={deleting}
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        onConfirm={handleConfirmDelete}
        loading={isPending}
      />
    </div>
  )
}
