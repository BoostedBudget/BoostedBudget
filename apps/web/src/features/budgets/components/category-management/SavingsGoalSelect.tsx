/**
 * SavingsGoalSelect
 * Reusable select for choosing a savings goal (linked to budget categories).
 */

import { useMemo } from 'react'
import { cn } from '@/lib/utils'
import { AlertCircleIcon } from 'lucide-react'
import { useGetSavingsGoals } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'

type SavingsGoalItem = {
  id: string
  name: string
  target_amount: number
  current_amount: number
}

export interface SavingsGoalSelectProps {
  /** Currently selected goal id (empty string = none) */
  value: string
  /** Called with the new goal id when selection changes */
  onChange: (goalId: string) => void
  /** Disable all interaction */
  disabled?: boolean
  /** HTML id for the underlying <select> */
  id?: string
  /** Additional class names */
  className?: string
}

/**
 * A controlled select for choosing a savings goal.
 * Fetches available goals via `useGetSavingsGoals`.
 */
export function SavingsGoalSelect({
  value,
  onChange,
  disabled = false,
  id = 'savings-goal',
  className,
}: SavingsGoalSelectProps) {
  const { data: savingsGoals, isLoading, isError } = useGetSavingsGoals()
  const goalsList = useMemo(() => (savingsGoals ?? []) as SavingsGoalItem[], [savingsGoals])

  if (isError) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2.5 text-sm text-destructive">
        <AlertCircleIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
        <span>Couldn&apos;t load savings goals. Please try again.</span>
      </div>
    )
  }

  return (
    <select
      id={id}
      role="combobox"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled || isLoading}
      className={cn(
        'w-full cursor-pointer rounded-lg border border-input bg-card px-3 py-2.5 text-sm text-foreground transition-[border-color,box-shadow] duration-150 focus:border-primary focus:outline-none focus:ring-[3px] focus:ring-ring/15 disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
    >
      <option value="">Select a savings goal…</option>
      {goalsList.map((goal) => (
        <option key={goal.id} value={goal.id} data-ph-mask>
          {goal.name}
        </option>
      ))}
    </select>
  )
}
