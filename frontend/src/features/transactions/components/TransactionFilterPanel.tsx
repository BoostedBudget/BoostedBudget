/**
 * Transaction Filter Panel Component
 * Container for date and status filter controls
 * Issue #529: Transaction page filtering
 * Issue #590: Simplified - Account/Category/Amount filters moved inline to table headers
 */

import { useState, useEffect } from 'react'
import { ChevronDownIcon, XMarkIcon, FunnelIcon } from '@heroicons/react/24/outline'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetFooter,
  SheetTrigger,
} from '@/components/ui/sheet'
import type { TransactionFilters, DatePreset, StatusFilter } from '../types'
import {
  TransactionDateFilter,
  TransactionStatusFilter,
  FilterPill,
} from './filters'

interface TransactionFilterPanelProps {
  filters: TransactionFilters
  datePreset: DatePreset
  statusFilter: StatusFilter
  activeFilterCount: number
  hasActiveFilters: boolean
  pendingCount?: number
  onFilterChange: <K extends keyof TransactionFilters>(key: K, value: TransactionFilters[K]) => void
  onFiltersChange: (updates: Partial<TransactionFilters>) => void
  onDatePresetChange: (preset: DatePreset) => void
  onCustomDateChange: (startDate: string, endDate: string) => void
  onStatusFilterChange: (status: StatusFilter) => void
  onClearFilter: (key: keyof TransactionFilters) => void
  onClearAllFilters: () => void
  disabled?: boolean
}

// Props interface for FilterPillsSection component
interface FilterPillsSectionProps {
  dateDisplayValue: string | null
  statusDisplayValue: string | null
  onDatePresetChange: (preset: DatePreset) => void
  onStatusFilterChange: (status: StatusFilter) => void
}

// Extracted module-level component for filter pills
function FilterPillsSection({
  dateDisplayValue,
  statusDisplayValue,
  onDatePresetChange,
  onStatusFilterChange,
}: FilterPillsSectionProps) {
  return (
    <div className="flex flex-wrap gap-2">
      {dateDisplayValue && (
        <FilterPill
          label="Date"
          value={dateDisplayValue}
          onRemove={() => onDatePresetChange('all')}
        />
      )}
      {statusDisplayValue && (
        <FilterPill
          label="Status"
          value={statusDisplayValue}
          onRemove={() => onStatusFilterChange('all')}
        />
      )}
    </div>
  )
}

// Custom hook for detecting mobile viewport
function useIsMobile(breakpoint = 640) {
  const [isMobile, setIsMobile] = useState(
    typeof window !== 'undefined' ? window.innerWidth < breakpoint : false
  )

  useEffect(() => {
    if (typeof window === 'undefined') return

    const handleResize = () => {
      setIsMobile(window.innerWidth < breakpoint)
    }

    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [breakpoint])

  return isMobile
}

export function TransactionFilterPanel({
  filters,
  datePreset,
  statusFilter,
  // These props are kept for API compatibility but no longer used
  // since Account/Category/Amount filters moved inline to table headers
  activeFilterCount: _activeFilterCount,
  hasActiveFilters: _hasActiveFilters,
  pendingCount,
  onDatePresetChange,
  onCustomDateChange,
  onStatusFilterChange,
  onClearAllFilters: _onClearAllFilters,
  disabled,
}: TransactionFilterPanelProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [isSheetOpen, setIsSheetOpen] = useState(false)
  const isMobile = useIsMobile()

  // Get display values for filter pills
  const getDateDisplayValue = () => {
    if (datePreset === 'all') return null
    if (datePreset === 'custom' && filters.start_date && filters.end_date) {
      return `${filters.start_date} - ${filters.end_date}`
    }
    const presetLabels: Record<DatePreset, string> = {
      '7d': '7 days',
      '30d': '30 days',
      '90d': '90 days',
      '1y': '1 year',
      all: 'All time',
      custom: 'Custom',
    }
    return presetLabels[datePreset]
  }

  const getStatusDisplayValue = () => {
    if (statusFilter === 'all') return null
    return statusFilter === 'posted' ? 'Posted' : 'Pending'
  }

  // Prepare filter pills props
  const filterPillsProps: FilterPillsSectionProps = {
    dateDisplayValue: getDateDisplayValue(),
    statusDisplayValue: getStatusDisplayValue(),
    onDatePresetChange,
    onStatusFilterChange,
  }

  // Calculate active count for date/status only
  const dateStatusActiveCount = (datePreset !== 'all' ? 1 : 0) + (statusFilter !== 'all' ? 1 : 0)
  const hasDateStatusFilters = dateStatusActiveCount > 0

  // Mobile: Bottom sheet
  if (isMobile) {
    return (
      <div className="mb-4">
        {/* Mobile filter trigger button */}
        <Sheet open={isSheetOpen} onOpenChange={setIsSheetOpen}>
          <SheetTrigger asChild>
            <button
              type="button"
              className="w-full flex items-center justify-between px-4 py-3 bg-white border border-gray-200 rounded-lg shadow-sm hover:bg-gray-50 transition-colors"
            >
              <div className="flex items-center gap-2">
                <FunnelIcon className="w-5 h-5 text-gray-500" />
                <span className="font-medium text-gray-900">Date & Status</span>
                {dateStatusActiveCount > 0 && (
                  <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 rounded-full">
                    {dateStatusActiveCount}
                  </span>
                )}
              </div>
              <ChevronDownIcon className="w-5 h-5 text-gray-400" />
            </button>
          </SheetTrigger>

          <SheetContent side="bottom" className="max-h-[85vh] overflow-auto">
            <SheetHeader>
              <SheetTitle>Date & Status Filters</SheetTitle>
            </SheetHeader>

            <div className="py-4 overflow-auto">
              {/* Inline filter controls grid for mobile */}
              <div className="grid grid-cols-1 gap-4">
                <TransactionDateFilter
                  preset={datePreset}
                  startDate={filters.start_date}
                  endDate={filters.end_date}
                  onPresetChange={onDatePresetChange}
                  onCustomRangeChange={onCustomDateChange}
                  disabled={disabled}
                />
                <TransactionStatusFilter
                  value={statusFilter}
                  onChange={onStatusFilterChange}
                  pendingCount={pendingCount}
                  disabled={disabled}
                />
              </div>
            </div>

            <SheetFooter className="flex-row gap-2">
              {hasDateStatusFilters && (
                <button
                  type="button"
                  onClick={() => {
                    onDatePresetChange('all')
                    onStatusFilterChange('all')
                  }}
                  className="flex-1 px-4 py-2 text-sm text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
                >
                  Clear All
                </button>
              )}
              <button
                type="button"
                onClick={() => setIsSheetOpen(false)}
                className="flex-1 px-4 py-2 text-sm text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors"
              >
                Apply
              </button>
            </SheetFooter>
          </SheetContent>
        </Sheet>

        {/* Active filter pills (always visible when filters are active) */}
        {hasDateStatusFilters && (
          <div className="mt-2 px-1">
            <FilterPillsSection {...filterPillsProps} />
          </div>
        )}
      </div>
    )
  }

  // Desktop: Collapsible panel
  return (
    <div className="bg-white border border-gray-200 rounded-lg shadow-sm mb-4">
      {/* Header with toggle */}
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center justify-between px-4 py-3 text-left hover:bg-gray-50 rounded-lg transition-colors"
      >
        <div className="flex items-center gap-2">
          <FunnelIcon className="w-5 h-5 text-gray-500" />
          <span className="font-medium text-gray-900">Date & Status</span>
          {dateStatusActiveCount > 0 && (
            <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 rounded-full">
              {dateStatusActiveCount}
            </span>
          )}
        </div>
        <ChevronDownIcon
          className={`w-5 h-5 text-gray-400 transition-transform ${isExpanded ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Active filter pills (always visible when filters are active) */}
      {hasDateStatusFilters && !isExpanded && (
        <div className="px-4 pb-3 flex flex-wrap gap-2">
          <FilterPillsSection {...filterPillsProps} />
          <button
            type="button"
            onClick={() => {
              onDatePresetChange('all')
              onStatusFilterChange('all')
            }}
            className="text-xs text-gray-500 hover:text-gray-700 underline"
          >
            Clear all
          </button>
        </div>
      )}

      {/* Expanded filter controls */}
      {isExpanded && (
        <div className="px-4 pb-4 border-t border-gray-100">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-4">
            {/* Date Filter */}
            <TransactionDateFilter
              preset={datePreset}
              startDate={filters.start_date}
              endDate={filters.end_date}
              onPresetChange={onDatePresetChange}
              onCustomRangeChange={onCustomDateChange}
              disabled={disabled}
            />

            {/* Status Filter */}
            <TransactionStatusFilter
              value={statusFilter}
              onChange={onStatusFilterChange}
              pendingCount={pendingCount}
              disabled={disabled}
            />
          </div>

          {/* Clear all button */}
          {hasDateStatusFilters && (
            <div className="mt-4 pt-4 border-t border-gray-100 flex justify-end">
              <button
                type="button"
                onClick={() => {
                  onDatePresetChange('all')
                  onStatusFilterChange('all')
                }}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors"
              >
                <XMarkIcon className="w-4 h-4" />
                Clear filters
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
