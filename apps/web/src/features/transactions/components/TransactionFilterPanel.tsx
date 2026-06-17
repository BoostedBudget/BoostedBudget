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
import { Card } from '@/components/ui/card'
import type { TransactionFilters, DatePreset, StatusFilter, ActivityType } from '../types'
import { PRESETS } from '@/shared/dates'
import {
  TransactionDateFilter,
  TransactionInvestmentFilter,
  TransactionStatusFilter,
  FilterPill,
} from './filters'

interface TransactionFilterPanelProps {
  filters: TransactionFilters
  datePreset: DatePreset
  statusFilter: StatusFilter
  pendingCount?: number
  onDatePresetChange: (preset: DatePreset) => void
  onCustomDateChange: (startDate: string, endDate: string) => void
  onStatusFilterChange: (status: StatusFilter) => void
  onIncludeInvestmentActivityChange: (val: boolean) => void
  onActivityTypesChange: (types: ActivityType[]) => void
  onClearAll?: () => void
  disabled?: boolean
}

// Props interface for FilterPillsSection component
interface FilterPillsSectionProps {
  dateDisplayValue: string | null
  statusDisplayValue: string | null
  investmentDisplayValue: string | null
  activityTypesDisplayValue: string | null
  onDatePresetChange: (preset: DatePreset) => void
  onStatusFilterChange: (status: StatusFilter) => void
  onIncludeInvestmentActivityChange: (val: boolean) => void
  onActivityTypesChange: (types: ActivityType[]) => void
}

// Extracted module-level component for filter pills
function FilterPillsSection({
  dateDisplayValue,
  statusDisplayValue,
  investmentDisplayValue,
  activityTypesDisplayValue,
  onDatePresetChange,
  onStatusFilterChange,
  onIncludeInvestmentActivityChange,
  onActivityTypesChange,
}: FilterPillsSectionProps) {
  const pills: Array<{ show: string | null, label: string, value: string | null, onRemove: () => void }> = [
    { show: dateDisplayValue, label: 'Date', value: dateDisplayValue, onRemove: () => onDatePresetChange('all') },
    { show: statusDisplayValue, label: 'Status', value: statusDisplayValue, onRemove: () => onStatusFilterChange('all') },
    { show: investmentDisplayValue, label: 'Investments', value: investmentDisplayValue, onRemove: () => onIncludeInvestmentActivityChange(true) },
    { show: activityTypesDisplayValue, label: 'Activity', value: activityTypesDisplayValue, onRemove: () => onActivityTypesChange([]) },
  ]

  return (
    <div className="flex flex-wrap gap-2">
      {pills.filter(({ show }) => show).map(({ label, value, onRemove }) => (
        <FilterPill key={label} label={label} value={value!} onRemove={onRemove} />
      ))}
    </div>
  )
}

function useIsMobile(breakpoint = 640) {
  const [isMobile, setIsMobile] = useState(
    typeof window !== 'undefined' && window.innerWidth < breakpoint
  )

  useEffect(() => {
    if (typeof window === 'undefined') return
    const handleResize = () => setIsMobile(window.innerWidth < breakpoint)
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [breakpoint])

  return isMobile
}

export function TransactionFilterPanel({
  filters,
  datePreset,
  statusFilter,
  pendingCount,
  onDatePresetChange,
  onCustomDateChange,
  onStatusFilterChange,
  onIncludeInvestmentActivityChange,
  onActivityTypesChange,
  onClearAll,
  disabled,
}: TransactionFilterPanelProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [isSheetOpen, setIsSheetOpen] = useState(false)
  const isMobile = useIsMobile()

  const includeInvestmentActivity = filters.include_investment_activity ?? true
  const activityTypes = (filters.activity_types ?? []) as ActivityType[]

  // Consolidated clear all handler — use atomic onClearAll (single setSearchParams) when
  // provided to avoid the multi-call race where the last setSearchParams overwrites earlier
  // ones if they all read from the same original URL state.
  const handleClearAllFilters = () => {
    if (onClearAll) {
      onClearAll()
      return
    }
    onDatePresetChange('all')
    onStatusFilterChange('all')
    onIncludeInvestmentActivityChange(true)
    onActivityTypesChange([])
  }

  // Get display values for filter pills
  const getDateDisplayValue = () => {
    if (datePreset === 'all') return null
    if (datePreset === 'custom' && filters.start_date && filters.end_date) {
      return `${filters.start_date} - ${filters.end_date}`
    }
    const match = PRESETS.find((p) => p.value === datePreset)
    return match?.label ?? datePreset
  }

  const getStatusDisplayValue = () => {
    if (statusFilter === 'all') return null
    return statusFilter === 'posted' ? 'Posted' : 'Pending'
  }

  const getInvestmentDisplayValue = () => {
    if (includeInvestmentActivity) return null
    return 'Hidden'
  }

  const getActivityTypesDisplayValue = () => {
    if (!activityTypes.length) return null
    return activityTypes.length === 1 ? activityTypes[0] : `${activityTypes.length} types`
  }

  // Prepare filter pills props
  const filterPillsProps: FilterPillsSectionProps = {
    dateDisplayValue: getDateDisplayValue(),
    statusDisplayValue: getStatusDisplayValue(),
    investmentDisplayValue: getInvestmentDisplayValue(),
    activityTypesDisplayValue: getActivityTypesDisplayValue(),
    onDatePresetChange,
    onStatusFilterChange,
    onIncludeInvestmentActivityChange,
    onActivityTypesChange,
  }

  // Calculate active count for date/status/investment filters
  const investmentActiveCount = (!includeInvestmentActivity ? 1 : 0) + (activityTypes.length > 0 ? 1 : 0)
  const dateStatusActiveCount = (datePreset !== 'all' ? 1 : 0) + (statusFilter !== 'all' ? 1 : 0) + investmentActiveCount
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
              className="w-full flex items-center justify-between px-4 py-3 bg-card border border-border rounded-lg shadow-sm hover:bg-accent transition-colors"
            >
              <div className="flex items-center gap-2">
                <FunnelIcon className="w-5 h-5 text-muted-foreground" />
                <span className="font-medium text-foreground">Date & Status</span>
                {dateStatusActiveCount > 0 && (
                  <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-info/10 text-info rounded-full">
                    {dateStatusActiveCount}
                  </span>
                )}
              </div>
              <ChevronDownIcon className="w-5 h-5 text-muted-foreground" />
            </button>
          </SheetTrigger>

          <SheetContent side="bottom" className="max-h-[85vh] overflow-auto">
            <SheetHeader>
              <SheetTitle>Filters</SheetTitle>
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
                <TransactionInvestmentFilter
                  includeInvestmentActivity={includeInvestmentActivity}
                  activityTypes={activityTypes}
                  onIncludeChange={onIncludeInvestmentActivityChange}
                  onActivityTypesChange={onActivityTypesChange}
                  disabled={disabled}
                />
              </div>
            </div>

            <SheetFooter className="flex-row gap-2">
              {hasDateStatusFilters && (
                <button
                  type="button"
                  onClick={handleClearAllFilters}
                  className="flex-1 px-4 py-2 text-sm text-muted-foreground border border-input rounded-lg hover:bg-accent transition-colors"
                >
                  Clear All
                </button>
              )}
              <button
                type="button"
                onClick={() => setIsSheetOpen(false)}
                className="flex-1 px-4 py-2 text-sm text-primary-foreground bg-primary rounded-lg hover:bg-primary/90 transition-colors"
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
    <Card className="mb-4">
      {/* Header with toggle */}
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center justify-between px-4 py-3 text-left hover:bg-accent rounded-lg transition-colors"
      >
        <div className="flex items-center gap-2">
          <FunnelIcon className="w-5 h-5 text-muted-foreground" />
          <span className="font-medium text-foreground">Filters</span>
          {dateStatusActiveCount > 0 && (
            <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-info/10 text-info rounded-full">
              {dateStatusActiveCount}
            </span>
          )}
        </div>
        <ChevronDownIcon
          className={`w-5 h-5 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Active filter pills (always visible when filters are active) */}
      {hasDateStatusFilters && !isExpanded && (
        <div className="px-4 pb-3 flex flex-wrap gap-2">
          <FilterPillsSection {...filterPillsProps} />
          <button
            type="button"
            onClick={handleClearAllFilters}
            className="text-xs text-muted-foreground hover:text-foreground underline"
          >
            Clear all
          </button>
        </div>
      )}

      {/* Expanded filter controls */}
      {isExpanded && (
        <div className="px-4 pb-4 border-t border-border/50">
          <div className="flex flex-col gap-4 pt-4 lg:flex-row lg:items-start">
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

            {/* Investment Activity Filter */}
            <TransactionInvestmentFilter
              includeInvestmentActivity={includeInvestmentActivity}
              activityTypes={activityTypes}
              onIncludeChange={onIncludeInvestmentActivityChange}
              onActivityTypesChange={onActivityTypesChange}
              disabled={disabled}
            />
          </div>

          {/* Clear all button */}
          {hasDateStatusFilters && (
            <div className="mt-4 pt-4 border-t border-border/50 flex justify-end">
              <button
                type="button"
                onClick={handleClearAllFilters}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-lg transition-colors"
              >
                <XMarkIcon className="w-4 h-4" />
                Clear filters
              </button>
            </div>
          )}
        </div>
      )}
    </Card>
  )
}
