import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface NetWorthSectionProps {
  title?: ReactNode
  action?: ReactNode
  children: ReactNode
  className?: string
}

export function NetWorthSection({ title, action, children, className }: NetWorthSectionProps) {
  return (
    <div className={cn('rounded-xl border border-border bg-card p-4', className)}>
      {(title || action) && (
        <div className="flex items-center justify-between mb-4">
          {title && <h2 className="text-lg font-semibold">{title}</h2>}
          {action && <div>{action}</div>}
        </div>
      )}
      {children}
    </div>
  )
}
