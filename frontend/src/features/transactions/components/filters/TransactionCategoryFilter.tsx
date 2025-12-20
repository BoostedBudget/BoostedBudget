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
      <label className="text-sm font-medium text-gray-700">Categories</label>

      {/* Dropdown trigger */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        disabled={disabled || loading}
        className={`
          flex items-center justify-between w-full px-3 py-2 text-sm
          border border-gray-300 rounded-lg bg-white
          focus:ring-2 focus:ring-blue-500 focus:border-blue-500
          ${disabled || loading ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-gray-50'}
        `}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
      >
        <span className={selectedCategories.length === 0 ? 'text-gray-500' : 'text-gray-900'}>
          {loading ? 'Loading categories...' : getDisplayText()}
        </span>
        <ChevronDownIcon
          className={`w-4 h-4 text-gray-400 transition-transform ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Dropdown menu */}
      {isOpen && !loading && (
        <div className="absolute z-10 mt-1 w-full max-h-72 bg-white border border-gray-200 rounded-lg shadow-lg top-full">
          {/* Search input */}
          <div className="p-2 border-b border-gray-100">
            <input
              ref={searchInputRef}
              type="text"
              placeholder="Search categories..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="w-full px-3 py-1.5 text-sm border border-gray-200 rounded-md focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
            />
          </div>

          {/* Select All / Clear All */}
          <div className="flex justify-between p-2 border-b border-gray-100">
            <button
              type="button"
              onClick={handleSelectAll}
              className="text-xs text-blue-600 hover:text-blue-800"
            >
              Select All
            </button>
            <button
              type="button"
              onClick={handleClearAll}
              className="text-xs text-gray-500 hover:text-gray-700"
            >
              Clear All
            </button>
          </div>

          {/* Category list */}
          <div className="max-h-48 overflow-auto">
            {filteredCategories.length === 0 ? (
              <div className="p-3 text-sm text-gray-500 text-center">
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
                        ${isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'}
                      `}
                    >
                      <div
                        className={`
                          w-4 h-4 flex items-center justify-center rounded border
                          ${isSelected ? 'bg-blue-600 border-blue-600' : 'border-gray-300'}
                        `}
                      >
                        {isSelected && <CheckIcon className="w-3 h-3 text-white" />}
                      </div>
                      <span className="text-sm text-gray-900 truncate">{category}</span>
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
