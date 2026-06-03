export const ADMIN_ROUTES = {
  DASHBOARD: '/admin',
  USERS: '/admin/users',
  HEALTH: '/admin/health',
  WEBHOOKS: '/admin/webhooks',
  DEAD_LETTER: '/admin/dead-letter',
} as const

export const ADMIN_NAV_ITEMS = [
  { to: ADMIN_ROUTES.DASHBOARD, label: 'Admin', icon: 'Shield' },
] as const

export const ADMIN_SIDEBAR_ITEMS = [
  { to: ADMIN_ROUTES.DASHBOARD, label: 'Dashboard', icon: 'LayoutDashboard' },
  { to: ADMIN_ROUTES.USERS, label: 'Users', icon: 'Users' },
  { to: ADMIN_ROUTES.HEALTH, label: 'System Health', icon: 'Activity' },
  { to: ADMIN_ROUTES.WEBHOOKS, label: 'Webhooks', icon: 'Webhook' },
  { to: ADMIN_ROUTES.DEAD_LETTER, label: 'Dead Letter Queue', icon: 'AlertTriangle' },
] as const

export const ADMIN_MODE_STORAGE_KEY = 'boosted-admin-mode'
