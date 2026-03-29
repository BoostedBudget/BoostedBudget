import { cn } from '@/lib/utils'
import type { Environment } from '../hooks/useEnvironment'
import './EnvironmentBadge.css'

interface EnvironmentBadgeProps {
  environment: Environment
}

export function EnvironmentBadge({ environment }: EnvironmentBadgeProps) {
  return (
    <span
      className={cn(
        'environment-badge',
        `environment-badge--${environment}`
      )}
    >
      {environment.toUpperCase()}
    </span>
  )
}
