import { Badge } from '@/components/ui/badge'
import type { Environment } from '../hooks/useEnvironment'
import type { BadgeProps } from '@/components/ui/badge'

interface EnvironmentBadgeProps {
  environment: Environment
}

const envVariants: Record<string, BadgeProps['variant']> = {
  development: 'info',
  qa: 'warning',
  staging: 'warning',
  production: 'success',
}

export function EnvironmentBadge({ environment }: EnvironmentBadgeProps) {
  const variant = envVariants[environment] ?? 'default'

  return (
    <Badge variant={variant} className="tracking-wider">
      {environment.toUpperCase()}
    </Badge>
  )
}
