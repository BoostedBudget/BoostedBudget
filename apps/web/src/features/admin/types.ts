export interface AdminStats {
  totalUsers: number
  activeSubscriptions: { [tier: string]: number }
  systemStatus: 'healthy' | 'degraded' | 'down'
  failedJobs: number
}

export interface AdminSettingsSectionProps {
  adminModeEnabled: boolean
  onToggle: () => void
}

export interface SummaryCardProps {
  title: string
  value: string | number
  icon: React.ReactNode
  trend?: {
    value: number
    direction: 'up' | 'down' | 'neutral'
  }
  status?: 'success' | 'warning' | 'error' | 'neutral'
}

export interface QuickActionLinkProps {
  to: string
  label: string
  description: string
  icon: React.ReactNode
}

export interface AdminNavItem {
  to: string
  label: string
  icon: string
}
