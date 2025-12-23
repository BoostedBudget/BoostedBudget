/**
 * Transaction Filter Panel Component
 * Container for all transaction filter controls with collapsible design
 * Mobile: Bottom sheet slide-up panel
 * Desktop: Collapsible panel above transaction list
 * Issue #529: Transaction page filtering
 */

import { useState, useEffect, useCallback } from 'react'
import { ChevronDownIcon, XMarkIcon, FunnelIcon } from '@heroicons/react/24/outline'
import { fetchAccounts } from '../../accounts/api/plaidApi'
import { fetchPlaidCategories } from '../../categories/api/categoriesApi'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetFooter,
  SheetTrigger,
} from '@/components/ui/sheet'
import type { Account } from '../../accounts/types'
import type { TransactionFilters, DatePreset, StatusFilter } from '../types'
import {
  TransactionAccountFilter,
  TransactionAmountFilter,
  TransactionCategoryFilter,
  TransactionDateFilter,
  TransactionStatusFilter,
  TransactionTypeFilter,
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
  activeFilterCount,
  hasActiveFilters,
  pendingCount,
  onFilterChange,
  onFiltersChange,
  onDatePresetChange,
  onCustomDateChange,
  onStatusFilterChange,
  onClearFilter,
  onClearAllFilters,
  disabled,
}: TransactionFilterPanelProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [isSheetOpen, setIsSheetOpen] = useState(false)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<string[]>([])
  const [loadingAccounts, setLoadingAccounts] = useState(true)
  const [loadingCategories, setLoadingCategories] = useState(true)
  const isMobile = useIsMobile()

  // Fetch accounts for the filter dropdown
  useEffect(() => {
    const loadAccounts = async () => {
      try {
        const response = await fetchAccounts()
        setAccounts(response.accounts ?? [])
      } catch (err) {
        console.error('Failed to load accounts for filter:', err)
      } finally {
        setLoadingAccounts(false)
      }
    }
    loadAccounts()
  }, [])

  // Fetch categories for the filter dropdown
  useEffect(() => {
    const loadCategories = async () => {
      try {
        const result = await fetchPlaidCategories()
        setCategories(result)
      } catch (err) {
        console.error('Failed to load categories for filter:', err)
      } finally {
        setLoadingCategories(false)
      }
    }
    loadCategories()
  }, [])

  // Handle account filter changes
  const handleAccountsChange = useCallback(
    (accountIds: string[]) => {
      onFilterChange('account_ids', accountIds.length > 0 ? accountIds : undefined)
    },
    [onFilterChange]
  )

  // Handle category filter changes
  const handleCategoriesChange = useCallback(
    (selectedCategories: string[]) => {
      onFilterChange('categories', selectedCategories.length > 0 ? selectedCategories : undefined)
    },
    [onFilterChange]
  )

  // Handle type filter changes
  const handleTypeChange = useCallback(
    (type: 'all' | 'income' | 'expense') => {
      onFilterChange('amount_type', type === 'all' ? undefined : type)
    },
    [onFilterChange]
  )

  // Handle amount filter changes - uses batch update to avoid race condition
  const handleAmountChange = useCallback(
    (min?: number, max?: number) => {
      onFiltersChange({
        min_amount: min,
        max_amount: max,
      })
    },
    [onFiltersChange]
  )

  // Get display values for filter pills
  const getAccountsDisplayValue = () => {
    if (!filters.account_ids || filters.account_ids.length === 0) return null
    if (filters.account_ids.length === 1) {
      const account = accounts.find((a) => a.id === filters.account_ids![0])
      return account?.name || 'Selected account'
    }
    return `${filters.account_ids.length} accounts`
  }

  const getCategoriesDisplayValue = () => {
    if (!filters.categories || filters.categories.length === 0) return null
    if (filters.categories.length === 1) return filters.categories[0]
    return `${filters.categories.length} categories`
  }

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

  const getAmountDisplayValue = () => {
    if (filters.min_amount === undefined && filters.max_amount === undefined) return null
    if (filters.min_amount !== undefined && filters.max_amount !== undefined) {
      return `$${filters.min_amount.toFixed(2)} - $${filters.max_amount.toFixed(2)}`
    }
    if (filters.min_amount !== undefined) {
      return `>= $${filters.min_amount.toFixed(2)}`
    }
    return `<= $${filters.max_amount!.toFixed(2)}`
  }

  const getTypeDisplayValue = () => {
    if (!filters.amount_type) return null
    return filters.amount_type === 'income' ? 'Income' : 'Expenses'
  }

  const getStatusDisplayValue = () => {
    if (statusFilter === 'all') return null
    return statusFilter === 'posted' ? 'Posted' : 'Pending'
  }

  // Shared filter controls grid (used in both mobile and desktop)
  const FilterControlsGrid = () => (
    <div className="grid grid-cols-1 gap-4">
      {/* Date Filter */}
      <TransactionDateFilter
        preset={datePreset}
        startDate={filters.start_date}
        endDate={filters.end_date}
        onPresetChange={onDatePresetChange}
        onCustomRangeChange={onCustomDateChange}
        disabled={disabled}
      />

      {/* Account Filter */}
      <TransactionAccountFilter
        accounts={accounts.map((a) => ({
          id: a.id,
          name: a.name,
          official_name: a.official_name,
          institution_name: a.institution_name,
          type: a.type,
        }))}
        selectedIds={filters.account_ids || []}
        onChange={handleAccountsChange}
        loading={loadingAccounts}
        disabled={disabled}
      />

      {/* Category Filter */}
      <TransactionCategoryFilter
        categories={categories}
        selectedCategories={filters.categories || []}
        onChange={handleCategoriesChange}
        loading={loadingCategories}
        disabled={disabled}
      />

      {/* Type Filter */}
      <TransactionTypeFilter
        value={filters.amount_type || 'all'}
        onChange={handleTypeChange}
        disabled={disabled}
      />

      {/* Amount Filter */}
      <TransactionAmountFilter
        minAmount={filters.min_amount}
        maxAmount={filters.max_amount}
        onChange={handleAmountChange}
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
  )

  // Filter pills component (shared)
  const FilterPills = () => (
    <div className="flex flex-wrap gap-2">
      {getDateDisplayValue() && (
        <FilterPill
          label="Date"
          value={getDateDisplayValue()!}
          onRemove={() => onDatePresetChange('all')}
        />
      )}
      {getAccountsDisplayValue() && (
        <FilterPill
          label="Accounts"
          value={getAccountsDisplayValue()!}
          onRemove={() => onClearFilter('account_ids')}
        />
      )}
      {getCategoriesDisplayValue() && (
        <FilterPill
          label="Categories"
          value={getCategoriesDisplayValue()!}
          onRemove={() => onClearFilter('categories')}
        />
      )}
      {getTypeDisplayValue() && (
        <FilterPill
          label="Type"
          value={getTypeDisplayValue()!}
          onRemove={() => onClearFilter('amount_type')}
        />
      )}
      {getAmountDisplayValue() && (
        <FilterPill
          label="Amount"
          value={getAmountDisplayValue()!}
          onRemove={() => {
            onClearFilter('min_amount')
            onClearFilter('max_amount')
          }}
        />
      )}
      {getStatusDisplayValue() && (
        <FilterPill
          label="Status"
          value={getStatusDisplayValue()!}
          onRemove={() => onStatusFilterChange('all')}
        />
      )}
    </div>
  )

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
                <span className="font-medium text-gray-900">Filters</span>
                {activeFilterCount > 0 && (
                  <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 rounded-full">
                    {activeFilterCount}
                  </span>
                )}
              </div>
              <ChevronDownIcon className="w-5 h-5 text-gray-400" />
            </button>
          </SheetTrigger>

          <SheetContent side="bottom" className="max-h-[85vh] overflow-auto">
            <SheetHeader>
              <SheetTitle>Filters</SheetTitle>
            </SheetHeader>

            <div className="py-4 overflow-auto">
              <FilterControlsGrid />
            </div>

            <SheetFooter className="flex-row gap-2">
              {hasActiveFilters && (
                <button
                  type="button"
                  onClick={() => {
                    onClearAllFilters()
                  }}
                  className="flex-1 px-4 py-2 text-sm text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
                >
                  Clear All ({activeFilterCount})
                </button>
              )}
              <button
                type="button"
                onClick={() => setIsSheetOpen(false)}
                className="flex-1 px-4 py-2 text-sm text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors"
              >
                Apply Filters
              </button>
            </SheetFooter>
          </SheetContent>
        </Sheet>

        {/* Active filter pills (always visible when filters are active) */}
        {hasActiveFilters && (
          <div className="mt-2 px-1">
            <FilterPills />
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
          <span className="font-medium text-gray-900">Filters</span>
          {activeFilterCount > 0 && (
            <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 rounded-full">
              {activeFilterCount}
            </span>
          )}
        </div>
        <ChevronDownIcon
          className={`w-5 h-5 text-gray-400 transition-transform ${isExpanded ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Active filter pills (always visible when filters are active) */}
      {hasActiveFilters && !isExpanded && (
        <div className="px-4 pb-3 flex flex-wrap gap-2">
          <FilterPills />
          <button
            type="button"
            onClick={onClearAllFilters}
            className="text-xs text-gray-500 hover:text-gray-700 underline"
          >
            Clear all
          </button>
        </div>
      )}

      {/* Expanded filter controls */}
      {isExpanded && (
        <div className="px-4 pb-4 border-t border-gray-100">
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 pt-4">
            {/* Date Filter */}
            <TransactionDateFilter
              preset={datePreset}
              startDate={filters.start_date}
              endDate={filters.end_date}
              onPresetChange={onDatePresetChange}
              onCustomRangeChange={onCustomDateChange}
              disabled={disabled}
            />

            {/* Account Filter */}
            <TransactionAccountFilter
              accounts={accounts.map((a) => ({
                id: a.id,
                name: a.name,
                official_name: a.official_name,
                institution_name: a.institution_name,
                type: a.type,
              }))}
              selectedIds={filters.account_ids || []}
              onChange={handleAccountsChange}
              loading={loadingAccounts}
              disabled={disabled}
            />

            {/* Category Filter */}
            <TransactionCategoryFilter
              categories={categories}
              selectedCategories={filters.categories || []}
              onChange={handleCategoriesChange}
              loading={loadingCategories}
              disabled={disabled}
            />

            {/* Type Filter */}
            <TransactionTypeFilter
              value={filters.amount_type || 'all'}
              onChange={handleTypeChange}
              disabled={disabled}
            />

            {/* Amount Filter */}
            <TransactionAmountFilter
              minAmount={filters.min_amount}
              maxAmount={filters.max_amount}
              onChange={handleAmountChange}
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
          {hasActiveFilters && (
            <div className="mt-4 pt-4 border-t border-gray-100 flex justify-end">
              <button
                type="button"
                onClick={onClearAllFilters}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors"
              >
                <XMarkIcon className="w-4 h-4" />
                Clear {activeFilterCount} filter{activeFilterCount !== 1 ? 's' : ''}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
