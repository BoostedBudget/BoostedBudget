import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { StatCard } from '@/components/ui/stat-card'
import type { SummaryCardProps } from '../types'

const trendIcons = {
  up: TrendingUp,
  down: TrendingDown,
  neutral: Minus,
}

const statusAccentMap: Record<string, 'success' | 'warning' | 'destructive' | 'info'> = {
  success: 'success',
  warning: 'warning',
  error: 'destructive',
  neutral: 'info',
}

export function SummaryCard({ title, value, icon, trend, status = 'neutral' }: SummaryCardProps) {
  const TrendIcon = trend ? trendIcons[trend.direction] : null
  const prefix = trend?.direction === 'up' ? '+' : trend?.direction === 'down' ? '-' : ''

  const trendChip = trend ? (
    <span
      className={cn(
        'flex items-center gap-1 text-sm font-medium',
        trend.direction === 'up' && 'text-success',
        trend.direction === 'down' && 'text-destructive',
        trend.direction === 'neutral' && 'text-muted-foreground'
      )}
    >
      {TrendIcon && <TrendIcon size={16} />}
      <span>{prefix}{trend.value}</span>
    </span>
  ) : null

  return (
    <StatCard
      label={title}
      value={
        <span className="flex items-baseline gap-3">
          <span>{value}</span>
          {trendChip}
        </span>
      }
      icon={icon}
      accent={statusAccentMap[status] ?? 'info'}
    />
  )
}
