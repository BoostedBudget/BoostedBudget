import { cn } from '@/lib/utils'
import type { Environment } from '../hooks/useEnvironment'

interface EnvironmentBadgeProps {
  environment: Environment
}

const envStyles: Record<string, string> = {
  development: 'bg-info/10 text-info border border-info/20',
  qa: 'bg-warning/10 text-warning border border-warning/20',
  staging: 'bg-warning/10 text-warning border border-warning/20',
  production: 'bg-success/10 text-success border border-success/20',
}

export function EnvironmentBadge({ environment }: EnvironmentBadgeProps) {
  return (
    <span
      className={cn(
        'inline-block px-3 py-1 text-xs font-semibold tracking-wider rounded-full',
        envStyles[environment] ?? 'bg-muted text-muted-foreground border border-border'
      )}
    >
      {environment.toUpperCase()}
    </span>
  )
}
