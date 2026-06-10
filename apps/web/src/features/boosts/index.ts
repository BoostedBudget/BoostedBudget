// Issue #1267: Boosts feature public API
// Issue #1283: Rename rules → boosts (Phase 5) — all components migrated here
export { BoostsPage } from './components/BoostsPage'
export { RuleDetailPage } from './components/RuleDetailPage'
export { BoostBuilder } from './components/BoostBuilder'
export { BoostsManagePage } from './components/BoostsManagePage'
export { BoostCard } from './components/BoostCard'
export { BoostTable } from './components/BoostTable'
export { BoostExecutionHistory } from './components/BoostExecutionHistory'
export { BoostTestResults } from './components/BoostTestResults'
export { ConditionBuilder } from './components/ConditionBuilder'
export { CronBuilder } from './components/CronBuilder'
export { TriggerSelector } from './components/TriggerSelector'
export * from './components/actions'

// Hooks
export { useBoostForm } from './hooks/useBoostForm'
export { useBoostFilters } from './hooks/useBoostFilters'

// Types
export type {
  BoostType,
  TriggerType,
  TriggerFormState,
  EventType,
  BoostConditionLocal,
  ConditionOperator,
  ConditionLogic,
  AlertAction,
  TransferAction,
  RolloverAction,
  SavingsGoalAction,
  BoostAction,
  BoostFormState,
} from './types'

// Constants
export {
  BOOST_TYPE_LABELS,
  BOOST_TYPE_DESCRIPTIONS,
  TRIGGER_TYPE_LABELS,
  TRIGGER_TYPE_DESCRIPTIONS,
  CONDITION_FIELD_OPTIONS,
  CONDITION_OPERATOR_LABELS,
  EVENT_TYPE_OPTIONS,
  CRON_PRESETS,
  NOTIFICATION_CHANNEL_OPTIONS,
  DEFAULT_PRIORITY,
  DEFAULT_TRIGGER_CONFIG,
  DEFAULT_CRON,
  DEFAULT_TIMEZONE,
} from './boostConstants'
