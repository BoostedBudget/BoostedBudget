import type { ActivityType } from '@/features/transactions/types'
import type { VariantProps } from 'class-variance-authority'
import { TrendingUp, TrendingDown, Coins, RefreshCw, Receipt, DollarSign, Circle } from 'lucide-react'
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
  fee:      { label: 'Fee',      variant: 'destructive', Icon: Receipt },
  cash:     { label: 'Cash',     variant: 'secondary',   Icon: DollarSign },
  other:    { label: 'Other',    variant: 'outline',     Icon: Circle },
}

// Fallback for unknown/null activity type values
export const ACTIVITY_TYPE_FALLBACK: ActivityTypeConfig = ACTIVITY_TYPE_CONFIG.other
