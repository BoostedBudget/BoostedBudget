/**
 * Capability Manifest — the MCP authorization model.
 *
 * DENY-BY-DEFAULT: getCapability() returns undefined for any operationId
 * not explicitly listed here. Undefined → the host-proxy denies the call.
 *
 * Every operationId from api/openapi.yaml MUST appear in this file.
 * A CI check (scripts/check-manifest-completeness.ts) fails the build
 * if any OpenAPI operationId is missing from this manifest.
 */

export type OperationClass = 'read' | 'write'

export interface CapabilityEntry {
  operationId: string
  /** 'read' for safe, non-mutating ops; 'write' for state-changing ops */
  class: OperationClass
  /** Whether this write destroys or permanently removes data */
  destructive: boolean
  /** True if confirmation token required before execution */
  confirmationRequired: boolean
  /** True if safe to retry without additional side effects */
  idempotent: boolean
  /** True = never reachable through MCP */
  excluded: boolean
  /** Human-readable reason for exclusion */
  excludedReason?: string
}

// Shorthand helpers for compact manifest entries
const read = (operationId: string, idempotent = true): CapabilityEntry => ({
  operationId,
  class: 'read',
  destructive: false,
  confirmationRequired: false,
  idempotent,
  excluded: false,
})

const write = (
  operationId: string,
  opts: { destructive?: boolean; idempotent?: boolean } = {},
): CapabilityEntry => ({
  operationId,
  class: 'write',
  destructive: opts.destructive ?? false,
  confirmationRequired: true,
  idempotent: opts.idempotent ?? false,
  excluded: false,
})

const excluded = (operationId: string, reason: string): CapabilityEntry => ({
  operationId,
  class: 'read',
  destructive: false,
  confirmationRequired: false,
  idempotent: true,
  excluded: true,
  excludedReason: reason,
})

// ── Exclusion reason constants ──────────────────────────────────────────────

const REASON_ADMIN = 'Admin endpoints are excluded from MCP'
const REASON_PLAID_LINKING = 'Plaid account-linking is permanently non-MCP (app-only, owner directive)'
const REASON_RULES_UNATTENDED = 'Requires standing-auth model for unattended execution (follow-up issue)'
const REASON_DASHBOARD_DEFERRED = 'Dashboard layout writes deferred to follow-up issue'
const REASON_SUBSCRIPTION_INAPP = 'Subscription management stays in-app'
const REASON_DELETION_INAPP = 'Account deletion must be a deliberate in-app action'
const REASON_PRIVACY_INAPP = 'Privacy/consent settings require deliberate in-app action (deferred)'
const REASON_INTERNAL = 'Internal infrastructure endpoint'
const REASON_WEBHOOK = 'Internal webhook endpoint'
const REASON_QA = 'QA/testing endpoint, not for production MCP use'

// ── Manifest ───────────────────────────────────────────────────────────────

const MANIFEST_ENTRIES: CapabilityEntry[] = [
  // ── MCP Audit (service-auth via X-Origin-Secret, not end-user Clerk JWT) ──
  // The audit endpoint is excluded from MCP tool calls; only the Worker service calls it directly.
  excluded('recordMCPAuditEntry', 'Internal service endpoint; called by Worker service auth, not end-user MCP tools'),

  // ── MCP Recipes (read/write allowed; recipes exposed as MCP tools) ────────
  read('listMCPRecipes'),
  read('getMCPRecipe'),
  // runMCPRecipe is READ in v1: recipes can only compose read-class ops, so running
  // them produces no writes. The tool resolves the recipe to ordered API calls and
  // returns them for the agent to pass to execute(). Classifying as write would
  // require a confirmation token that the recipe.run tool has no input field for.
  read('runMCPRecipe'),
  write('createMCPRecipe', { idempotent: false }),
  write('updateMCPRecipe', { idempotent: true }),
  write('deleteMCPRecipe', { destructive: true }),

  // ── Analytics (all read) ────────────────────────────────────────────────
  read('getCashFlowHistory'),
  read('getCashFlowSummary'),
  read('getSpendingByCategory'),
  read('getSpendingByMerchant'),
  read('getSpendingTrends'),
  read('getCategoryTotalForPeriod'),  // MCP analytics helper — Issue #1087
  read('getTopExpense'),               // MCP analytics helper — Issue #1087
  read('getAverageMonthlySpend'),      // MCP analytics helper — Issue #1215

  // ── Accounts ────────────────────────────────────────────────────────────
  read('getAccounts'),
  write('reorderAccounts', { idempotent: true }),
  write('updateAccount', { idempotent: true }),
  excluded('deleteAccount', REASON_PLAID_LINKING),
  excluded('disconnectInstitution', REASON_PLAID_LINKING),

  // ── Admin (all excluded) ─────────────────────────────────────────────────
  excluded('checkRole', REASON_ADMIN),
  excluded('listAdminDeadLetterJobs', REASON_ADMIN),
  excluded('getAdminDeadLetterJobStats', REASON_ADMIN),
  excluded('getAdminDeadLetterJobDetail', REASON_ADMIN),
  excluded('getAdminDeadLetterJobArgs', REASON_ADMIN),
  excluded('getAdminPaymentFailures', REASON_ADMIN),
  excluded('getSystemHealth', REASON_ADMIN),
  excluded('listAdminUsers', REASON_ADMIN),
  excluded('getAdminUserDetail', REASON_ADMIN),
  excluded('listAdminPaddleWebhooks', REASON_ADMIN),
  excluded('getAdminPaddleWebhookStats', REASON_ADMIN),
  excluded('getAdminPaddleWebhookDetail', REASON_ADMIN),
  excluded('bulkDeleteAdminDeadLetterJobs', REASON_ADMIN),
  excluded('addAdminDeadLetterJobNotes', REASON_ADMIN),
  excluded('retryAdminDeadLetterJob', REASON_ADMIN),
  excluded('fireWebhook', REASON_ADMIN),
  excluded('grantTrial', REASON_ADMIN),
  excluded('replayAdminPaddleWebhook', REASON_ADMIN),
  excluded('deleteAdminDeadLetterJob', REASON_ADMIN),
  excluded('get-admin-user-subscription', REASON_ADMIN),
  excluded('create-admin-subscription-override', REASON_ADMIN),
  excluded('update-admin-user-tier', REASON_ADMIN),

  // ── Auto-Categorization ─────────────────────────────────────────────────
  read('getUserRules'),
  read('getRuleByID'),
  excluded('createRule', REASON_RULES_UNATTENDED),
  excluded('updateRule', REASON_RULES_UNATTENDED),
  excluded('deleteRule', REASON_RULES_UNATTENDED),
  excluded('toggleRule', REASON_RULES_UNATTENDED),

  // ── Automation Rules ────────────────────────────────────────────────────
  read('getAutomationRules'),
  read('getAutomationRuleByID'),
  read('getRuleExecutions'),
  excluded('createAutomationRule', REASON_RULES_UNATTENDED),
  excluded('updateAutomationRule', REASON_RULES_UNATTENDED),
  excluded('deleteAutomationRule', REASON_RULES_UNATTENDED),
  excluded('disableAutomationRule', REASON_RULES_UNATTENDED),
  excluded('enableAutomationRule', REASON_RULES_UNATTENDED),
  excluded('toggleAutomationRule', REASON_RULES_UNATTENDED),
  excluded('triggerAutomationRule', REASON_RULES_UNATTENDED),
  excluded('testAutomationRule', REASON_RULES_UNATTENDED),

  // ── Budgets ─────────────────────────────────────────────────────────────
  read('getBudgets'),
  read('getAvailableCategories'),
  read('getCurrentBudget'),
  read('getBudget'),
  read('getCategoryTransactions'),
  write('createBudget'),
  write('updateBudget', { idempotent: true }),
  write('deleteBudget', { destructive: true }),
  write('addBudgetCategory'),
  write('updateBudgetCategory', { idempotent: true }),
  write('deleteBudgetCategory', { destructive: true }),
  write('syncBudget'),
  read('previewCategoryMatches'), // POST but non-mutating

  // ── Categories ──────────────────────────────────────────────────────────
  read('getUserCategories'),
  read('getPlaidCategories'),
  read('getPlaidDetailedCategories'),
  read('getPlaidPrimaryCategories'),
  read('getCategoryByID'),
  read('getCategoryUsage'),
  write('createCategory'),
  write('updateCategory', { idempotent: true }),
  write('deleteCategory', { destructive: true }),

  // ── Dashboard (reads + layout writes excluded) ──────────────────────────
  read('getAccountTypes'),
  read('getBalanceHistory'),
  read('getBalanceSummary'),
  read('getNetWorth'),
  read('getNetWorthHistory'),
  read('getDashboardPreferences'),
  read('getDefaultLayout'),
  read('getWidgetTypes'),
  excluded('saveDashboardPreferences', REASON_DASHBOARD_DEFERRED),
  excluded('resetDashboardPreferences', REASON_DASHBOARD_DEFERRED),

  // ── Health / Monitoring ─────────────────────────────────────────────────
  excluded('getHealth', REASON_INTERNAL),
  excluded('getMetrics', REASON_INTERNAL),

  // ── Notifications ────────────────────────────────────────────────────────
  read('getNotifications'),
  read('getUnreadNotificationCount'),
  write('markNotificationAsRead', { idempotent: true }),
  write('markAllNotificationsAsRead', { idempotent: true }),

  // ── Plaid (account-linking permanently non-MCP) ──────────────────────────
  excluded('exchangePublicToken', REASON_PLAID_LINKING),
  excluded('createLinkToken', REASON_PLAID_LINKING),

  // ── Privacy / Profile ────────────────────────────────────────────────────
  read('getPrivacySettings'),
  read('getUserProfile'),
  excluded('updatePrivacySettings', REASON_PRIVACY_INAPP),
  excluded('updateUserProfile', 'Profile mutations deferred to follow-up'),

  // ── Regions ─────────────────────────────────────────────────────────────
  read('getRegions'),

  // ── Savings Goals ────────────────────────────────────────────────────────
  read('getSavingsGoals'),
  read('getActiveSavingsGoals'),
  read('getSavingsGoal'),
  write('createSavingsGoal'),
  write('updateSavingsGoal', { idempotent: true }),
  write('deleteSavingsGoal', { destructive: true }),

  // ── Subscription ─────────────────────────────────────────────────────────
  read('get-current-subscription'),
  read('getSubscriptionTiers'),
  read('get-billing-portal-url'),
  excluded('cancel-paddle-subscription', REASON_SUBSCRIPTION_INAPP),
  excluded('create-paddle-checkout', REASON_SUBSCRIPTION_INAPP),
  excluded('preview-paddle-subscription-update', REASON_SUBSCRIPTION_INAPP),
  excluded('update-paddle-subscription', REASON_SUBSCRIPTION_INAPP),

  // ── Tags ────────────────────────────────────────────────────────────────
  read('getUserTags'),
  read('getTagByID'),
  write('createTag'),
  write('updateTag', { idempotent: true }),
  write('deleteTag', { destructive: true }),

  // ── Transactions ────────────────────────────────────────────────────────
  read('getTransactions'),
  read('getTransactionByID'),
  write('updateTransactionCategory', { idempotent: true }),
  write('updateTransactionTags', { idempotent: true }),
  write('setTransactionReviewed', { idempotent: true }),
  write('bulkSetTransactionReviewed', { idempotent: true }),

  // ── Transfer Suggestions ────────────────────────────────────────────────
  read('getTransferSuggestions'),
  write('acceptTransferSuggestion', { idempotent: true }),
  write('rejectTransferSuggestion', { idempotent: true }),

  // ── User ────────────────────────────────────────────────────────────────
  excluded('requestDeletion', REASON_DELETION_INAPP),
  excluded('cancelDeletion', REASON_DELETION_INAPP),

  // ── User Accounts ────────────────────────────────────────────────────────
  read('getUserAccounts'),
  read('getUserAccountById'),
  write('updateUserAccount', { idempotent: true }),

  // ── Webhooks + QA ────────────────────────────────────────────────────────
  excluded('paddle-webhook', REASON_WEBHOOK),
  excluded('handlePlaidWebhook', REASON_WEBHOOK),
  excluded('qaAuthBypass', REASON_QA),
]

// ── Lookup map ─────────────────────────────────────────────────────────────

const MANIFEST_MAP = new Map<string, CapabilityEntry>(
  MANIFEST_ENTRIES.map((entry) => [entry.operationId, entry]),
)

/**
 * All known operationIds (used for completeness checks).
 * The CI manifest-completeness script cross-references this against the
 * live OpenAPI spec — any unlisted operationId fails the build.
 */
export const ALL_OPERATION_IDS: string[] = MANIFEST_ENTRIES.map((e) => e.operationId)

// ── Public API ─────────────────────────────────────────────────────────────

/**
 * Look up the capability entry for an operationId.
 * Returns `undefined` for any unknown operationId (deny-by-default).
 */
export function getCapability(operationId: string): CapabilityEntry | undefined {
  return MANIFEST_MAP.get(operationId)
}

/**
 * Returns true if the operation is allowed through MCP (known + not excluded).
 */
export function isAllowed(operationId: string): boolean {
  const entry = getCapability(operationId)
  return entry !== undefined && !entry.excluded
}

/**
 * Returns true if the operation is explicitly excluded.
 * Returns false for unknown operations (treat them as denied, not excluded).
 */
export function isExcluded(operationId: string): boolean {
  return getCapability(operationId)?.excluded === true
}

/**
 * Returns true if the operation is a write class and not excluded.
 */
export function isWrite(operationId: string): boolean {
  const entry = getCapability(operationId)
  return entry !== undefined && !entry.excluded && entry.class === 'write'
}

/**
 * Returns true if the operation requires a confirmation token before execution.
 */
export function requiresConfirmation(operationId: string): boolean {
  return getCapability(operationId)?.confirmationRequired === true
}
