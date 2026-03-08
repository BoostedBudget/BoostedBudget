/**
 * WidgetEmptyState
 * Issue #834: Dashboard empty states with context-aware CTAs
 *
 * Reusable empty state for dashboard widgets. Reads emptyState config from
 * the widget registry and uses hasAccounts from DashboardContext to show
 * the right message and CTA.
 */

import { Link } from 'react-router'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { useDashboardContext } from '../context/DashboardContext'
import { getWidget } from './registry'
import type { WidgetType } from '../types'

interface WidgetEmptyStateProps {
  widgetType: WidgetType | string
  className?: string
}

export function WidgetEmptyState({ widgetType, className }: WidgetEmptyStateProps) {
  const { hasAccounts } = useDashboardContext()
  const definition = getWidget(widgetType as WidgetType)
  const config = definition?.emptyState

  if (!config) {
    return (
      <EmptyState
        size="compact"
        icon={<span />}
        title="No data available"
        className={className ?? 'h-full'}
      />
    )
  }

  const description = hasAccounts ? config.noDataDescription : config.noAccountsDescription
  const action = !hasAccounts && (config.noAccountsAction ?? (
    <Button size="sm" asChild>
      <Link to="/accounts">Connect Account</Link>
    </Button>
  ))

  return (
    <EmptyState
      size="compact"
      icon={config.icon}
      title={config.title}
      description={description}
      action={action}
      className={className ?? 'h-full'}
    />
  )
}
