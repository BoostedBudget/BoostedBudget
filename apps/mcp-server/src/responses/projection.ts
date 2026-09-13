/** Pure Worker-safe MCP presentation policy. The REST payload is never mutated. */
export type ResponseDetail = 'compact' | 'full'

export interface ProjectionTarget {
  profile: string
  array?: boolean
  required?: boolean
}

export interface ResponseProfile {
  schema: string
  keep: readonly string[]
  omit: readonly string[]
  children: Readonly<Record<string, ProjectionTarget>>
  derived?: 'activity' | 'transaction'
}

export type ResponsePolicy =
  | { kind: 'project'; target: ProjectionTarget; recoveryOperationId: string }
  | { kind: 'passthrough'; reason: string }

const fields = (value: string): string[] => value.split(' ').filter(Boolean)
const profile = (schema: string, keep: string, omit: string, children: Record<string, ProjectionTarget> = {}, derived?: ResponseProfile['derived']): ResponseProfile => ({ schema, keep: fields(keep), omit: fields(omit), children, ...(derived ? { derived } : {}) })
const target = (profile: string, array = false, required = false): ProjectionTarget => ({ profile, array, required })

const accountKeep = 'available_balance currency_code current_balance exclude_investment_sync expired_at id institution_name is_hidden last_known_balance last_known_balance_at linked_item_status mask name nickname official_name removed_at replaced_at replaced_by_id status subtype type user_account_id'
const accountOmit = 'created_at custom_color custom_emoji custom_icon display_order linked_item_id plaid_account_id updated_at user_id'
const txKeep = 'account account_id amount authorized_date category_override_kind custom_category_override_display custom_category_override_id date effective_category_name effective_merchant_name effective_pfc_detailed effective_pfc_primary id merchant_id merchant_name name payment_channel pending reviewed reviewed_at source tags user_account_id user_notes'
const txOmit = 'external_transaction_id linked_item_id pfc_category_icon_url pfc_confidence_level pfc_detailed pfc_detailed_display pfc_detailed_override pfc_detailed_override_display pfc_primary pfc_primary_display pfc_primary_override pfc_primary_override_display user_id'

export const responseProfiles: Readonly<Record<string, ResponseProfile>> = {
  Account: profile('Account', accountKeep, accountOmit),
  AccountIdentity: profile('Account', 'id user_account_id name nickname official_name institution_name mask type subtype currency_code status', 'available_balance current_balance exclude_investment_sync expired_at is_hidden last_known_balance last_known_balance_at linked_item_status removed_at replaced_at replaced_by_id ' + accountOmit),
  UserAccount: profile('UserAccount', 'account_name_hint exclude_investment_sync id institution_id is_hidden mask nickname primary_subtype primary_type', '$schema created_at custom_color custom_emoji custom_icon display_order updated_at user_id'),
  ActivityItem: profile('ActivityItem', txKeep + ' activity_type currency_code data_source investment_fees investment_price investment_quantity investment_subtype reversed reversed_at security_id security_name sort_date ticker_symbol', txOmit, { account: target('AccountIdentity') }, 'activity'),
  Transaction: profile('Transaction', txKeep + ' budget_expense_amount category_source', txOmit + ' $schema category_override_date created_at deleted_at deleted_reason external_metadata tags_source updated_at', { account: target('AccountIdentity') }, 'transaction'),
  CategorizationRule: profile('CategorizationRule', 'condition_logic conditions description enabled id last_matched_at match_count name override_count priority quarantine_reason quarantined source tag_behavior target_custom_category_display target_custom_category_id target_kind target_pfc_detailed target_pfc_detailed_display target_pfc_primary target_pfc_primary_display target_tags version', '$schema created_at updated_at user_id'),
  Boost: profile('Boost', 'actions boost_type condition_logic conditions description enabled execution_count id last_executed_at last_successful_at name priority trigger_config', '$schema created_at updated_at user_id'),
  BoostExecution: profile('BoostExecution', 'boost_id boost_name completed_at error_code error_message execution_duration_ms id max_retries output_data retry_count started_at status', '$schema definition_snapshot input_data trigger_context user_id'),
  RuleExecution: profile('RuleExecution', 'action_type executed_at id result rule_id rule_name transaction_id', 'definition_snapshot user_id'),
  Budget: profile('Budget', 'amount categories currency_code description end_date expected_income id income_amount income_sources is_active name period series_id start_date', '$schema created_at updated_at user_id', { categories: target('BudgetCategory', true) }),
  BudgetCategory: profile('BudgetCategory', 'allocated_amount auto_created budget_id category_kind custom_category_id custom_grandparent_pfc_primary custom_parent_id custom_parent_pfc_primary group_name id managed_by_goal_id match_logic members pfc_detailed pfc_detailed_display pfc_primary pfc_primary_display savings_goal_id savings_goal_name spent_amount tags', '$schema created_at updated_at'),
  CustomCategory: profile('CustomCategory', 'id name parent_id parent_name parent_pfc_primary parent_pfc_primary_display usage_count', '$schema color created_at icon updated_at user_id'),
  SavingsGoal: profile('SavingsGoal', 'account account_id account_percentage balance_as_of completed_at currency_code current_amount deadline deleted_at description id is_completed last_calculated_at name progress_basis target_amount user_account_id', '$schema created_at updated_at user_id', { account: target('AccountIdentity') }),
  SavingsGoalContribution: profile('SavingsGoalContribution', 'amount archived_at confirmed_by_user id managed_by_reconciliation occurred_on savings_goal_id source status transaction_id', '$schema created_at updated_at'),
  Notification: profile('Notification', 'action_label action_url created_at id interaction_pattern message read title transfer_suggestion_id type', 'email_sent_at sent_email updated_at user_id'),
  Household: profile('Household', 'id name over_capacity_since owner_user_id', '$schema created_at over_capacity_start_notice_sent_at over_capacity_warn3d_sent_at updated_at'),
  HouseholdMemberView: profile('HouseholdMemberView', 'display_name joined_at role status user_id', 'avatar_url'),
  HouseholdInvite: profile('HouseholdInvite', 'accepted_at accepted_user_id created_at expires_at household_id id invitee_email inviter_user_id status', 'updated_at'),
  HoldingView: profile('HoldingView', 'account_id cost_basis currency_code day_change day_change_as_of day_change_is_stale day_change_pct day_change_unavailable_reason gain_loss gain_loss_pct id institution_price institution_value quantity security_id security_name security_type ticker_symbol user_account_id', 'user_id'),
  InvestmentTransaction: profile('InvestmentTransaction', 'account_id activity_type amount currency_code date fees id name price quantity security_id security_name subtype ticker_symbol type user_account_id', 'created_at plaid_investment_transaction_id plaid_security_id user_id'),
  MCPRecipe: profile('MCPRecipe', 'description enabled id name schedule spec', '$schema created_at updated_at user_id'),
  MerchantSummary: profile('MerchantSummary', 'dominant_category effective_display_name has_override is_merged merchant_id merged_into_merchant_id normalized_name total_spend transaction_count', 'raw_variants'),
  MerchantNameOverride: profile('MerchantNameOverride', 'display_name id merchant_id merged_into_merchant_id', '$schema created_at updated_at user_id'),
  UserTag: profile('UserTag', 'id name usage_count', '$schema color created_at updated_at user_id'),
  TransferSuggestion: profile('TransferSuggestion', 'actual_amount amount boost_execution_id boost_id boost_name completed_at completion_source created_at currency_code expires_at from_account_id from_account_name from_user_account_id id match_state matched_transaction_at matched_transaction_id reason source status to_account_id to_account_name to_user_account_id user_action_at user_notes', 'updated_at user_id'),
  PrivacySettings: profile('PrivacySettings', 'data_sharing_enabled gdpr_advertising_consent gdpr_analytics_consent gdpr_consent_timestamp gdpr_necessary_consent id include_in_aggregate_stats marketing_emails_enabled product_updates_enabled security_alerts_enabled', '$schema created_at updated_at user_id'),
  User: profile('User', 'comp_ends_at comp_tier deletion_requested_at deletion_scheduled_at effective_status effective_tier email email_verified had_trial id is_active is_comped is_deleted last_login_at manual_tier_ends_at manual_tier_override past_due_started_at plaid_cleanup_pending_at primary_region retention_expires_at retention_started_at subscription_ends_at subscription_started_at subscription_status subscription_tier timezone timezone_source trial_days_remaining trial_ends_at trial_started_at', '$schema clerk_user_id created_at paddle_customer_id paddle_subscription_id retention_notice_30d_sent_at retention_notice_7d_sent_at updated_at'),
  TransactionAnnotationChange: profile('TransactionAnnotationChange', 'change_source changed_at id new prior transaction_id', 'user_id'),
  GetAccountsOutputBody: profile('GetAccountsOutputBody', 'accounts total_count', '$schema', { accounts: target('Account', true, true) }),
  UpdateAccountOutputBody: profile('UpdateAccountOutputBody', 'account', '$schema', { account: target('Account', false, true) }),
  GetTransactionsOutputBody: profile('GetTransactionsOutputBody', 'limit next_cursor total_amount total_count transactions', '$schema', { transactions: target('ActivityItem', true, true) }),
  CategoryTransactionsResponse: profile('CategoryTransactionsResponse', 'has_more total_amount total_count transactions', '$schema', { transactions: target('Transaction', true, true) }),
  BoostExecutionsResponse: profile('BoostExecutionsResponse', 'executions limit offset total', '$schema', { executions: target('BoostExecution', true, true) }),
  RuleExecutionsResponse: profile('RuleExecutionsResponse', 'executions limit offset total', '$schema', { executions: target('RuleExecution', true, true) }),
  DeletedGoalContributionsBody: profile('DeletedGoalContributionsBody', 'contributions total_confirmed', '$schema', { contributions: target('SavingsGoalContribution', true, true) }),
  GetMyHouseholdResponseBody: profile('GetMyHouseholdResponseBody', 'household summary', '$schema', { household: target('Household') }),
  GetHoldingsOutputBody: profile('GetHoldingsOutputBody', 'count holdings', '$schema', { holdings: target('HoldingView', true, true) }),
  GetInvestmentTransactionsOutputBody: profile('GetInvestmentTransactionsOutputBody', 'limit next_cursor transactions', '$schema', { transactions: target('InvestmentTransaction', true, true) }),
  TransferSuggestionListResponse: profile('TransferSuggestionListResponse', 'items limit offset total', '$schema', { items: target('TransferSuggestion', true, true) }),
  TransactionAnnotationHistoryResponse: profile('TransactionAnnotationHistoryResponse', 'changes limit offset total', '$schema', { changes: target('TransactionAnnotationChange', true, true) }),
  ...Object.fromEntries(['Accept', 'Cancel', 'Confirm', 'Decline', 'Reject'].map((verb) => {
    const name = `${verb}TransferSuggestion${verb === 'Confirm' || verb === 'Decline' ? 'Match' : ''}OutputBody`
    return [name, profile(name, 'message suggestion', '$schema', { suggestion: target('TransferSuggestion') })]
  })),
}

const projections: Record<string, ResponsePolicy> = {}
function register(operations: string, profileName: string, array = false, recoveryOperationId?: string): void {
  for (const operationId of fields(operations)) {
    projections[operationId] = { kind: 'project', target: target(profileName, array), recoveryOperationId: recoveryOperationId ?? operationId }
  }
}
register('getAccounts', 'GetAccountsOutputBody')
register('updateAccount', 'UpdateAccountOutputBody', false, 'getAccounts')
register('getUserAccounts', 'UserAccount', true)
register('getUserAccountById', 'UserAccount')
register('updateUserAccount', 'UserAccount', false, 'getUserAccountById')
register('getTransactions', 'GetTransactionsOutputBody')
register('getCategoryTransactions', 'CategoryTransactionsResponse')
register('getTransactionByID', 'Transaction')
register('updateTransactionCategory updateTransactionTags', 'Transaction', false, 'getTransactionByID')
register('getUserTransactionAnnotationHistory', 'TransactionAnnotationHistoryResponse')
register('getUserRules', 'CategorizationRule', true)
register('getRuleByID', 'CategorizationRule')
register('createRule updateRule toggleRule', 'CategorizationRule', false, 'getRuleByID')
register('getBoosts', 'Boost', true)
register('getBoostByID', 'Boost')
// The parent-scoped endpoint requires a live boost; user history also retains deleted parents.
register('getBoostExecutions getUserBoostExecutions', 'BoostExecutionsResponse', false, 'getUserBoostExecutions')
register('getUserRuleExecutions', 'RuleExecutionsResponse')
register('getBudgets', 'Budget', true)
register('getCurrentBudget getBudget', 'Budget')
register('createBudget updateBudget syncBudget', 'Budget', false, 'getBudget')
register('addBudgetCategory updateBudgetCategory', 'BudgetCategory', false, 'getBudget')
register('getUserCategories', 'CustomCategory', true)
register('getCategoryByID', 'CustomCategory')
register('createCategory updateCategory', 'CustomCategory', false, 'getCategoryByID')
register('getSavingsGoals getActiveSavingsGoals getDeletedSavingsGoals', 'SavingsGoal', true)
register('getSavingsGoal', 'SavingsGoal')
register('createSavingsGoal updateSavingsGoal', 'SavingsGoal', false, 'getSavingsGoal')
register('listSavingsGoalContributions', 'SavingsGoalContribution', true)
register('listDeletedSavingsGoalContributions', 'DeletedGoalContributionsBody')
register('logSavingsGoalContribution confirmSavingsGoalContribution rejectSavingsGoalContribution', 'SavingsGoalContribution', false, 'listSavingsGoalContributions')
register('getNotifications', 'Notification', true)
register('getMyHousehold', 'GetMyHouseholdResponseBody')
register('listHouseholdMembers', 'HouseholdMemberView', true)
register('listHouseholdInvites', 'HouseholdInvite', true)
register('getHoldings', 'GetHoldingsOutputBody')
register('getInvestmentTransactions', 'GetInvestmentTransactionsOutputBody')
register('listMCPRecipes', 'MCPRecipe', true)
register('getMCPRecipe', 'MCPRecipe')
register('createMCPRecipe updateMCPRecipe', 'MCPRecipe', false, 'getMCPRecipe')
register('getMerchants', 'MerchantSummary', true)
register('getMerchantOverrides', 'MerchantNameOverride', true)
register('mergeMerchants bulkSetMerchantOverrides', 'MerchantNameOverride', true, 'getMerchantOverrides')
register('setMerchantOverride', 'MerchantNameOverride', false, 'getMerchantOverrides')
register('getUserTags', 'UserTag', true)
register('getTagByID', 'UserTag')
register('createTag updateTag', 'UserTag', false, 'getTagByID')
register('getTransferSuggestions', 'TransferSuggestionListResponse')
for (const verb of ['Accept', 'Cancel', 'Confirm', 'Decline', 'Reject']) {
  const suffix = `TransferSuggestion${verb === 'Confirm' || verb === 'Decline' ? 'Match' : ''}`
  register(verb.toLowerCase() + suffix, verb + suffix + 'OutputBody', false, 'getTransferSuggestions')
}
register('getPrivacySettings', 'PrivacySettings')
register('getUserProfile', 'User')

const passthroughGroups: Record<string, string> = {
  'Focused financial analytics, history, summaries, or previews; all fields are meaningful.': 'getAccountBalanceHistory getAverageMonthlySpend getBudgetVsActual getCashFlowHistory getCashFlowSummary getCategoryHierarchy getCategoryTotalForPeriod getFinancialSummary getIncomeSourceBreakdown getLargestTransactionsForPeriod getCategoryBreakdownForMerchant getMonthOverMonthSpendingChange getNetWorthTrend getRecurringSubscriptions getSavingsRateForPeriod getSpendingByCategory getSpendingByMerchant getSpendingTrends getTopExpense getUnusualSpend previewRule previewCategoryMatches getCategoryUsage getBalanceHistory getBalanceSummary getNetWorth getNetWorthHistory getAccountHoldingsHistory getPortfolioAllocationBreakdown getInvestmentGainLossSummary getPortfolioHistory getInvestmentIncomeSummary getPortfolioSummary getTopMovers getSavingsProgressSummary getSavingsImpactAggregate getSavingsImpactMe getTransactionReviewSummary',
  'Reference, configuration, invitation, grant, or entitlement fields define the complete contract.': 'getConditionSchema getAvailableCategories getPlaidCategories getPlaidDetailedCategories getPlaidPrimaryCategories getAccountTypes getDefaultLayout getDashboardPreferences getWidgetTypes getHouseholdInvitePreview listMyAccountShares getHouseholdEntitlement getRegions get-billing-portal-url get-current-subscription getSubscriptionTiers',
  'Focused mutation acknowledgement, count, or empty response.': 'reorderAccounts deleteRule deleteBudget deleteBudgetCategory deleteCategory deleteMCPRecipe clearMerchantOverride markAllNotificationsAsRead getUnreadNotificationCount markNotificationAsRead deleteSavingsGoal deleteTag bulkUpdateTransactionCategory bulkSetTransactionReviewed setTransactionReviewed',
  'Raw merchant variants are necessary evidence when comparing duplicate candidates.': 'getMerchantDuplicateSuggestions',
  'Recipe resolution must preserve its entire executable call list.': 'runMCPRecipe',
}
export const responseCatalog: Readonly<Record<string, ResponsePolicy>> = {
  ...projections,
  ...Object.fromEntries(Object.entries(passthroughGroups).flatMap(([reason, operations]) => fields(operations).map((id) => [id, { kind: 'passthrough' as const, reason }]))),
}

export interface ProjectionResult { value: unknown; reduced: boolean; warning?: string }
const has = (value: object, key: string): boolean => Object.prototype.hasOwnProperty.call(value, key)

function projectTarget(value: unknown, location: ProjectionTarget, state: { reduced: boolean }): unknown {
  if (value === null || value === undefined) return value
  if (location.array) {
    if (!Array.isArray(value)) throw new Error('Invalid collection')
    return value.map((item) => projectTarget(item, { profile: location.profile }, state))
  }
  if (typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid object')
  const policy = responseProfiles[location.profile]
  if (!policy) throw new Error('Missing profile')
  const input = value as Record<string, unknown>
  const known = new Set([...policy.keep, ...policy.omit])
  if (Object.keys(input).some((key) => !known.has(key))) throw new Error('Unclassified response field')
  for (const [key, child] of Object.entries(policy.children)) {
    if (child.required && !has(input, key)) throw new Error('Missing collection')
  }
  const output: Record<string, unknown> = {}
  for (const key of policy.keep) {
    if (has(input, key)) output[key] = policy.children[key] ? projectTarget(input[key], policy.children[key], state) : input[key]
  }
  for (const key of policy.omit) if (has(input, key)) state.reduced = true
  if (policy.derived) {
    if (input.category_override_kind !== 'custom') {
      for (const level of ['primary', 'detailed']) {
        const source = `pfc_${level}${input.category_override_kind === 'pfc' ? '_override' : ''}_display`
        if (has(input, source)) output[`effective_pfc_${level}_display`] = input[source]
      }
    }
    if (policy.derived === 'transaction') {
      output.reversed = input.deleted_at != null && input.deleted_reason === 'provider_removed_posted'
      if (output.reversed) output.reversed_at = input.deleted_at
    }
  }
  return output
}

export function projectResponse(operationId: string, value: unknown, detail: ResponseDetail = 'compact'): ProjectionResult {
  if (detail === 'full') return { value, reduced: false }
  const policy = responseCatalog[operationId]
  if (!policy) return { value, reduced: false, warning: 'Compact response unavailable; returned full response.' }
  if (policy.kind === 'passthrough') return { value, reduced: false }
  const state = { reduced: false }
  try {
    return { value: projectTarget(value, policy.target, state), reduced: state.reduced }
  } catch {
    return { value, reduced: false, warning: 'Compact response unavailable; returned full response.' }
  }
}
