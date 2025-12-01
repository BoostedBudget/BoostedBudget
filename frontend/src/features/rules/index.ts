/**
 * Rules Feature Module
 *
 * Public API for the categorization rules feature.
 * Issue #346: Custom Transaction Categorization Rules
 */

// Types
export type {
  ConditionType,
  RuleCondition,
  AmountRangeValue,
  DateRangeValue,
  ConditionLogic,
  TagBehavior,
  RuleSource,
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  ToggleRuleRequest,
  UseRulesResult,
} from './types'

export { CONDITION_TYPE_LABELS } from './types'

// API
export {
  fetchRules,
  fetchRuleById,
  createRule,
  updateRule,
  deleteRule,
  toggleRule,
} from './api/rulesApi'

// Hooks
export { useRules } from './hooks/useRules'

// Components
export { RuleCard } from './components/RuleCard'
export type { RuleCardProps } from './components/RuleCard'

export { RuleEditor } from './components/RuleEditor'
export type { RuleEditorProps } from './components/RuleEditor'

export { RulesPage } from './components/RulesPage'
export type { RulesPageProps } from './components/RulesPage'
