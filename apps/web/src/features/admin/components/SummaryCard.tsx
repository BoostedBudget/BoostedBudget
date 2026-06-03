import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { SummaryCardProps } from '../types'

const trendIcons = {
  up: TrendingUp,
  down: TrendingDown,
  neutral: Minus,
}

const statusBorderMap: Record<string, string> = {
  success: 'border-l-success',
  warning: 'border-l-warning',
  error: 'border-l-destructive',
  neutral: 'border-l-info',
}

export function SummaryCard({ title, value, icon, trend, status = 'neutral' }: SummaryCardProps) {
  return (
    <div
      className={cn(
        'bg-card rounded-lg border border-border border-l-4 p-5 shadow-sm hover:shadow-md transition-shadow',
        statusBorderMap[status] ?? 'border-l-info'
      )}
    >
      <div className="flex items-center gap-2 mb-3">
        <span className="text-muted-foreground">{icon}</span>
        <span className="text-sm font-medium text-muted-foreground uppercase tracking-wide">{title}</span>
      </div>

      <div className="flex items-baseline gap-3">
        <span className="text-3xl font-bold text-foreground max-xs:text-2xl">{value}</span>

        {trend && (
          <span
            className={cn(
              'flex items-center gap-1 text-sm font-medium',
              trend.direction === 'up' && 'text-success',
              trend.direction === 'down' && 'text-destructive',
              trend.direction === 'neutral' && 'text-muted-foreground'
            )}
          >
            {(() => {
              const Icon = trendIcons[trend.direction]
              const prefix = trend.direction === 'up' ? '+' : trend.direction === 'down' ? '-' : ''
              return (
                <>
                  <Icon size={16} />
                  <span>{prefix}{trend.value}</span>
                </>
              )
            })()}
          </span>
        )}
      </div>
    </div>
  )
}
