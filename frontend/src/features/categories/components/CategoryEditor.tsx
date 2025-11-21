import { useState } from 'react'
import type { CustomCategory } from '../types'
import './CategoryEditor.css'

interface CategoryEditorProps {
  selectedCategory: string[]
  availableCategories: CustomCategory[]
  onChange: (category: string[]) => void
  disabled?: boolean
}

export function CategoryEditor({
  selectedCategory,
  availableCategories,
  onChange,
  disabled = false,
}: CategoryEditorProps) {
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)

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
          </ul>
        )}
      </div>
    </div>
  )
}
