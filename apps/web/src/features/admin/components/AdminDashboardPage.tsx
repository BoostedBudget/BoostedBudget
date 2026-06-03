import { useEffect } from 'react'
import { Users, CreditCard, Activity, AlertTriangle, Search, Wrench } from 'lucide-react'
import { useEnvironment } from '../hooks/useEnvironment'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { SummaryCard } from './SummaryCard'
import { QuickActionLink } from './QuickActionLink'
import { EnvironmentBadge } from './EnvironmentBadge'
import { ADMIN_ROUTES } from '../constants'

export function AdminDashboardPage() {
  const { environment } = useEnvironment()
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'dashboard' })
  }, [trackEvent])

  return (
    <div className="max-w-[1200px]">
      <div className="flex items-center justify-between mb-8 max-sm:flex-col max-sm:items-start max-sm:gap-3">
        <h1 className="text-[1.75rem] font-bold text-foreground m-0 max-sm:text-2xl">Admin Dashboard</h1>
        <EnvironmentBadge environment={environment} />
      </div>

      <div className="grid gap-6 mb-8 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))] max-sm:[grid-template-columns:1fr]">
        <SummaryCard
          title="Total Users"
          value="--"
          icon={<Users size={20} />}
          status="neutral"
        />
        <SummaryCard
          title="Active Subscriptions"
          value="--"
          icon={<CreditCard size={20} />}
          status="neutral"
        />
        <SummaryCard
          title="System Status"
          value="Healthy"
          icon={<Activity size={20} />}
          status="success"
        />
        <SummaryCard
          title="Failed Jobs"
          value="--"
          icon={<AlertTriangle size={20} />}
          status="neutral"
        />
      </div>

      <section className="bg-card border border-border rounded-lg p-6 mb-6">
        <h2 className="text-lg font-semibold text-foreground m-0 mb-4">Quick Actions</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(280px,1fr))] max-sm:[grid-template-columns:1fr]">
          <QuickActionLink
            to={ADMIN_ROUTES.USERS}
            label="Manage Users"
            description="View and manage user accounts"
            icon={<Search size={20} />}
          />
          <QuickActionLink
            to={ADMIN_ROUTES.HEALTH}
            label="System Health"
            description="View system health and job queues"
            icon={<Wrench size={20} />}
          />
        </div>
      </section>
    </div>
  )
}
