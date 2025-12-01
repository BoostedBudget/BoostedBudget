/**
 * Auto-Categorization Feature Module
 *
 * Public API for the auto-categorization rules feature.
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
  UseAutoCategorizationRulesResult,
} from './types'

export { CONDITION_TYPE_LABELS } from './types'

// API
export {
  fetchAutoCategorizationRules,
  fetchAutoCategorizationRuleById,
  createAutoCategorizationRule,
  updateAutoCategorizationRule,
  deleteAutoCategorizationRule,
  toggleAutoCategorizationRule,
} from './api/autoCategorizationApi'

// Hooks
export { useAutoCategorizationRules } from './hooks/useAutoCategorizationRules'

// Components
export { AutoCategorizationRuleCard } from './components/AutoCategorizationRuleCard'
export type { AutoCategorizationRuleCardProps } from './components/AutoCategorizationRuleCard'

export { AutoCategorizationRuleEditor } from './components/AutoCategorizationRuleEditor'
export type { AutoCategorizationRuleEditorProps } from './components/AutoCategorizationRuleEditor'

export { AutoCategorizationPage } from './components/AutoCategorizationPage'
export type { AutoCategorizationPageProps } from './components/AutoCategorizationPage'

export { AutoCategorizationPageWrapper } from './components/AutoCategorizationPageWrapper'
