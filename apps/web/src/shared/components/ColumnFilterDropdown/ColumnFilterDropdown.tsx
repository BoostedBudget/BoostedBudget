/**
 * ColumnFilterDropdown Component
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * A reusable popover filter dropdown for table column filtering.
 * Supports single-select, multi-select, and range filtering modes.
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { Filter, Check, X } from 'lucide-react'

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
      className={`relative inline-block ${className}`.trim()}
    >
      <button
        type="button"
        onClick={handleToggle}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        className={[
          'flex items-center justify-center w-6 h-6 p-1 border-none rounded cursor-pointer',
          'bg-transparent transition-colors duration-150',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          isActive
            ? 'column-filter__trigger--active text-primary bg-primary/10 hover:bg-primary/20 hover:text-primary'
            : 'text-muted-foreground hover:bg-muted hover:text-foreground',
        ].join(' ')}
      >
        <Filter className="w-4 h-4" aria-hidden="true" />
      </button>

      {isOpen && (
        <div
          className="absolute top-full left-0 z-50 min-w-[180px] max-w-[280px] mt-1 py-2 bg-card border border-border rounded-lg shadow-[0_4px_12px_rgba(0,0,0,0.15)]"
          role="listbox"
        >
          {type === 'select' ? (
            <>
              {/* Search input for searchable multi-select */}
              {searchable && multiSelect && (
                <div className="px-2 pb-2 border-b border-border">
                  <input
                    type="text"
                    placeholder="Search..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="w-full px-2.5 py-1.5 text-sm border border-border rounded bg-card text-foreground placeholder:text-muted-foreground focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10"
                    autoFocus
                  />
                </div>
              )}

              {/* Multi-select actions */}
              {multiSelect && (
                <div className="flex gap-2 px-2 py-2 border-b border-border">
                  <button
                    type="button"
                    onClick={handleSelectAll}
                    className="flex-1 px-2 py-1 text-xs font-medium border-none rounded bg-muted text-muted-foreground cursor-pointer transition-colors duration-150 hover:bg-border"
                  >
                    Select All
                  </button>
                  <button
                    type="button"
                    onClick={handleClearAll}
                    className="flex-1 px-2 py-1 text-xs font-medium border-none rounded bg-muted text-muted-foreground cursor-pointer transition-colors duration-150 hover:bg-border"
                  >
                    Clear
                  </button>
                </div>
              )}

              {/* Options list */}
              <div className="max-h-60 overflow-y-auto">
                {/* "All" option for single select */}
                {!multiSelect && (
                  <button
                    type="button"
                    onClick={handleClearAll}
                    className={[
                      'flex items-center w-full px-3 py-2 text-sm text-left border-none cursor-pointer transition-colors duration-100',
                      value.length === 0
                        ? 'bg-primary/10 text-primary'
                        : 'bg-transparent text-foreground hover:bg-muted',
                    ].join(' ')}
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
                        className={[
                          'flex items-center gap-2 w-full px-3 py-2 text-sm text-left border-none cursor-pointer transition-colors duration-100',
                          isSelected
                            ? 'bg-primary/10 text-primary'
                            : 'bg-transparent text-foreground hover:bg-muted',
                        ].join(' ')}
                      >
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => handleSelectOption(option.value)}
                          className="w-4 h-4 m-0 cursor-pointer"
                        />
                        <span className="flex-1">
                          {option.label}
                        </span>
                        {isSelected && (
                          <Check
                            className="w-3.5 h-3.5 ml-auto text-primary"
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
                      className={[
                        'flex items-center w-full px-3 py-2 text-sm text-left border-none cursor-pointer transition-colors duration-100',
                        isSelected
                          ? 'column-filter__option--selected bg-primary/10 text-primary hover:bg-primary/20'
                          : 'bg-transparent text-foreground hover:bg-muted',
                      ].join(' ')}
                    >
                      {option.label}
                      {isSelected && (
                        <Check
                          className="w-3.5 h-3.5 ml-auto text-primary"
                          aria-hidden="true"
                        />
                      )}
                    </button>
                  )
                })}

                {filteredOptions.length === 0 && (
                  <div className="px-4 py-4 text-sm text-muted-foreground text-center">
                    No matching options
                  </div>
                )}
              </div>
            </>
          ) : (
            /* Range filter mode */
            <div className="p-3">
              <div className="flex items-end gap-2 mb-3">
                <div className="flex-1">
                  <label htmlFor="filter-min" className="block mb-1 text-xs font-medium text-muted-foreground">
                    Min
                  </label>
                  <input
                    id="filter-min"
                    type="number"
                    value={localMin}
                    onChange={(e) => setLocalMin(e.target.value)}
                    placeholder="0"
                    className="w-full px-2 py-1.5 text-sm border border-border rounded bg-card text-foreground focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10"
                  />
                </div>
                <div className="pb-2 text-muted-foreground">-</div>
                <div className="flex-1">
                  <label htmlFor="filter-max" className="block mb-1 text-xs font-medium text-muted-foreground">
                    Max
                  </label>
                  <input
                    id="filter-max"
                    type="number"
                    value={localMax}
                    onChange={(e) => setLocalMax(e.target.value)}
                    placeholder="999"
                    className="w-full px-2 py-1.5 text-sm border border-border rounded bg-card text-foreground focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10"
                  />
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={handleClearRange}
                  className="flex-1 px-2 py-1 text-xs font-medium border border-border rounded bg-transparent text-muted-foreground cursor-pointer transition-colors duration-150 hover:bg-muted"
                  aria-label="Clear"
                >
                  <X className="w-3 h-3 mr-1 align-middle inline" aria-hidden="true" />
                  Clear
                </button>
                <button
                  type="button"
                  onClick={handleApplyRange}
                  className="flex-1 px-2 py-1 text-xs font-medium border-none rounded bg-primary text-white cursor-pointer transition-colors duration-150 hover:bg-primary/90"
                  aria-label="Apply"
                >
                  <Check className="w-3 h-3 mr-1 align-middle inline" aria-hidden="true" />
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
