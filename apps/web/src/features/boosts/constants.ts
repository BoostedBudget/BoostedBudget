// Issue #1267: Boosts page constants
export const BOOSTS_TAB_STORAGE_KEY = 'boosts-active-tab'
export const HISTORY_PAGE_SIZE = 20

// Rule types that generate transfer suggestions — scopes AC5 summary count and Rules-tab default.
// Mirrors domain.RuleTypeTransferSuggestion + domain.RuleTypeSavingsGoal on the backend.
export const TRANSFER_GENERATING_RULE_TYPES = ['transfer_suggestion', 'savings_goal'] as const
export type TransferGeneratingRuleType = (typeof TRANSFER_GENERATING_RULE_TYPES)[number]

export const BOOSTS_TABS = ['active', 'awaiting', 'history', 'rules'] as const
export type BoostsTab = (typeof BOOSTS_TABS)[number]

export const TAB_LABELS: Record<BoostsTab, string> = {
  active: 'Active Recommendations',
  awaiting: 'Awaiting Match',
  history: 'History',
  rules: 'Rules',
}

export function isBoostsTab(value: string | null): value is BoostsTab {
  return BOOSTS_TABS.includes(value as BoostsTab)
}

// Shared formatters for TransferSuggestion tables
export const formatCurrency = (amount: number, currency: string) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount)

export const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
