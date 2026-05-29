// Custom Category Manager — full CRUD over custom categories (Issue #1203).
// Rendered inline in the Settings "Categories" tab and (via a Dialog) from budgets.
import { useState } from 'react'
import { Plus } from 'lucide-react'
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
        <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
          No custom categories yet. Create one to group your spending your way.
        </p>
      )}

      {/* Top-level custom categories and their sub-categories */}
      {hierarchy.topLevel.length > 0 && (
        <div className="space-y-1" role="list" aria-label="Top-level custom categories">
          {hierarchy.topLevel.map((group) => (
            <div key={group.parent.id} className="space-y-1">
              <CustomCategoryListItem category={group.parent} onEdit={openEdit} onDelete={setDeleting} />
              {group.children.map((child) => (
                <CustomCategoryListItem
                  key={child.id}
                  category={child}
                  variant="child"
                  onEdit={openEdit}
                  onDelete={setDeleting}
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
              <CustomCategoryListItem key={c.id} category={c} onEdit={openEdit} onDelete={setDeleting} />
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
