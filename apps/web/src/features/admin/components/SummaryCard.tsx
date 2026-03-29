import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { SummaryCardProps } from '../types'
import './SummaryCard.css'

const trendIcons = {
  up: TrendingUp,
  down: TrendingDown,
  neutral: Minus,
}

export function SummaryCard({ title, value, icon, trend, status = 'neutral' }: SummaryCardProps) {
  return (
    <div
      className={cn(
        'summary-card',
        `summary-card--${status}`
      )}
    >
      <div className="summary-card__header">
        <span className="summary-card__icon">{icon}</span>
        <span className="summary-card__title">{title}</span>
      </div>

      <div className="summary-card__body">
        <span className="summary-card__value">{value}</span>

        {trend && (
          <span className={cn('summary-card__trend', `summary-card__trend--${trend.direction}`)}>
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
