/**
 * Transaction Category Filter Component
 * Multi-select dropdown for filtering by categories
 * Searches both user overrides AND original Plaid categories
 * Issue #529: Transaction page filtering
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { ChevronDownIcon, CheckIcon } from '@heroicons/react/24/outline'

interface TransactionCategoryFilterProps {
  categories: string[]
  selectedCategories: string[]
  onChange: (categories: string[]) => void
  loading?: boolean
  disabled?: boolean
}

export function TransactionCategoryFilter({
  categories,
  selectedCategories,
  onChange,
  loading,
  disabled,
}: TransactionCategoryFilterProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const dropdownRef = useRef<HTMLDivElement>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false)
        setSearchTerm('')
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Focus search input when dropdown opens
  useEffect(() => {
    if (isOpen && searchInputRef.current) {
      searchInputRef.current.focus()
    }
  }, [isOpen])

  const filteredCategories = categories.filter((category) =>
    category.toLowerCase().includes(searchTerm.toLowerCase())
  )

  const handleToggle = useCallback(
    (category: string) => {
      if (selectedCategories.includes(category)) {
        onChange(selectedCategories.filter((c) => c !== category))
      } else {
        onChange([...selectedCategories, category])
      }
    },
    [selectedCategories, onChange]
  )

  const handleSelectAll = useCallback(() => {
    onChange([...categories])
  }, [categories, onChange])

  const handleClearAll = useCallback(() => {
    onChange([])
  }, [onChange])

  const getDisplayText = () => {
    if (selectedCategories.length === 0) return 'All categories'
    if (selectedCategories.length === 1) return selectedCategories[0]
    return `${selectedCategories.length} categories`
  }

  return (
    <div className="flex flex-col gap-1 relative" ref={dropdownRef}>
      <label className="text-sm font-medium text-foreground">Categories</label>

      {/* Dropdown trigger */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        disabled={disabled || loading}
        className={`
          flex items-center justify-between w-full px-3 py-2 text-sm
          border border-input rounded-lg bg-card
          focus:ring-2 focus:ring-ring focus:border-ring
          ${disabled || loading ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-accent'}
        `}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
      >
        <span className={selectedCategories.length === 0 ? 'text-muted-foreground' : 'text-foreground'}>
          {loading ? 'Loading categories...' : getDisplayText()}
        </span>
        <ChevronDownIcon
          className={`w-4 h-4 text-muted-foreground transition-transform ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Dropdown menu */}
      {isOpen && !loading && (
        <div className="absolute left-0 z-10 mt-1 min-w-full max-w-xs max-h-72 bg-card border border-border rounded-lg shadow-lg top-full">
          {/* Search input */}
          <div className="p-2 border-b border-border/50">
            <input
              ref={searchInputRef}
              type="text"
              placeholder="Search categories..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="w-full px-3 py-1.5 text-sm border border-input rounded-md bg-background text-foreground focus:ring-1 focus:ring-ring focus:border-ring"
            />
          </div>

          {/* Select All / Clear All */}
          <div className="flex justify-between p-2 border-b border-border/50">
            <button
              type="button"
              onClick={handleSelectAll}
              className="text-xs text-primary hover:opacity-80"
            >
              Select All
            </button>
            <button
              type="button"
              onClick={handleClearAll}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              Clear All
            </button>
          </div>

          {/* Category list */}
          <div className="max-h-48 overflow-auto">
            {filteredCategories.length === 0 ? (
              <div className="p-3 text-sm text-muted-foreground text-center">
                {searchTerm ? 'No matching categories' : 'No categories available'}
              </div>
            ) : (
              <ul role="listbox" className="py-1">
                {filteredCategories.map((category) => {
                  const isSelected = selectedCategories.includes(category)
                  return (
                    <li
                      key={category}
                      role="option"
                      aria-selected={isSelected}
                      onClick={() => handleToggle(category)}
                      className={`
                        flex items-center gap-2 px-3 py-2 cursor-pointer
                        ${isSelected ? 'bg-primary/10' : 'hover:bg-accent'}
                      `}
                    >
                      <div
                        className={`
                          w-4 h-4 flex items-center justify-center rounded border
                          ${isSelected ? 'bg-primary border-primary' : 'border-input'}
                        `}
                      >
                        {isSelected && <CheckIcon className="w-3 h-3 text-white" />}
                      </div>
                      <span className="text-sm text-foreground truncate">{category}</span>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
