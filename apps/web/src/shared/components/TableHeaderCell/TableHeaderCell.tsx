/**
 * TableHeaderCell Component
 * Issue #590: Enhanced Table Sorting and Inline Column Filtering
 *
 * A reusable table header cell that combines sorting and filtering capabilities.
 * Renders a sort button and/or filter dropdown based on props.
 */

import { useCallback } from 'react'
import type { ReactNode } from 'react'
import { ChevronUp, ChevronDown } from 'lucide-react'
import {
  ColumnFilterDropdown,
  type FilterOption,
  type RangeValue,
} from '../ColumnFilterDropdown'

export type SortDirection = 'asc' | 'desc'

export interface TableHeaderCellProps {
  /** Column label text */
  label: string
  /** Additional CSS class */
  className?: string

  // Sorting props
  /** Enable sorting for this column */
  sortable?: boolean
  /** Sort column identifier */
  sortColumn?: string
  /** Currently sorted column */
  currentSortColumn?: string
  /** Current sort direction */
  currentSortDirection?: SortDirection
  /** Called when sort changes */
  onSort?: (column: string, direction: SortDirection) => void

  // Filtering props
  /** Enable filtering for this column */
  filterable?: boolean
  /** Filter type: select (dropdown) or range (min/max) */
  filterType?: 'select' | 'range'
  /** Available options for select filter */
  filterOptions?: FilterOption[]
  /** Current filter value */
  filterValue?: string[] | RangeValue | null
  /** Enable multi-select for select filter */
  multiSelect?: boolean
  /** Enable search in multi-select filter */
  searchable?: boolean
  /** Called when filter changes */
  onFilterChange?: (value: string[] | RangeValue | null) => void
  /** Custom aria-label for filter button */
  filterAriaLabel?: string
  /**
   * When set, renders this ReactNode instead of the built-in ColumnFilterDropdown.
   * The component is responsible for its own trigger/popover and active-state styling.
   */
  customFilter?: ReactNode
  /**
   * When true, marks this column's header as having an active filter (used alongside customFilter).
   * Accepted for type compatibility; visual treatment is managed by the custom filter component.
   */
  isFilterActive?: boolean
}

export function TableHeaderCell({
  label,
  className = '',
  // Sorting
  sortable = false,
  sortColumn,
  currentSortColumn,
  currentSortDirection = 'asc',
  onSort,
  // Filtering
  filterable = false,
  filterType = 'select',
  filterOptions = [],
  filterValue,
  multiSelect = false,
  searchable = false,
  onFilterChange,
  filterAriaLabel,
  customFilter,
  isFilterActive: _isFilterActive,
}: TableHeaderCellProps) {
  const isActiveSortColumn = sortColumn === currentSortColumn

  const handleSort = useCallback(() => {
    if (!sortable || !onSort || !sortColumn) return

    const newDirection =
      isActiveSortColumn && currentSortDirection === 'asc' ? 'desc' : 'asc'
    onSort(sortColumn, newDirection)
  }, [sortable, onSort, sortColumn, isActiveSortColumn, currentSortDirection])

  const handleFilterChange = useCallback(
    (value: string[] | RangeValue | null) => {
      onFilterChange?.(value)
    },
    [onFilterChange]
  )

  // Determine filter value based on type
  const selectFilterValue = filterType === 'select' ? (filterValue as string[] | undefined) : undefined
  const rangeFilterValue = filterType === 'range' ? (filterValue as RangeValue | null | undefined) : undefined

  return (
    <div className={`flex items-center gap-1 ${className}`.trim()}>
      {sortable && sortColumn ? (
        <button
          type="button"
          onClick={handleSort}
          className={[
            'inline-flex items-center gap-1 p-0 border-none bg-transparent font-[inherit] font-semibold cursor-pointer transition-colors duration-150',
            isActiveSortColumn
              ? 'text-primary'
              : 'text-muted-foreground hover:text-foreground',
          ].join(' ')}
        >
          {label}
          <span className="inline-flex items-center w-4 h-4 [&>svg]:w-4 [&>svg]:h-4">
            {isActiveSortColumn && currentSortDirection === 'asc' && (
              <ChevronUp aria-hidden="true" />
            )}
            {isActiveSortColumn && currentSortDirection === 'desc' && (
              <ChevronDown aria-hidden="true" />
            )}
          </span>
        </button>
      ) : (
        <span className="font-semibold text-muted-foreground">{label}</span>
      )}

      {customFilter || (filterable && onFilterChange && (
        <ColumnFilterDropdown
          type={filterType}
          options={filterOptions}
          value={selectFilterValue}
          rangeValue={rangeFilterValue}
          onChange={handleFilterChange}
          multiSelect={multiSelect}
          searchable={searchable}
          ariaLabel={filterAriaLabel || `Filter by ${label.toLowerCase()}`}
          className="shrink-0 ml-1"
        />
      ))}
    </div>
  )
}
