import { useMemo, useState } from 'react'
import type { CustomCategory } from '../types'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { formatPFCEnum } from '@/shared/categories/formatPfc'
import { useCreateCategory } from '@boostedbudget/api-client/endpoints/categories/categories'
import { ReadOnlyGuard } from '@/auth'

interface CategoryEditorProps {
  selectedCategory: string[]
  availableCategories: CustomCategory[]
  plaidPrimaries?: Primary[]
  onChange: (category: string[]) => void
  onCategoryCreated?: (category: CustomCategory) => void
  disabled?: boolean
}

export function CategoryEditor({
  selectedCategory,
  availableCategories,
  plaidPrimaries,
  onChange,
  onCategoryCreated,
  disabled = false,
}: CategoryEditorProps) {
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)
  const [isCreating, setIsCreating] = useState(false)
  const [newCategoryName, setNewCategoryName] = useState('')
  const [newCategoryColor, setNewCategoryColor] = useState('#6B7280')

  // Use generated mutation hook
  const createCategoryMutation = useCreateCategory()

  const mergedCategories = useMemo(() => {
    const result = new Map<string, { key: string; name: string; color?: string | null }>()

    for (const cat of availableCategories) {
      const norm = cat.name.trim().toLowerCase()
      if (norm === 'uncategorized') continue
      result.set(norm, { key: cat.id, name: cat.name, color: cat.color })
    }

    if (plaidPrimaries) {
      for (const primary of plaidPrimaries) {
        const norm = primary.display.trim().toLowerCase()
        if (norm === 'uncategorized') continue
        if (!result.has(norm)) {
          result.set(norm, { key: `pfc:${primary.enum}`, name: primary.display, color: null })
        }
      }
    }

    return [...result.values()].sort((a, b) => a.name.localeCompare(b.name))
  }, [availableCategories, plaidPrimaries])

  const currentCategory = selectedCategory[0] || null
  const currentCategoryObj = mergedCategories.find(
    (cat) => cat.key === `pfc:${currentCategory}` || cat.name === currentCategory
  )
  const currentLabel = currentCategoryObj?.name || (currentCategory ? formatPFCEnum(currentCategory) : null)

  const handleSelectCategory = (categoryName: string) => {
    onChange([categoryName])
    setIsDropdownOpen(false)
  }

  const handleClear = () => {
    onChange([])
  }

  const toggleDropdown = () => {
    if (!disabled) {
      setIsDropdownOpen(!isDropdownOpen)
    }
  }

  const resetCreateForm = () => {
    setIsCreating(false)
    setNewCategoryName('')
    setNewCategoryColor('#6B7280')
  }

  const handleCreateCategory = async () => {
    if (!newCategoryName.trim()) return

    try {
      const newCategory = await createCategoryMutation.mutateAsync({
        data: {
          name: newCategoryName.trim(),
          color: newCategoryColor,
        },
      })

      onCategoryCreated?.(newCategory as unknown as CustomCategory)
      onChange([newCategory.name])
      resetCreateForm()
      setIsDropdownOpen(false)
    } catch (error) {
      console.error('Failed to create category:', error)
    }
  }

  return (
    <div className="flex items-center gap-3">
      <div className="flex items-center gap-2 flex-1">
        {currentLabel ? (
          <>
            <span
              className="w-3 h-3 rounded-full shrink-0"
              data-testid="category-color"
              style={{ backgroundColor: currentCategoryObj?.color || '#6B7280' }}
            />
            <span className="text-sm text-foreground" data-ph-mask>{currentLabel}</span>
            <button
              type="button"
              className="inline-flex items-center justify-center w-5 h-5 p-0 border-none bg-muted rounded-full text-muted-foreground cursor-pointer text-sm leading-none hover:bg-border hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
              onClick={handleClear}
              disabled={disabled}
              aria-label="Clear category"
            >
              ×
            </button>
          </>
        ) : (
          <span className="text-muted-foreground text-sm italic">Uncategorized</span>
        )}
      </div>

      <div className="relative">
        <ReadOnlyGuard message="Resubscribe to change categories">
          <button
            type="button"
            className="px-3 py-1.5 border border-input rounded-md bg-card text-foreground text-xs cursor-pointer hover:border-border hover:bg-muted transition-all disabled:cursor-not-allowed disabled:opacity-50"
            onClick={toggleDropdown}
            disabled={disabled}
            aria-label="Change category"
          >
            Change
          </button>
        </ReadOnlyGuard>

        {isDropdownOpen && (
          <ul className="absolute top-full right-0 z-50 min-w-48 max-h-60 overflow-y-auto mt-1 py-1 list-none m-0 bg-card border border-border rounded-md shadow-md" role="listbox">
            {/* Uncategorized option first */}
            <li
              role="option"
              aria-label="Uncategorized"
              className={`flex items-center gap-2 px-3 py-2 cursor-pointer text-sm hover:bg-muted ${!currentCategory ? 'bg-primary/5 font-medium' : ''}`}
              onClick={() => {
                onChange([])
                setIsDropdownOpen(false)
              }}
            >
              <span
                className="w-3 h-3 rounded-full shrink-0 bg-muted-foreground"
              />
              Uncategorized
            </li>
            {mergedCategories.map((category) => (
              <li
                key={category.key}
                role="option"
                className={`flex items-center gap-2 px-3 py-2 cursor-pointer text-sm hover:bg-muted ${category.name === currentCategory ? 'bg-primary/5 font-medium' : ''}`}
                onClick={() => handleSelectCategory(category.name)}
              >
                <span
                  className="w-3 h-3 rounded-full shrink-0"
                  style={{ backgroundColor: category.color || '#6B7280' }}
                />
                <span data-ph-mask>{category.name}</span>
              </li>
            ))}

            {isCreating ? (
              <li className="px-3 py-2 flex flex-col gap-2">
                <input
                  type="text"
                  placeholder="Category name"
                  value={newCategoryName}
                  onChange={(e) => setNewCategoryName(e.target.value)}
                  className="w-full px-2 py-1 text-sm border border-input rounded bg-background text-foreground"
                />
                <input
                  type="color"
                  value={newCategoryColor}
                  onChange={(e) => setNewCategoryColor(e.target.value)}
                  className="w-full h-8 border border-input rounded cursor-pointer"
                  aria-label="Color"
                />
                <div className="flex gap-2">
                  <ReadOnlyGuard message="Resubscribe to create categories">
                    <button
                      type="button"
                      onClick={handleCreateCategory}
                      disabled={!newCategoryName.trim()}
                      className="flex-1 px-2 py-1 text-xs bg-primary text-primary-foreground border-none rounded cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      Create
                    </button>
                  </ReadOnlyGuard>
                  <button
                    type="button"
                    onClick={resetCreateForm}
                    className="flex-1 px-2 py-1 text-xs border border-input rounded bg-card text-foreground cursor-pointer hover:bg-muted"
                  >
                    Cancel
                  </button>
                </div>
              </li>
            ) : (
              <li
                className="flex items-center gap-2 px-3 py-2 cursor-pointer text-sm hover:bg-muted text-muted-foreground"
                onClick={() => setIsCreating(true)}
              >
                + Create new category
              </li>
            )}
          </ul>
        )}
      </div>
    </div>
  )
}
