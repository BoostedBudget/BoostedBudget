/**
 * HierarchicalCategoryTreeSelect Component
 * Issue #1200: Multi-Level Category Hierarchy
 *
 * Two-mode tree picker for Plaid PFC categories:
 *   - Multi-select (transaction filters): independent primary + detailed checkboxes
 *   - Single-select (budget form): one selection at a time
 *
 * Uses Radix Popover so the panel portals to document.body, escaping
 * the sticky thead CSS stacking context (same approach as ColumnFilterDropdown).
 */

import { useState, useCallback } from 'react'
import { ChevronRight, ChevronDown, Filter, Check } from 'lucide-react'
import { cva } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { useCuratedPfcDetailed } from '@/shared/categories/pfcOptions'
import type { PfcPrimaryOption, PfcDetailedOption } from '@/shared/categories/pfcOptions'

// ─── Props ─────────────────────────────────────────────────────────────────

export interface HierarchicalCategoryTreeSelectProps {
  // Multi-select mode (transaction filters)
  selectedPrimaries?: string[]
  onPrimariesChange?: (values: string[]) => void
  selectedDetaileds?: string[]
  onDetailedsChange?: (values: string[]) => void

  // Single-select mode (budget form)
  // Exclusive: if provided, overrides multi-select behavior
  singleSelectValue?: { primary: string; detailed?: string } | null
  onSingleSelectChange?: (value: { primary: string; detailed?: string } | null) => void

  // Common
  className?: string
  disabled?: boolean
  'aria-label'?: string
}

// ─── CVA variants ──────────────────────────────────────────────────────────

const treeRowVariants = cva(
  'flex items-center gap-2 w-full px-3 py-1.5 text-sm text-left transition-colors',
  {
    variants: {
      level: {
        primary: 'hover:bg-accent font-medium',
        detailed: 'pl-8 hover:bg-accent text-muted-foreground',
      },
      selected: {
        true: 'text-primary',
        false: '',
      },
    },
    defaultVariants: {
      level: 'primary',
      selected: false,
    },
  }
)

// ─── Checkbox state type ────────────────────────────────────────────────────

type CheckState = 'checked' | 'unchecked' | 'indeterminate'

// ─── FilteredCategory interface ─────────────────────────────────────────────

interface FilteredCategory {
  cat: PfcPrimaryOption
  showAll: boolean
  matchingDetaileds?: PfcDetailedOption[]
}

function getCheckState(
  primary: PfcPrimaryOption,
  selectedPrimaries: string[],
  selectedDetaileds: string[]
): CheckState {
  if (selectedPrimaries.includes(primary.primary)) return 'checked'
  const selectedChildren = primary.detailed.filter((d) =>
    selectedDetaileds.includes(d.value)
  )
  if (selectedChildren.length === 0) return 'unchecked'
  return 'indeterminate'
}

// ─── CheckboxIcon ────────────────────────────────────────────────────────────

interface CheckboxIconProps {
  state: CheckState
  'data-testid'?: string
}

function CheckboxIcon({ state, 'data-testid': testId }: CheckboxIconProps) {
  return (
    <span
      data-testid={testId}
      data-state={state}
      className={cn(
        'inline-flex items-center justify-center w-4 h-4 rounded border shrink-0 transition-colors',
        state === 'checked' && 'bg-primary border-primary text-primary-foreground',
        state === 'indeterminate' && 'bg-primary/20 border-primary',
        state === 'unchecked' && 'border-border bg-background'
      )}
      aria-checked={state === 'indeterminate' ? 'mixed' : state === 'checked'}
      role="checkbox"
    >
      {state === 'checked' && <Check className="w-3 h-3" aria-hidden="true" />}
      {state === 'indeterminate' && (
        <span className="w-2 h-0.5 bg-primary block" aria-hidden="true" />
      )}
    </span>
  )
}

// ─── Main component ─────────────────────────────────────────────────────────

export function HierarchicalCategoryTreeSelect({
  selectedPrimaries = [],
  onPrimariesChange,
  selectedDetaileds = [],
  onDetailedsChange,
  singleSelectValue,
  onSingleSelectChange,
  className,
  disabled = false,
  'aria-label': ariaLabel = 'Category filter',
}: HierarchicalCategoryTreeSelectProps) {
  const isSingleSelect =
    singleSelectValue !== undefined || onSingleSelectChange !== undefined

  const [searchQuery, setSearchQuery] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const { data: categories } = useCuratedPfcDetailed()

  const isActive = isSingleSelect
    ? singleSelectValue != null
    : selectedPrimaries.length > 0 || selectedDetaileds.length > 0

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (disabled && open) return
      // Reset search whenever the popover opens or closes
      if (!open) setSearchQuery('')
    },
    [disabled]
  )

  // ─── Search filtering ────────────────────────────────────────────────────

  const q = searchQuery.toLowerCase()

  const filteredCategories: FilteredCategory[] = categories.reduce<FilteredCategory[]>((acc, cat) => {
    if (!q) {
      acc.push({ cat, showAll: false })
      return acc
    }

    const primaryMatches = cat.primaryLabel.toLowerCase().includes(q)
    const matchingDetaileds = cat.detailed.filter((d) =>
      d.label.toLowerCase().includes(q)
    )

    if (!primaryMatches && matchingDetaileds.length === 0) return acc
    acc.push({ cat, showAll: primaryMatches, matchingDetaileds })
    return acc
  }, [])

  // ─── Expand toggle ───────────────────────────────────────────────────────

  const handleExpandToggle = useCallback((primary: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(primary)) {
        next.delete(primary)
      } else {
        next.add(primary)
      }
      return next
    })
  }, [])

  // ─── Multi-select handlers ───────────────────────────────────────────────

  const handlePrimaryCheckbox = useCallback(
    (primary: string) => {
      if (!onPrimariesChange) return
      if (selectedPrimaries.includes(primary)) {
        onPrimariesChange(selectedPrimaries.filter((p) => p !== primary))
      } else {
        onPrimariesChange([...selectedPrimaries, primary])
        // Clear its children when selecting the primary level
        if (onDetailedsChange) {
          const cat = categories.find((c) => c.primary === primary)
          if (cat) {
            const childValues = cat.detailed.map((d) => d.value)
            onDetailedsChange(
              selectedDetaileds.filter((d) => !childValues.includes(d))
            )
          }
        }
      }
    },
    [selectedPrimaries, onPrimariesChange, selectedDetaileds, onDetailedsChange, categories]
  )

  const handleDetailedCheckbox = useCallback(
    (value: string) => {
      if (!onDetailedsChange) return
      if (selectedDetaileds.includes(value)) {
        onDetailedsChange(selectedDetaileds.filter((d) => d !== value))
      } else {
        onDetailedsChange([...selectedDetaileds, value])
      }
    },
    [selectedDetaileds, onDetailedsChange]
  )

  const handleClear = useCallback(() => {
    onPrimariesChange?.([])
    onDetailedsChange?.([])
  }, [onPrimariesChange, onDetailedsChange])

  // ─── Single-select handlers ──────────────────────────────────────────────

  const handleSingleSelectPrimary = useCallback(
    (primary: string) => {
      if (!onSingleSelectChange) return
      if (singleSelectValue?.primary === primary && !singleSelectValue.detailed) {
        onSingleSelectChange(null)
      } else {
        onSingleSelectChange({ primary })
      }
    },
    [singleSelectValue, onSingleSelectChange]
  )

  const handleSingleSelectDetailed = useCallback(
    (primary: string, detailed: string) => {
      if (!onSingleSelectChange) return
      if (
        singleSelectValue?.primary === primary &&
        singleSelectValue.detailed === detailed
      ) {
        onSingleSelectChange(null)
      } else {
        onSingleSelectChange({ primary, detailed })
      }
    },
    [singleSelectValue, onSingleSelectChange]
  )

  // ─── Render helpers ──────────────────────────────────────────────────────

  function renderPrimaryRow(cat: PfcPrimaryOption) {
    const isExpanded =
      expanded.has(cat.primary) || (!!q && filteredCategories.some(
        (f) => f.cat.primary === cat.primary
      ))

    if (isSingleSelect) {
      const isSelected =
        singleSelectValue?.primary === cat.primary && !singleSelectValue.detailed

      return (
        <div key={cat.primary}>
          <div className="flex items-center">
            <button
              type="button"
              role="treeitem"
              aria-expanded={isExpanded}
              className={cn(treeRowVariants({ level: 'primary', selected: isSelected }), 'flex-1')}
              data-testid={`primary-row-${cat.primary}`}
              onClick={() => handleSingleSelectPrimary(cat.primary)}
            >
              <span className="flex-1">{cat.primaryLabel}</span>
              {isSelected && (
                <Check className="w-3.5 h-3.5 text-primary shrink-0" aria-hidden="true" />
              )}
            </button>
            <button
              type="button"
              className="p-1 hover:bg-accent rounded"
              data-testid={`expand-toggle-${cat.primary}`}
              onClick={() => handleExpandToggle(cat.primary)}
              aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${cat.primaryLabel}`}
            >
              {isExpanded ? (
                <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" aria-hidden="true" />
              ) : (
                <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" aria-hidden="true" />
              )}
            </button>
          </div>
          {isExpanded && (
            <div role="group">
              {renderDetailedRows(cat)}
            </div>
          )}
        </div>
      )
    }

    // Multi-select
    const checkState = getCheckState(cat, selectedPrimaries, selectedDetaileds)

    return (
      <div key={cat.primary}>
        <div className="flex items-center">
          <button
            type="button"
            role="treeitem"
            aria-expanded={isExpanded}
            className={cn(treeRowVariants({ level: 'primary' }), 'flex-1')}
            onClick={() => handlePrimaryCheckbox(cat.primary)}
          >
            <CheckboxIcon
              state={checkState}
              data-testid={`primary-checkbox-${cat.primary}`}
            />
            <span className="flex-1">{cat.primaryLabel}</span>
          </button>
          <button
            type="button"
            className="p-1 hover:bg-accent rounded"
            data-testid={`expand-toggle-${cat.primary}`}
            onClick={() => handleExpandToggle(cat.primary)}
            aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${cat.primaryLabel}`}
          >
            {isExpanded ? (
              <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" aria-hidden="true" />
            ) : (
              <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" aria-hidden="true" />
            )}
          </button>
        </div>
        {isExpanded && (
          <div role="group">
            {renderDetailedRows(cat)}
          </div>
        )}
      </div>
    )
  }

  function renderDetailedRows(cat: PfcPrimaryOption) {
    const filterEntry = filteredCategories.find((f) => f.cat.primary === cat.primary)
    const detailedsToShow =
      q && filterEntry && !filterEntry.showAll && filterEntry.matchingDetaileds
        ? filterEntry.matchingDetaileds
        : cat.detailed

    return detailedsToShow.map((d) => {
      if (isSingleSelect) {
        const isSelected =
          singleSelectValue?.primary === cat.primary &&
          singleSelectValue.detailed === d.value

        return (
          <button
            key={d.value}
            type="button"
            role="treeitem"
            className={cn(treeRowVariants({ level: 'detailed', selected: isSelected }))}
            data-testid={`detailed-row-${d.value}`}
            onClick={() => handleSingleSelectDetailed(cat.primary, d.value)}
          >
            <span className="flex-1">{d.label}</span>
            {isSelected && (
              <Check className="w-3.5 h-3.5 text-primary shrink-0" aria-hidden="true" />
            )}
          </button>
        )
      }

      const isChecked = selectedDetaileds.includes(d.value)

      return (
        <button
          key={d.value}
          type="button"
          role="treeitem"
          className={cn(treeRowVariants({ level: 'detailed' }))}
          onClick={() => handleDetailedCheckbox(d.value)}
          data-testid={`detailed-checkbox-${d.value}`}
        >
          <CheckboxIcon
            state={isChecked ? 'checked' : 'unchecked'}
          />
          <span className="flex-1">{d.label}</span>
        </button>
      )
    })
  }

  // ─── Determine which rows to show in the dropdown ─────────────────────────

  function renderRows() {
    if (!q) {
      return categories.map((cat) => renderPrimaryRow(cat))
    }
    return filteredCategories.map(({ cat }) => renderPrimaryRow(cat))
  }

  // ─── JSX ─────────────────────────────────────────────────────────────────

  return (
    <Popover onOpenChange={handleOpenChange}>
      <div className={cn('relative inline-block', className)}>
        <PopoverTrigger asChild>
          <button
            type="button"
            disabled={disabled}
            aria-label={ariaLabel}
            aria-haspopup="tree"
            className={cn(
              'relative inline-flex items-center justify-center w-6 h-6 p-1 border-none rounded cursor-pointer bg-transparent transition-colors duration-150',
              'disabled:opacity-50 disabled:cursor-not-allowed',
              isActive
                ? 'text-primary bg-primary/10 hover:bg-primary/20 hover:text-primary'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            <Filter className="w-4 h-4" aria-hidden="true" />
            {isActive && (
              <span
                data-testid="active-dot"
                className="absolute -top-1 -right-1 w-2 h-2 rounded-full bg-primary"
                aria-hidden="true"
              />
            )}
          </button>
        </PopoverTrigger>
      </div>
      <PopoverContent
        role="tree"
        aria-multiselectable={!isSingleSelect}
        className="w-64 p-0"
        align="start"
        onOpenAutoFocus={(e) => {
          // Let autoFocus on the search input handle focus; prevent Radix stealing it
          e.preventDefault()
        }}
      >
        {/* Search input */}
        <div className="p-2 border-b border-border">
          <input
            type="text"
            placeholder="Search categories..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className={cn(
              'w-full px-2.5 py-1 text-sm rounded-md border border-border bg-background',
              'placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary'
            )}
            aria-label="Search categories"
            autoFocus
          />
        </div>

        {/* Tree rows */}
        <div className="overflow-y-auto max-h-60 py-1">
          {q && filteredCategories.length === 0 ? (
            <div className="px-3 py-2 text-sm text-muted-foreground">
              No matching categories
            </div>
          ) : (
            renderRows()
          )}
        </div>

        {/* Footer: Clear (multi-select only) */}
        {!isSingleSelect && (
          <div className="p-2 border-t border-border flex justify-end">
            <button
              type="button"
              onClick={handleClear}
              className={cn(
                'px-3 py-1 text-sm rounded-md',
                'text-muted-foreground hover:text-foreground hover:bg-accent transition-colors'
              )}
              aria-label="Clear category filter"
            >
              Clear
            </button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
