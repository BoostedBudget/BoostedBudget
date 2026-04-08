import { useState, useRef, useEffect, useMemo, useCallback } from 'react'

export interface SearchableSelectOption {
  value: string
  label: string
  sublabel?: string
}

export interface SearchableSelectProps {
  options: SearchableSelectOption[]
  value: string | null
  onChange: (value: string | null) => void
  placeholder?: string
  disabled?: boolean
  className?: string
  'aria-label'?: string
  allowClear?: boolean
  clearLabel?: string
}

export function SearchableSelect({
  options,
  value,
  onChange,
  placeholder = 'Search...',
  disabled = false,
  className = '',
  'aria-label': ariaLabel,
  allowClear = false,
  clearLabel = 'Clear selection',
}: SearchableSelectProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [searchText, setSearchText] = useState('')
  const [highlightedIndex, setHighlightedIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxRef = useRef<HTMLUListElement>(null)

  // Find the selected option's label
  const selectedOption = useMemo(() => {
    return options.find((opt) => opt.value === value)
  }, [options, value])

  // Filter options based on search text
  const filteredOptions = useMemo(() => {
    if (!searchText.trim()) return options

    const searchLower = searchText.toLowerCase()
    return options.filter((opt) => {
      const labelMatch = opt.label.toLowerCase().includes(searchLower)
      const sublabelMatch = opt.sublabel?.toLowerCase().includes(searchLower)
      return labelMatch || sublabelMatch
    })
  }, [options, searchText])

  // Handle click outside to close dropdown
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
        setSearchText('')
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Scroll highlighted option into view
  useEffect(() => {
    if (highlightedIndex >= 0 && listboxRef.current) {
      const highlightedElement = listboxRef.current.children[
        allowClear ? highlightedIndex + 1 : highlightedIndex
      ] as HTMLElement
      if (highlightedElement && highlightedElement.scrollIntoView) {
        highlightedElement.scrollIntoView({ block: 'nearest' })
      }
    }
  }, [highlightedIndex, allowClear])

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setSearchText(e.target.value)
    setHighlightedIndex(-1) // Reset highlighted index when search changes
    if (!isOpen) {
      setIsOpen(true)
    }
  }

  const handleInputFocus = () => {
    if (!disabled) {
      setIsOpen(true)
    }
  }

  const handleInputClick = () => {
    if (!disabled) {
      setIsOpen(true)
    }
  }

  const handleSelect = useCallback(
    (optionValue: string | null) => {
      onChange(optionValue)
      setIsOpen(false)
      setSearchText('')
      setHighlightedIndex(-1)
    },
    [onChange]
  )

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter') {
        setIsOpen(true)
        e.preventDefault()
      }
      return
    }

    const totalOptions = filteredOptions.length

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setHighlightedIndex((prev) => {
          if (prev < totalOptions - 1) return prev + 1
          return 0 // Wrap to start
        })
        break

      case 'ArrowUp':
        e.preventDefault()
        setHighlightedIndex((prev) => {
          if (prev > 0) return prev - 1
          return totalOptions - 1 // Wrap to end
        })
        break

      case 'Enter':
        e.preventDefault()
        if (highlightedIndex >= 0 && highlightedIndex < filteredOptions.length) {
          handleSelect(filteredOptions[highlightedIndex].value)
        }
        break

      case 'Escape':
        e.preventDefault()
        setIsOpen(false)
        setSearchText('')
        setHighlightedIndex(-1)
        break
    }
  }

  // Determine input display value
  const inputValue = useMemo(() => {
    if (isOpen && searchText) {
      return searchText
    }
    if (selectedOption) {
      return selectedOption.label
    }
    return ''
  }, [isOpen, searchText, selectedOption])

  const optionId = (index: number) => `searchable-select-option-${index}`

  return (
    <div
      ref={containerRef}
      className={`searchable-select relative inline-block w-full ${className}`.trim()}
    >
      <input
        ref={inputRef}
        type="text"
        role="combobox"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        aria-label={ariaLabel}
        aria-autocomplete="list"
        aria-activedescendant={
          isOpen && highlightedIndex >= 0 ? optionId(highlightedIndex) : undefined
        }
        className="w-full px-3 py-2 text-sm border border-input rounded bg-background text-foreground transition-colors focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70 disabled:cursor-not-allowed placeholder:text-muted-foreground"
        placeholder={placeholder}
        value={inputValue}
        onChange={handleInputChange}
        onFocus={handleInputFocus}
        onClick={handleInputClick}
        onKeyDown={handleKeyDown}
        disabled={disabled}
      />

      {isOpen && (
        <ul
          ref={listboxRef}
          role="listbox"
          className="absolute top-full left-0 right-0 mt-1 p-0 m-0 list-none bg-card border border-border rounded shadow-lg max-h-60 overflow-y-auto z-[100]"
        >
          {allowClear && (
            <li
              role="option"
              aria-selected={value === null}
              className={`flex flex-col gap-0.5 px-3 py-2 cursor-pointer transition-colors text-muted-foreground italic border-b border-border ${
                value === null ? 'font-medium text-primary' : 'hover:bg-muted'
              }`}
              onClick={() => handleSelect(null)}
            >
              {clearLabel}
            </li>
          )}

          {filteredOptions.length === 0 ? (
            <li className="px-3 py-3 text-center text-muted-foreground text-sm italic">No results found</li>
          ) : (
            filteredOptions.map((option, index) => (
              <li
                key={option.value}
                id={optionId(index)}
                role="option"
                aria-selected={option.value === value}
                className={`flex flex-col gap-0.5 px-3 py-2 cursor-pointer transition-colors ${
                  index === highlightedIndex
                    ? 'searchable-select__option--highlighted bg-accent'
                    : 'hover:bg-muted'
                } ${
                  option.value === value ? 'font-medium' : ''
                }`}
                onClick={() => handleSelect(option.value)}
              >
                <span className={`text-sm whitespace-nowrap overflow-hidden text-ellipsis ${option.value === value ? 'text-primary' : 'text-foreground'}`}>{option.label}</span>
                {option.sublabel && (
                  <span className="text-xs text-muted-foreground whitespace-nowrap overflow-hidden text-ellipsis">
                    {option.sublabel}
                  </span>
                )}
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  )
}
