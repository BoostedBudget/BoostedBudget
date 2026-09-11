import type { ActivityType } from '@/features/transactions/types'
import type { VariantProps } from 'class-variance-authority'
import {
  TrendingUp,
  TrendingDown,
  Coins,
  RefreshCw,
  Receipt,
  DollarSign,
  Circle,
  Percent,
  ArrowLeftRight,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { badgeVariants } from '@/components/ui/badge'

interface ActivityTypeConfig {
  label: string
  variant: VariantProps<typeof badgeVariants>['variant']
  Icon: LucideIcon
}

export const ACTIVITY_TYPE_CONFIG: Record<ActivityType, ActivityTypeConfig> = {
  buy:      { label: 'Buy',      variant: 'info',        Icon: TrendingUp },
  sell:     { label: 'Sell',     variant: 'secondary',   Icon: TrendingDown },
  dividend: { label: 'Dividend', variant: 'success',     Icon: Coins },
  reinvest: { label: 'Reinvest', variant: 'info',        Icon: RefreshCw },
  interest: { label: 'Interest', variant: 'success',     Icon: Percent },
  fee:      { label: 'Fee',      variant: 'destructive', Icon: Receipt },
  transfer: { label: 'Transfer', variant: 'secondary',   Icon: ArrowLeftRight },
  cash:     { label: 'Cash',     variant: 'secondary',   Icon: DollarSign },
  other:    { label: 'Other',    variant: 'outline',     Icon: Circle },
}

/**
 * ACTIVITY_TYPES is the single ordered list of activity types, for filter UIs and tests.
 *
 * ⚠ Deliberately a hand-written literal rather than `Object.keys(ACTIVITY_TYPE_CONFIG)`. Deriving
 * it at module level triggers a cross-chunk TDZ error in the Rolldown production bundle — a fault
 * that passes every unit test and typecheck and breaks only the built app. See the note this
 * replaced in TransactionInvestmentFilter.tsx, which carried its own copy of the list for exactly
 * this reason.
 *
 * Drift is caught by a test instead, where Object.keys is safe:
 * activityTypeConfig.test.ts asserts this list equals the config's keys.
 */
export const ACTIVITY_TYPES: readonly ActivityType[] = [
  'buy',
  'sell',
  'dividend',
  'reinvest',
  'interest',
  'fee',
  'transfer',
  'cash',
  'other',
]

// Fallback for unknown/null activity type values
export const ACTIVITY_TYPE_FALLBACK: ActivityTypeConfig = ACTIVITY_TYPE_CONFIG.other
