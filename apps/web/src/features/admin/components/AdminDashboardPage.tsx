import { useEffect } from 'react'
import { Users, CreditCard, Activity, AlertTriangle, Search, Wrench } from 'lucide-react'
import { useEnvironment } from '../hooks/useEnvironment'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { SummaryCard } from './SummaryCard'
import { QuickActionLink } from './QuickActionLink'
import { EnvironmentBadge } from './EnvironmentBadge'
import { ADMIN_ROUTES } from '../constants'
import './AdminDashboardPage.css'

export function AdminDashboardPage() {
  const { environment } = useEnvironment()
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'dashboard' })
  }, [trackEvent])

  return (
    <div className="admin-dashboard">
      <div className="admin-dashboard__header">
        <h1 className="admin-dashboard__title">Admin Dashboard</h1>
        <EnvironmentBadge environment={environment} />
      </div>

      <div className="admin-dashboard__cards">
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

      <section className="admin-dashboard__section">
        <h2 className="admin-dashboard__section-title">Quick Actions</h2>
        <div className="admin-dashboard__actions">
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
