import { matchPath } from 'react-router'

export const POSTHOG_ENGAGEMENT_ROUTES = {
  dashboard: '/dashboard',
  accounts: '/accounts',
  reconnectAccount: '/accounts/reconnect',
  account: '/accounts/:accountId',
  transactions: '/transactions',
  review: '/review',
  insights: '/insights',
  planning: '/planning',
  budget: '/budgets/:budgetId',
  autoCategorization: '/auto-categorization',
  boosts: '/boosts',
  newBoost: '/boosts/new',
  boost: '/boosts/:id',
  editBoost: '/boosts/:id/edit',
  profile: '/profile',
  billingManage: '/billing/manage',
  billingSuccess: '/billing/success',
} as const

export function isPostHogEngagementRoute(pathname: string): boolean {
  return Object.values(POSTHOG_ENGAGEMENT_ROUTES)
    .some(path => matchPath({ path, end: true }, pathname) !== null)
}
