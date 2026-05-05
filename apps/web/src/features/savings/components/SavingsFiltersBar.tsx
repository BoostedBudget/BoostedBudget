/**
 * SavingsFiltersBar Component
 * Issue #954: Savings page — T9
 *
 * Filter bar with Active/Completed/All tabs and a sort dropdown.
 * Uses shadcn Tabs (Radix UI) for accessible tab semantics.
 * Sort is a native <select> since shadcn Select is not available.
 */

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import type { SortKey, TabKey } from '../hooks/useSavingsGoalsView'

export type { SortKey, TabKey }

export interface SavingsFiltersBarProps {
  tab: TabKey
  onTabChange: (tab: TabKey) => void
  sortKey: SortKey
  onSortChange: (key: SortKey) => void
  goalCount: number
}

const TAB_LABELS: Record<TabKey, string> = {
  active: 'Active',
  completed: 'Completed',
  all: 'All',
}

const SORT_OPTIONS: { value: SortKey; label: string }[] = [
  { value: 'progress_desc', label: 'Progress: highest first' },
  { value: 'deadline_asc', label: 'Deadline: soonest first' },
  { value: 'target_desc', label: 'Target: highest first' },
]

export function SavingsFiltersBar({
  tab,
  onTabChange,
  sortKey,
  onSortChange,
  goalCount: _goalCount,
}: SavingsFiltersBarProps) {
  return (
    <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between mb-6">
      <Tabs value={tab} onValueChange={(v) => onTabChange(v as TabKey)}>
        <TabsList>
          {(Object.keys(TAB_LABELS) as TabKey[]).map((key) => (
            <TabsTrigger key={key} value={key}>
              {TAB_LABELS[key]}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <select
        aria-label="Sort goals"
        value={sortKey}
        onChange={(e) => onSortChange(e.target.value as SortKey)}
        className={cn(
          'rounded-md border border-input bg-background px-3 py-1.5 text-sm',
          'text-foreground shadow-sm transition-colors',
          'focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2',
          'cursor-pointer',
        )}
      >
        {SORT_OPTIONS.map(({ value, label }) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
    </div>
  )
}
