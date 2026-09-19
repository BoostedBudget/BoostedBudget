export const ADMIN_ROUTES = {
  DASHBOARD: '/admin',
  USERS: '/admin/users',
  HEALTH: '/admin/health',
  REVENUECAT: '/admin/revenuecat',
  WEBHOOKS: '/admin/webhooks',
  DEAD_LETTER: '/admin/dead-letter',
  // Issue #1497 — Plaid Items stranded by a completed GDPR erasure. Path mirrors the endpoint
  // (/api/v1/admin/plaid/orphaned-items) so the two are recognisably the same subject.
  PLAID_ORPHANED_ITEMS: '/admin/plaid/orphaned-items',
} as const

export const ADMIN_NAV_ITEMS = [
  { to: ADMIN_ROUTES.DASHBOARD, label: 'Admin', icon: 'Shield' },
] as const

export const ADMIN_SIDEBAR_ITEMS = [
  { to: ADMIN_ROUTES.DASHBOARD, label: 'Dashboard', icon: 'LayoutDashboard' },
  { to: ADMIN_ROUTES.USERS, label: 'Users', icon: 'Users' },
  { to: ADMIN_ROUTES.HEALTH, label: 'System Health', icon: 'Activity' },
  { to: ADMIN_ROUTES.REVENUECAT, label: 'RevenueCat', icon: 'CreditCard' },
  { to: ADMIN_ROUTES.WEBHOOKS, label: 'Webhooks', icon: 'Webhook' },
  { to: ADMIN_ROUTES.DEAD_LETTER, label: 'Dead Letter Queue', icon: 'AlertTriangle' },
  { to: ADMIN_ROUTES.PLAID_ORPHANED_ITEMS, label: 'Orphaned Plaid Items', icon: 'Unplug' },
] as const

export const ADMIN_MODE_STORAGE_KEY = 'boosted-admin-mode'
