import { useState } from 'react'
import type { CustomCategory } from '../types'
import { useCreateCategory } from '@/api/generated/endpoints/categories/categories'
import './CategoryEditor.css'

interface CategoryEditorProps {
  selectedCategory: string[]
  availableCategories: CustomCategory[]
  onChange: (category: string[]) => void
  onCategoryCreated?: (category: CustomCategory) => void
  disabled?: boolean
}

export function CategoryEditor({
  selectedCategory,
  availableCategories,
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

  const currentCategory = selectedCategory[0] || null
  const currentCategoryObj = availableCategories.find(
    (cat) => cat.name === currentCategory
  )

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

  const handleShowCreateForm = () => {
    setIsCreating(true)
  }

  const handleCancelCreate = () => {
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
      setIsCreating(false)
      setIsDropdownOpen(false)
      setNewCategoryName('')
      setNewCategoryColor('#6B7280')
    } catch (error) {
      console.error('Failed to create category:', error)
    }
  }

  return (
    <div className="category-editor">
      <div className="category-editor__current">
        {currentCategory ? (
          <>
            <span
              className="category-editor__color"
              data-testid="category-color"
              style={{ backgroundColor: currentCategoryObj?.color || '#6B7280' }}
            />
            <span className="category-editor__name">{currentCategory}</span>
            <button
              type="button"
              className="category-editor__clear"
              onClick={handleClear}
              disabled={disabled}
              aria-label="Clear category"
            >
              ×
            </button>
          </>
        ) : (
          <span className="category-editor__empty">Uncategorized</span>
        )}
      </div>

      <div className="category-editor__dropdown-container">
        <button
          type="button"
          className="category-editor__change-button"
          onClick={toggleDropdown}
          disabled={disabled}
          aria-label="Change category"
        >
          Change
        </button>

        {isDropdownOpen && (
          <ul className="category-editor__dropdown" role="listbox">
            {/* Uncategorized option first */}
            <li
              role="option"
              aria-label="Uncategorized"
              className={`category-editor__option category-editor__uncategorized${
                !currentCategory ? ' selected' : ''
              }`}
              onClick={() => {
                onChange([])
                setIsDropdownOpen(false)
              }}
            >
              <span
                className="category-editor__option-color"
                style={{ backgroundColor: '#9CA3AF' }}
              />
              Uncategorized
            </li>
            {availableCategories.map((category) => (
              <li
                key={category.id}
                role="option"
                aria-label={category.name}
                className={`category-editor__option${
                  category.name === currentCategory ? ' selected' : ''
                }`}
                onClick={() => handleSelectCategory(category.name)}
              >
                <span
                  className="category-editor__option-color"
                  style={{ backgroundColor: category.color || '#6B7280' }}
                />
                {category.name}
              </li>
            ))}

            {isCreating ? (
              <li className="category-editor__create-form">
                <input
                  type="text"
                  placeholder="Category name"
                  value={newCategoryName}
                  onChange={(e) => setNewCategoryName(e.target.value)}
                  className="category-editor__create-input"
                />
                <input
                  type="color"
                  value={newCategoryColor}
                  onChange={(e) => setNewCategoryColor(e.target.value)}
                  className="category-editor__create-color"
                  aria-label="Color"
                />
                <div className="category-editor__create-actions">
                  <button
                    type="button"
                    onClick={handleCreateCategory}
                    disabled={!newCategoryName.trim()}
                    className="category-editor__create-button"
                  >
                    Create
                  </button>
                  <button
                    type="button"
                    onClick={handleCancelCreate}
                    className="category-editor__cancel-button"
                  >
                    Cancel
                  </button>
                </div>
              </li>
            ) : (
              <li
                className="category-editor__option category-editor__create-option"
                onClick={handleShowCreateForm}
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
