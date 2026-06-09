import * as React from "react"
import { cn } from "@/lib/utils"
import { Card } from "./card"

export interface StatCardProps extends React.HTMLAttributes<HTMLDivElement> {
  label: string
  value: React.ReactNode
  icon?: React.ReactNode
  /** 'up' applies text-success, 'down' applies text-destructive, 'neutral' / undefined is default foreground */
  trend?: 'up' | 'down' | 'neutral'
  /** When true, renders data-ph-mask on the value element (PostHog PII masking) */
  maskValue?: boolean
}

const TREND_CLASS: Record<NonNullable<StatCardProps['trend']>, string> = {
  up: 'text-success',
  down: 'text-destructive',
  neutral: 'text-foreground',
}

/**
 * StatCard — compact summary stat card used in dashboards and summary headers.
 * Promotes the hand-rolled stat card pattern from SavingsSummaryHeader to a shared primitive.
 */
function StatCard({ label, value, icon, trend, maskValue, className, ...props }: StatCardProps) {
  return (
    <Card className={cn("flex flex-col gap-1", className)} {...props}>
      <div className="flex items-center justify-between">
        <p
          data-slot="stat-label"
          className="text-xs text-muted-foreground uppercase tracking-wide"
        >
          {label}
        </p>
        {icon && (
          <span data-slot="stat-icon" className="text-muted-foreground" aria-hidden="true">
            {icon}
          </span>
        )}
      </div>
      <p
        data-slot="stat-value"
        className={cn(
          "text-2xl font-bold",
          trend ? TREND_CLASS[trend] : "text-foreground",
        )}
        {...(maskValue && { 'data-ph-mask': '' })}
      >
        {value}
      </p>
    </Card>
  )
}

export { StatCard }
