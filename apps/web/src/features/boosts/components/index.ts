/**
 * Boosts Components Index
 * Issue #1283: Rename rules → boosts (Phase 5)
 *
 * Exports all boost-related components.
 */

export { BoostCard } from './BoostCard'
export type { BoostCardProps } from './BoostCard'

export { BoostTable } from './BoostTable'
export type { BoostTableProps, SortColumn, SortDirection } from './BoostTable'

export { BoostsManagePage } from './BoostsManagePage'

export { BoostBuilder } from './BoostBuilder'

export { CronBuilder } from './CronBuilder'
export type { CronBuilderProps } from './CronBuilder'

export { TriggerSelector } from './TriggerSelector'
export type { TriggerSelectorProps } from './TriggerSelector'

export { ConditionBuilder } from './ConditionBuilder'
export type { ConditionBuilderProps } from './ConditionBuilder'

// Action forms
export * from './actions'

// Test results display
export { BoostTestResults } from './BoostTestResults'
export type { BoostTestResultsProps } from './BoostTestResults'

// Execution history
export { BoostExecutionHistory } from './BoostExecutionHistory'
export type { BoostExecutionHistoryProps } from './BoostExecutionHistory'

export { BoostExecutionTable } from './BoostExecutionTable'
export type { BoostExecutionTableProps } from './BoostExecutionTable'

// #1569: account-wide history — the only surface that reaches a DELETED boost's retained runs.
export { BoostAutomationHistoryPanel } from './BoostAutomationHistoryPanel'

// Existing boosts page components
export { BoostsPage } from './BoostsPage'
export { BoostsTab } from './BoostsTab'
export { BoostDetailPage } from './BoostDetailPage'
