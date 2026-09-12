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
  // Issue #1569, Task 6: ACCOUNT_REMOVED is fired by the disconnect (non-destructive) verb — kept
  // as-is for continuity with existing dashboards. ACCOUNT_PURGED is the new, genuinely
  // destructive verb and must be tracked separately.
  ACCOUNT_PURGED: 'account_purged',
  ACCOUNT_DETAIL_VIEWED: 'account_detail_viewed',
  ACCOUNT_INVESTMENT_ACTIVITY_CLICKED: 'account_investment_activity_clicked',
  // #1569: the retained holdings history of a CLOSED account. Fired when the user switches which
  // recorded day they are looking at — the only interaction the surface has.
  ACCOUNT_HOLDINGS_HISTORY_DATE_CHANGED: 'account_holdings_history_date_changed',
  // Issue #1569 T7.8: a user opened the day-by-day POSITIONS retained under a CLOSED account.
  // DATE_CHANGED above cannot answer "did anyone actually go and look at the money we kept?" —
  // the day picker only renders above ONE available day, so a recently-closed account with a
  // single retained day fired no event at all, and ACCOUNT_DETAIL_VIEWED cannot tell this tab
  // apart from any other. Same shape and same reasoning as ACCOUNT_BALANCE_HISTORY_VIEWED below.
  // ⚠ COUNTS AND A FIXED TYPE DISCRIMINATOR ONLY — no amount, no security name, no date. Event
  // properties are NOT covered by session-replay masking, and a security name is portfolio
  // composition. It is a rate, not a record.
  ACCOUNT_HOLDINGS_HISTORY_VIEWED: 'account_holdings_history_viewed',
  // Issue #1569: a user opened the day-by-day balances retained under a CLOSED account. A VIEW
  // event, not a change-only one, and that distinction is the whole point: the thesis of #1569 is
  // what the user can SEE, so the measurement that has to exist is the one answering "did anyone
  // actually go and look at the money we kept?". A date/range-changed event only ever counts the
  // people who already got there. Carries account_type — a fixed discriminator, never free text
  // and never an amount — so the rate can be read per kind of closed account.
  ACCOUNT_BALANCE_HISTORY_VIEWED: 'account_balance_history_viewed',
  // Issue #1609: the reconnect flow (/accounts/reconnect) for a broken bank connection. Mirrors
  // PLAID_LINK_STARTED/SUCCEEDED/FAILED's shape but is tracked separately — this is a repair of an
  // EXISTING linked item (update-mode Link, no new linked_items row), not a first-time link, and
  // that distinction matters for the funnel: a failure here means a user's connection STAYS broken,
  // whereas a failure in the normal flow just means they didn't add a bank yet.
  ACCOUNT_RECONNECT_LINK_STARTED: 'account_reconnect_link_started',
  ACCOUNT_RECONNECT_SUCCEEDED: 'account_reconnect_succeeded',
  ACCOUNT_RECONNECT_FAILED: 'account_reconnect_failed',

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

  // Boosts (formerly Rules)
  BOOST_CREATED: 'boost_created',
  BOOST_EDITED: 'boost_edited',
  BOOST_DELETED: 'boost_deleted',
  // Issue #1569 T7.3: a user opened the account-wide automation history — the only surface that
  // can reach an execution whose boost has been deleted or purged. A VIEW event, because the
  // thesis of #1569 is what the user can SEE: this is the measurement that answers "did anyone
  // actually go and look at the history we kept?". Carries `has_deleted_boost_rows`, a boolean
  // computed over the page on screen — a fixed discriminator, never a name, id or amount.
  BOOST_EXECUTION_HISTORY_VIEWED: 'boost_execution_history_viewed',

  // Auto-categorization
  AUTO_CATEGORIZATION_VIEWED: 'auto_categorization_viewed',
  // Issue #1569 T9.4: a user opened the account-wide RULE run history — the only surface that can
  // reach an execution whose categorization rule has since been hard-deleted (rule_id goes NULL,
  // rule_name and definition_snapshot are kept). The twin of BOOST_EXECUTION_HISTORY_VIEWED, and a
  // VIEW event for the same reason: #1569's thesis is what the user can SEE, so this is the
  // measurement that answers "did anyone actually go and look at the history we kept?". Carries
  // `has_deleted_rule_rows` and `deleted_row_count`, both computed over the page on screen —
  // counts and a boolean, never a rule name, a condition value, an amount or an id.
  RULE_EXECUTION_HISTORY_VIEWED: 'rule_execution_history_viewed',

  // #1625. The reporting session's failure mode was firing speculative rule writes at a live
  // financial account because there was no way to try a rule first. RULE_PREVIEWED measures
  // whether the dry run actually gets used before a save; RULE_SAVED carries the shape of the
  // rule so we can see which of the NEW condition fields (name, merchant_id, signed_amount,
  // direction) people reach for.
  //
  // ⚠ Properties are counts, booleans and the fixed field/operator vocabulary ONLY — never a
  // condition VALUE, a rule name, a merchant, an amount or an id. A rule's value is user-typed
  // financial detail and must not reach analytics.
  RULE_PREVIEWED: 'rule_previewed',
  RULE_SAVED: 'rule_saved',

  // Dashboard
  DASHBOARD_VIEWED: 'dashboard_viewed',
  WIDGET_ADDED: 'widget_added',
  WIDGET_REMOVED: 'widget_removed',
  TIMEFRAME_CHANGED: 'timeframe_changed',
  // Issue #1370: Household feature — Personal/Household dashboard scope toggle
  DASHBOARD_SCOPE_CHANGED: 'dashboard_scope_changed',
  // Issue #1370 Task 8: Household feature — per-account share/hide toggle
  HOUSEHOLD_ACCOUNT_SHARE_CHANGED: 'household_account_share_changed',

  // Issue #1370 Task 10: Household feature — remaining lifecycle/invite events.
  // No payloads carry PII (household name, member name, invitee email) —
  // only non-PII aggregate counts / method discriminators are passed, per
  // docs/security/posthog-pii-masking.md's "pass IDs/counts, not names" rule.
  HOUSEHOLD_CREATED: 'household_created',
  HOUSEHOLD_RENAMED: 'household_renamed',
  HOUSEHOLD_DISBANDED: 'household_disbanded',
  HOUSEHOLD_LEFT: 'household_left',
  HOUSEHOLD_MEMBER_REMOVED: 'household_member_removed',
  HOUSEHOLD_INVITE_CREATED: 'household_invite_created',
  HOUSEHOLD_INVITE_REVOKED: 'household_invite_revoked',
  // A single event for regenerate (revoke+create as one user action) rather
  // than firing HOUSEHOLD_INVITE_REVOKED + HOUSEHOLD_INVITE_CREATED back to
  // back — regenerate is one user-initiated action, not two.
  HOUSEHOLD_INVITE_REGENERATED: 'household_invite_regenerated',
  HOUSEHOLD_INVITE_ACCEPTED: 'household_invite_accepted',
  HOUSEHOLD_INVITE_DECLINED: 'household_invite_declined',
  // Fired for BOTH "Save & Continue" and "Skip" in ShareNewAccountsDialog —
  // both are batch-writes of visibility (Skip is not a no-op, see that
  // component's doc comment) — the `method` property distinguishes them
  // rather than using two separate event names.
  HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED: 'household_new_accounts_share_saved',

  // Issue #1371 Task 10: Household over-capacity owner banner
  HOUSEHOLD_OVER_CAPACITY_BANNER_VIEWED: 'household_over_capacity_banner_viewed',
  HOUSEHOLD_OVER_CAPACITY_BANNER_CTA_CLICKED: 'household_over_capacity_banner_cta_clicked',
  HOUSEHOLD_OVER_CAPACITY_BANNER_DISMISSED: 'household_over_capacity_banner_dismissed',

  // Billing
  CHECKOUT_STARTED: 'checkout_started',
  CHECKOUT_COMPLETED: 'checkout_completed',
  SUBSCRIPTION_MANAGED: 'subscription_managed',

  // Profile
  PRIVACY_SETTINGS_CHANGED: 'privacy_settings_changed',
  // Issue #1587: the user exercising their GDPR Article 20 right to data portability. Fired on the
  // DOWNLOAD, not on the click: an export the browser never handed the user is not a fulfilled
  // request, and counting it as one would make the funnel say the opposite of the truth.
  DATA_EXPORT_DOWNLOADED: 'data_export_downloaded',
  DATA_EXPORT_FAILED: 'data_export_failed',
  PROFILE_UPDATED: 'profile_updated',
  THEME_CHANGED: 'theme_changed',

  // Reports
  REPORTS_VIEWED: 'reports_viewed',
  REPORT_DATE_CHANGED: 'report_date_changed',
  REPORT_CATEGORY_CLICKED: 'report_category_clicked',
  REPORT_MERCHANT_CLICKED: 'report_merchant_clicked',
  REPORT_SANKEY_PRIVACY_TOGGLED: 'report_sankey_privacy_toggled',
  // #1626: carries the view MODE only. Never a category name, id or amount — event
  // properties are stored verbatim and are not covered by data-ph-mask.
  REPORT_SANKEY_VIEW_MODE_CHANGED: 'report_sankey_view_mode_changed',
  // #1626: a parent node was drilled into. Carries `depth` (from node.depth — see the warning on
  // its sibling below, which governs this event too) and nothing else.
  REPORT_SANKEY_CATEGORY_FOCUSED: 'report_sankey_category_focused',
  // #1626 Phase 2: a Sankey node was clicked through to the filtered transactions list. Same PII
  // rule as its siblings above — STRUCTURAL fields only (direction, depth), never a category
  // name, id or amount, because event properties are recorded verbatim and no masking applies.
  //
  // ⚠ Do NOT derive `depth` from a node id by splitting on ':', in EITHER of these two events.
  // Income nodes are flattened to `in:l1:*` regardless of their real depth, so that reading would
  // report every income navigation as level 1 — a plausible-looking number that is simply wrong.
  // `node.depth` carries the real value and both events now read it.
  REPORT_SANKEY_NODE_NAVIGATED: 'report_sankey_node_navigated',

  // Admin
  ADMIN_PAGE_VIEWED: 'admin_page_viewed',
  ADMIN_DLQ_RETRY: 'admin_dlq_retry',
  ADMIN_DLQ_DELETE: 'admin_dlq_delete',
  ADMIN_DLQ_BULK_DELETE: 'admin_dlq_bulk_delete',
  // Round 15, Tier E item 2: replaying a Paddle webhook is an operator-initiated retry of a
  // billing-state transition — the same class of action as ADMIN_DLQ_RETRY above, and it was
  // untracked while its DLQ twin was. Carries the event TYPE only: the Paddle event id is a
  // per-customer identifier.
  ADMIN_WEBHOOK_REPLAY: 'admin_webhook_replay',
  // Issue #1497 item 2: an operator ATTESTED (unverified) that they removed a stranded Plaid
  // Item by hand. Deliberately not named *_resolved: it is a claim, not a proved removal.
  ADMIN_ORPHANED_PLAID_ITEM_ATTESTED: 'admin_orphaned_plaid_item_attested',
  // Issue #1413: Admin subscription override (comp tier apply/remove)
  ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED: 'admin_subscription_override_applied',
  ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED: 'admin_subscription_override_removed',

  // Notifications (#952)
  NOTIFICATION_DRAWER_OPENED: 'notification_drawer_opened',
  NOTIFICATION_READ: 'notification_read',
  NOTIFICATION_ALL_READ: 'notification_all_read',
  NOTIFICATION_ACTION_CLICKED: 'notification_action_clicked',
  TRANSFER_SUGGESTION_ACCEPTED: 'transfer_suggestion_accepted',
  TRANSFER_SUGGESTION_REJECTED: 'transfer_suggestion_rejected',
  TRANSFER_SUGGESTION_CANCELED: 'transfer_suggestion_canceled',
  TRANSFER_SUGGESTION_MATCH_CONFIRMED: 'transfer_suggestion_match_confirmed',
  TRANSFER_SUGGESTION_MATCH_DECLINED: 'transfer_suggestion_match_declined',

  // Savings transfer automation (#972)
  SAVINGS_IMPACT_VIEWED: 'savings_impact_viewed',
  TRANSFER_SUGGESTION_VALIDATED: 'transfer_suggestion_validated',

  // Net Worth (#955)
  NET_WORTH_PAGE_VIEWED: 'net_worth_page_viewed',
  NET_WORTH_RANGE_CHANGED: 'net_worth_range_changed',

  // Savings Goals (#954, #1300)
  SAVINGS_PAGE_VIEWED: 'savings_page_viewed',
  SAVINGS_GOAL_CREATED: 'savings_goal_created',
  SAVINGS_GOAL_EDITED: 'savings_goal_edited',
  SAVINGS_GOAL_DELETED: 'savings_goal_deleted',
  SAVINGS_FILTER_CHANGED: 'savings_filter_changed',
  SAVINGS_SORT_CHANGED: 'savings_sort_changed',
  SAVINGS_GOAL_PROGRESS_BASIS_CHANGED: 'savings_goal_progress_basis_changed', // #1300 G1

  // Savings contributions (#1300 M21 / R3-7)
  SAVINGS_CONTRIBUTIONS_VIEWED: 'savings_contributions_viewed',
  SAVINGS_CONTRIBUTION_LOGGED: 'savings_contribution_logged',
  SAVINGS_CONTRIBUTION_CONFIRMED: 'savings_contribution_confirmed',
  SAVINGS_CONTRIBUTION_REJECTED: 'savings_contribution_rejected', // R3-7 sticky tombstone
  SAVINGS_CONTRIBUTION_DELETED: 'savings_contribution_deleted',

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

  // Issue #1210: Transaction review inbox
  REVIEW_INBOX_VIEWED: 'review_inbox_viewed',
  REVIEW_PRESET_CHANGED: 'review_preset_changed',
  TRANSACTION_APPROVED: 'transaction_approved',
  TRANSACTION_EDITOR_OPENED: 'transaction_editor_opened',
  TRANSACTION_REVIEW_FILTER_CHANGED: 'transaction_review_filter_changed',
  TRANSACTION_SKIPPED: 'transaction_skipped',
  REVIEW_BULK_RECATEGORIZED: 'review_bulk_recategorized',
  REVIEW_UNDO: 'review_undo',
  REVIEW_ALL_CAUGHT_UP: 'review_all_caught_up',

  // Boosts (#1267, #1300)
  BOOSTS_PAGE_VIEWED: 'boosts_page_viewed',
  BOOSTS_TAB_CHANGED: 'boosts_tab_changed',
  BOOSTS_FILTER_CHANGED: 'boosts_filter_changed',
  BOOSTS_CREATE_CLICKED: 'boosts_create_clicked', // #1300: always-visible header button

  // Planning tab (#1300)
  PLANNING_PAGE_VIEWED: 'planning_page_viewed',
  PLANNING_TAB_CHANGED: 'planning_tab_changed',

  // Insights tab (#1300)
  INSIGHTS_PAGE_VIEWED: 'insights_page_viewed',
  INSIGHTS_TAB_CHANGED: 'insights_tab_changed',

  // Portfolio tab (#1296)
  PORTFOLIO_PAGE_VIEWED: 'portfolio_page_viewed',
  PORTFOLIO_RANGE_CHANGED: 'portfolio_range_changed',
  PORTFOLIO_ALLOCATION_CONFIG_CHANGED: 'portfolio_allocation_config_changed',

  // Investment dashboard widgets (#1297)
  PORTFOLIO_SUMMARY_WIDGET_VIEWED: 'portfolio_summary_widget_viewed',
  PORTFOLIO_HOLDINGS_WIDGET_VIEWED: 'portfolio_holdings_widget_viewed',
  ASSET_ALLOCATION_WIDGET_VIEWED: 'asset_allocation_widget_viewed',

  // Unified investment feed (#1299)
  INVESTMENT_ACTIVITY_FILTER_TOGGLED: 'investment_activity_filter_toggled',
  // Issue #1569 T12: the user opted in/out of bank-reversed transactions. Worth measuring —
  // if nobody ever turns it on, the retention it exposes is invisible in practice.
  REVERSED_TRANSACTIONS_FILTER_TOGGLED: 'reversed_transactions_filter_toggled',
  // Issue #1626: the user removed the uncategorized pill. Only ever fired on a CLEAR — the pill
  // is the way out of a filter the reports Sankey turned on, and nothing in this panel turns it
  // on — so a high rate here means people are landing on that list and leaving immediately.
  UNCATEGORIZED_FILTER_CLEARED: 'uncategorized_filter_cleared',
  // Issue #1569 T12: a deleted savings goal was erased for good, destroying hand-logged
  // contributions. Deliberately carries no goal identity — it is a rate, not a record.
  SAVINGS_GOAL_PURGED: 'savings_goal_purged',
  // Issue #1569 T7: a user opened the contributions retained under a DELETED goal. The whole
  // thesis of #1569 is what the user can SEE, and this is the one measurement that answers
  // "did anyone actually go and look at the money we kept?". Like SAVINGS_GOAL_PURGED it carries
  // no goal identity — it is a rate, not a record.
  SAVINGS_GOAL_DELETED_LEDGER_VIEWED: 'savings_goal_deleted_ledger_viewed',
  INVESTMENT_ACTIVITY_TYPE_FILTER_APPLIED: 'investment_activity_type_filter_applied',
  INVESTMENT_DETAIL_DRAWER_OPENED: 'investment_detail_drawer_opened',

  // Issue #1411: public MCP marketing page + landing band
  // MCP_SETUP_TAB_CHANGED and MCP_CONFIG_COPIED fire with
  // { client: 'claude_desktop' | 'claude_code' | 'cursor' | 'vscode' } — a fixed
  // discriminator, never free text and never user data, per the `before_send`
  // property denylist rule in docs/security/posthog-pii-masking.md.
  MCP_PAGE_VIEWED: 'mcp_page_viewed',
  MCP_BAND_CTA_CLICKED: 'mcp_band_cta_clicked',
  MCP_PAGE_CTA_CLICKED: 'mcp_page_cta_clicked',
  MCP_SETUP_TAB_CHANGED: 'mcp_setup_tab_changed',
  MCP_CONFIG_COPIED: 'mcp_config_copied',

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

  // Subscription banner (#898)
  SUBSCRIPTION_BANNER_VIEWED: 'subscription_banner_viewed',
  SUBSCRIPTION_BANNER_CTA_CLICKED: 'subscription_banner_cta_clicked',
  SUBSCRIPTION_BANNER_DISMISSED: 'subscription_banner_dismissed',

  // Merchants (#957)
  MERCHANTS_TAB_VIEWED: 'merchants_tab_viewed',
  MERCHANT_OVERRIDE_SET: 'merchant_override_set',
  MERCHANT_OVERRIDE_CLEARED: 'merchant_override_cleared',
  MERCHANTS_MERGED: 'merchants_merged',
} as const

export type AnalyticsEventName = (typeof ANALYTICS_EVENTS)[keyof typeof ANALYTICS_EVENTS]
