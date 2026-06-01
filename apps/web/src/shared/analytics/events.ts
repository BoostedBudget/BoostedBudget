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
  BUDGET_CATEGORY_ADDED: 'budget_category_added',
  BUDGET_CATEGORY_UPDATED: 'budget_category_updated',
  BUDGET_CATEGORY_DELETED: 'budget_category_deleted',

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
  ADMIN_DLQ_RETRY: 'admin_dlq_retry',
  ADMIN_DLQ_DELETE: 'admin_dlq_delete',
  ADMIN_DLQ_BULK_DELETE: 'admin_dlq_bulk_delete',

  // Notifications (#952)
  NOTIFICATION_DRAWER_OPENED: 'notification_drawer_opened',
  NOTIFICATION_READ: 'notification_read',
  NOTIFICATION_ALL_READ: 'notification_all_read',
  NOTIFICATION_ACTION_CLICKED: 'notification_action_clicked',
  TRANSFER_SUGGESTION_ACCEPTED: 'transfer_suggestion_accepted',
  TRANSFER_SUGGESTION_REJECTED: 'transfer_suggestion_rejected',

  // Net Worth (#955)
  NET_WORTH_PAGE_VIEWED: 'net_worth_page_viewed',
  NET_WORTH_RANGE_CHANGED: 'net_worth_range_changed',

  // Savings Goals (#954)
  SAVINGS_PAGE_VIEWED: 'savings_page_viewed',
  SAVINGS_GOAL_CREATED: 'savings_goal_created',
  SAVINGS_GOAL_EDITED: 'savings_goal_edited',
  SAVINGS_GOAL_DELETED: 'savings_goal_deleted',
  SAVINGS_FILTER_CHANGED: 'savings_filter_changed',
  SAVINGS_SORT_CHANGED: 'savings_sort_changed',

  ACCOUNT_REPLACED_FILTERED: 'account_replaced_filtered',

  // DataTable column interactions
  DATATABLE_COLUMN_RESIZED: 'datatable_column_resized',
  DATATABLE_COLUMN_REORDERED: 'datatable_column_reordered',
  DATATABLE_COLUMN_PINNED: 'datatable_column_pinned',
  DATATABLE_COLUMN_VISIBILITY_TOGGLED: 'datatable_column_visibility_toggled',

  // Transactions infinite scroll (#1128)
  TRANSACTIONS_PAGE_LOADED_MORE: 'transactions_page_loaded_more',

  // Transactions category filter (#1200)
  CATEGORY_FILTER_APPLIED: 'category_filter_applied',
  CATEGORY_FILTER_CLEARED: 'category_filter_cleared',

  // Custom category management (#1203)
  CUSTOM_CATEGORY_CREATED: 'custom_category_created',
  CUSTOM_CATEGORY_UPDATED: 'custom_category_updated',
  CUSTOM_CATEGORY_DELETED: 'custom_category_deleted',
  CUSTOM_CATEGORY_MANAGER_OPENED: 'custom_category_manager_opened',
  BUDGET_SUB_ALLOCATION_ADDED: 'budget_sub_allocation_added',
  REPORT_CUSTOM_CATEGORY_FILTERED: 'report_custom_category_filtered',
  TRANSACTION_REVIEWED: 'transaction_reviewed',

  // Issue #1215: MCP analytics helper endpoints — analytics views
  ANALYTICS_FINANCIAL_SUMMARY_VIEWED: 'analytics_financial_summary_viewed',
  ANALYTICS_SAVINGS_RATE_VIEWED: 'analytics_savings_rate_viewed',
  ANALYTICS_MOM_CHANGE_VIEWED: 'analytics_mom_change_viewed',
  ANALYTICS_RECURRING_SUBS_VIEWED: 'analytics_recurring_subs_viewed',
  ANALYTICS_UNUSUAL_SPEND_VIEWED: 'analytics_unusual_spend_viewed',
  ANALYTICS_BUDGET_VS_ACTUAL_VIEWED: 'analytics_budget_vs_actual_viewed',
  ANALYTICS_NET_WORTH_TREND_VIEWED: 'analytics_net_worth_trend_viewed',
  ANALYTICS_INCOME_SOURCES_VIEWED: 'analytics_income_sources_viewed',
  ANALYTICS_LARGEST_TRANSACTIONS_VIEWED: 'analytics_largest_transactions_viewed',
  ANALYTICS_AVERAGE_MONTHLY_SPEND_VIEWED: 'analytics_average_monthly_spend_viewed',
} as const

export type AnalyticsEventName = (typeof ANALYTICS_EVENTS)[keyof typeof ANALYTICS_EVENTS]
