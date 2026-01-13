/**
 * ColumnFilterDropdown Component
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * A reusable popover filter dropdown for table column filtering.
 * Supports single-select, multi-select, and range filtering modes.
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { Filter, Check, X } from 'lucide-react'
import './ColumnFilterDropdown.css'

export interface FilterOption {
  value: string
  label: string
}

export interface RangeValue {
  min?: number
  max?: number
}

export interface ColumnFilterDropdownProps {
  /** Filter type */
  type?: 'select' | 'range'
  /** Available options for select mode */
  options?: FilterOption[]
  /** Current selected value(s) for select mode */
  value?: string[]
  /** Current range value for range mode */
  rangeValue?: RangeValue | null
  /** Called when filter changes */
  onChange: (value: string[] | RangeValue | null) => void
  /** Enable multi-select mode */
  multiSelect?: boolean
  /** Enable search in multi-select mode */
  searchable?: boolean
  /** Disable the filter */
  disabled?: boolean
  /** Custom aria-label */
  ariaLabel?: string
  /** Additional CSS class */
  className?: string
}

export function ColumnFilterDropdown({
  type = 'select',
  options = [],
  value = [],
  rangeValue,
  onChange,
  multiSelect = false,
  searchable = false,
  disabled = false,
  ariaLabel = 'Filter',
  className = '',
}: ColumnFilterDropdownProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [localMin, setLocalMin] = useState<string>(rangeValue?.min?.toString() ?? '')
  const [localMax, setLocalMax] = useState<string>(rangeValue?.max?.toString() ?? '')
  const containerRef = useRef<HTMLDivElement>(null)

  // Close on outside click
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [isOpen])

  // Close on Escape
  useEffect(() => {
    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setIsOpen(false)
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleEscape)
      return () => document.removeEventListener('keydown', handleEscape)
    }
  }, [isOpen])

  const isActive = type === 'range'
    ? rangeValue !== null && rangeValue !== undefined && (rangeValue.min !== undefined || rangeValue.max !== undefined)
    : value.length > 0

  const filteredOptions = searchQuery
    ? options.filter((opt) =>
        opt.label.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : options

  const handleToggle = useCallback(() => {
    if (!disabled) {
      const newIsOpen = !isOpen
      setIsOpen(newIsOpen)
      setSearchQuery('')
      // Reset range inputs when opening
      if (newIsOpen && type === 'range') {
        setLocalMin(rangeValue?.min?.toString() ?? '')
        setLocalMax(rangeValue?.max?.toString() ?? '')
      }
    }
  }, [disabled, isOpen, type, rangeValue])

  const handleSelectOption = useCallback(
    (optionValue: string) => {
      if (multiSelect) {
        const newValue = value.includes(optionValue)
          ? value.filter((v) => v !== optionValue)
          : [...value, optionValue]
        onChange(newValue)
      } else {
        onChange([optionValue])
        setIsOpen(false)
      }
    },
    [multiSelect, onChange, value]
  )

  const handleSelectAll = useCallback(() => {
    onChange(options.map((opt) => opt.value))
  }, [onChange, options])

  const handleClearAll = useCallback(() => {
    onChange([])
    if (!multiSelect) {
      setIsOpen(false)
    }
  }, [onChange, multiSelect])

  const handleApplyRange = useCallback(() => {
    const min = localMin ? parseFloat(localMin) : undefined
    const max = localMax ? parseFloat(localMax) : undefined

    if (min !== undefined || max !== undefined) {
      onChange({ min, max })
    } else {
      onChange(null)
    }
    setIsOpen(false)
  }, [localMin, localMax, onChange])

  const handleClearRange = useCallback(() => {
    setLocalMin('')
    setLocalMax('')
    onChange(null)
    setIsOpen(false)
  }, [onChange])

  return (
    <div
      ref={containerRef}
      className={`column-filter ${className}`.trim()}
    >
      <button
        type="button"
        onClick={handleToggle}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        className={`column-filter__trigger ${isActive ? 'column-filter__trigger--active' : ''}`}
      >
        <Filter className="column-filter__icon" aria-hidden="true" />
      </button>

      {isOpen && (
        <div className="column-filter__dropdown" role="listbox">
          {type === 'select' ? (
            <>
              {/* Search input for searchable multi-select */}
              {searchable && multiSelect && (
                <div className="column-filter__search">
                  <input
                    type="text"
                    placeholder="Search..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="column-filter__search-input"
                    autoFocus
                  />
                </div>
              )}

              {/* Multi-select actions */}
              {multiSelect && (
                <div className="column-filter__actions">
                  <button
                    type="button"
                    onClick={handleSelectAll}
                    className="column-filter__action-btn"
                  >
                    Select All
                  </button>
                  <button
                    type="button"
                    onClick={handleClearAll}
                    className="column-filter__action-btn"
                  >
                    Clear
                  </button>
                </div>
              )}

              {/* Options list */}
              <div className="column-filter__options">
                {/* "All" option for single select */}
                {!multiSelect && (
                  <button
                    type="button"
                    onClick={handleClearAll}
                    className={`column-filter__option ${value.length === 0 ? 'column-filter__option--selected' : ''}`}
                  >
                    All
                  </button>
                )}

                {filteredOptions.map((option) => {
                  const isSelected = value.includes(option.value)

                  if (multiSelect) {
                    return (
                      <label
                        key={option.value}
                        className={`column-filter__option column-filter__option--checkbox ${isSelected ? 'column-filter__option--selected' : ''}`}
                      >
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => handleSelectOption(option.value)}
                          className="column-filter__checkbox"
                        />
                        <span className="column-filter__option-label">
                          {option.label}
                        </span>
                        {isSelected && (
                          <Check
                            className="column-filter__check-icon"
                            aria-hidden="true"
                          />
                        )}
                      </label>
                    )
                  }

                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => handleSelectOption(option.value)}
                      className={`column-filter__option ${isSelected ? 'column-filter__option--selected' : ''}`}
                    >
                      {option.label}
                      {isSelected && (
                        <Check
                          className="column-filter__check-icon"
                          aria-hidden="true"
                        />
                      )}
                    </button>
                  )
                })}

                {filteredOptions.length === 0 && (
                  <div className="column-filter__no-results">
                    No matching options
                  </div>
                )}
              </div>
            </>
          ) : (
            /* Range filter mode */
            <div className="column-filter__range">
              <div className="column-filter__range-inputs">
                <div className="column-filter__range-field">
                  <label htmlFor="filter-min" className="column-filter__range-label">
                    Min
                  </label>
                  <input
                    id="filter-min"
                    type="number"
                    value={localMin}
                    onChange={(e) => setLocalMin(e.target.value)}
                    placeholder="0"
                    className="column-filter__range-input"
                  />
                </div>
                <div className="column-filter__range-separator">-</div>
                <div className="column-filter__range-field">
                  <label htmlFor="filter-max" className="column-filter__range-label">
                    Max
                  </label>
                  <input
                    id="filter-max"
                    type="number"
                    value={localMax}
                    onChange={(e) => setLocalMax(e.target.value)}
                    placeholder="999"
                    className="column-filter__range-input"
                  />
                </div>
              </div>
              <div className="column-filter__range-actions">
                <button
                  type="button"
                  onClick={handleClearRange}
                  className="column-filter__action-btn column-filter__action-btn--secondary"
                  aria-label="Clear"
                >
                  <X className="column-filter__btn-icon" aria-hidden="true" />
                  Clear
                </button>
                <button
                  type="button"
                  onClick={handleApplyRange}
                  className="column-filter__action-btn column-filter__action-btn--primary"
                  aria-label="Apply"
                >
                  <Check className="column-filter__btn-icon" aria-hidden="true" />
                  Apply
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
