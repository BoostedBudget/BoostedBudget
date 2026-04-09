export const ANALYTICS_EVENTS = {
  // Landing
  LANDING_CTA_CLICKED: 'landing_cta_clicked',
  PRICING_VIEWED: 'pricing_viewed',

  // Auth
  SIGN_UP_COMPLETED: 'sign_up_completed',
  SIGN_IN_COMPLETED: 'sign_in_completed',
  SIGN_OUT_COMPLETED: 'sign_out_completed',

  // Onboarding
  FIRST_ACCOUNT_LINKED: 'first_account_linked',
  FIRST_BUDGET_CREATED: 'first_budget_created',
  GETTING_STARTED_VIEWED: 'getting_started_viewed',
  GETTING_STARTED_DISMISSED: 'getting_started_dismissed',
  GETTING_STARTED_STEP_CLICKED: 'getting_started_step_clicked',
  GETTING_STARTED_COMPLETED: 'getting_started_completed',

  // Accounts
  PLAID_LINK_STARTED: 'plaid_link_started',
  PLAID_LINK_SUCCEEDED: 'plaid_link_succeeded',
  PLAID_LINK_FAILED: 'plaid_link_failed',
  ACCOUNT_REMOVED: 'account_removed',

  // Transactions
  TRANSACTIONS_VIEWED: 'transactions_viewed',
  TRANSACTION_CATEGORIZED: 'transaction_categorized',
  TRANSACTION_SEARCHED: 'transaction_searched',

  // Budgets
  BUDGET_CREATED: 'budget_created',
  BUDGET_EDITED: 'budget_edited',
  BUDGET_DELETED: 'budget_deleted',
  BUDGET_DETAIL_VIEWED: 'budget_detail_viewed',

  // Rules
  RULE_CREATED: 'rule_created',
  RULE_EDITED: 'rule_edited',
  RULE_DELETED: 'rule_deleted',

  // Auto-categorization
  AUTO_CATEGORIZATION_VIEWED: 'auto_categorization_viewed',

  // Dashboard
  DASHBOARD_VIEWED: 'dashboard_viewed',
  WIDGET_ADDED: 'widget_added',
  WIDGET_REMOVED: 'widget_removed',
  TIMEFRAME_CHANGED: 'timeframe_changed',

  // Billing
  CHECKOUT_STARTED: 'checkout_started',
  CHECKOUT_COMPLETED: 'checkout_completed',
  SUBSCRIPTION_MANAGED: 'subscription_managed',

  // Profile
  PRIVACY_SETTINGS_CHANGED: 'privacy_settings_changed',
  PROFILE_UPDATED: 'profile_updated',
  THEME_CHANGED: 'theme_changed',

  // Reports
  REPORTS_VIEWED: 'reports_viewed',
  REPORT_DATE_CHANGED: 'report_date_changed',
  REPORT_CATEGORY_CLICKED: 'report_category_clicked',
  REPORT_MERCHANT_CLICKED: 'report_merchant_clicked',

  // Admin
  ADMIN_PAGE_VIEWED: 'admin_page_viewed',

  // Notifications (#952)
  NOTIFICATION_DRAWER_OPENED: 'notification_drawer_opened',
  NOTIFICATION_READ: 'notification_read',
  NOTIFICATION_ALL_READ: 'notification_all_read',
  NOTIFICATION_ACTION_CLICKED: 'notification_action_clicked',
  TRANSFER_SUGGESTION_ACCEPTED: 'transfer_suggestion_accepted',
  TRANSFER_SUGGESTION_REJECTED: 'transfer_suggestion_rejected',
} as const

export type AnalyticsEventName = (typeof ANALYTICS_EVENTS)[keyof typeof ANALYTICS_EVENTS]
