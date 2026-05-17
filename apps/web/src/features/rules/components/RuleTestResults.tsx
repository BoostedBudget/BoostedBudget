/**
 * RuleTestResults Component
 * Issue #39: Rule Builder UI
 *
 * Displays the results of a rule test execution.
 * Shows conditions evaluated, whether they passed, and simulated actions.
 */

import { CheckCircle, XCircle, AlertTriangle, X, Loader2 } from 'lucide-react'
import type { RuleTestResult, ConditionEvalResult } from '@boostedbudget/api-client/models'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'

export interface RuleTestResultsProps {
  /** Test result data */
  result: RuleTestResult | null
  /** Whether test is currently running */
  isLoading?: boolean
  /** Called when dialog is closed */
  onClose: () => void
  /** Additional CSS class */
  className?: string
}


/**
 * RuleTestResults displays the results of a rule test execution.
 */
export function RuleTestResults({
  result,
  isLoading = false,
  onClose,
  className = '',
}: RuleTestResultsProps) {
  // Loading state
  if (isLoading) {
    return (
      <div
        className={`fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/50 ${className}`.trim()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="test-results-title"
      >
        <div className="max-w-lg w-full max-h-[80vh] overflow-y-auto bg-background border border-border rounded-lg shadow-xl">
          <div className="flex flex-col items-center justify-center py-12 px-8 text-center gap-4">
            <Loader2 className="w-8 h-8 animate-spin text-primary" />
            <h2 id="test-results-title">Running Backtest...</h2>
            <p>Analyzing your recent data to show what this rule would have done.</p>
          </div>
        </div>
      </div>
    )
  }

  // No result yet
  if (!result) {
    return null
  }

  const ruleType = result.rule_type as RuleType
  const typeLabel = RULE_TYPE_LABELS[ruleType] || result.rule_type
  const hasError = !!result.error
  const conditionsPassed = result.conditions_met
  const conditions: ConditionEvalResult[] = result.condition_results || []
  const simulatedAction = result.simulated_action as Record<string, unknown> | undefined

  return (
    <div
      className={`fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/50 ${className}`.trim()}
      role="dialog"
      aria-modal="true"
      aria-labelledby="test-results-title"
    >
      <div className="max-w-lg w-full max-h-[80vh] overflow-y-auto bg-background border border-border rounded-lg shadow-xl">
        {/* Header */}
        <div className="px-6 py-4 border-b border-border">
          <div className="flex items-start justify-between gap-4">
            <h2 id="test-results-title" className="m-0 text-lg font-semibold text-foreground">
              {result.rule_name}
            </h2>
            <button
              type="button"
              onClick={onClose}
              className="flex items-center justify-center w-7 h-7 p-0 border-none bg-transparent text-muted-foreground cursor-pointer rounded hover:bg-muted hover:text-foreground transition-colors"
              aria-label="Close"
            >
              <X aria-hidden="true" className="w-4 h-4" />
            </button>
          </div>
          <span className="inline-block mt-1 text-xs text-muted-foreground capitalize">{typeLabel}</span>
        </div>

        {/* Status */}
        <div className="px-6 py-4 border-b border-border">
          {hasError ? (
            <div className="flex items-center gap-2 text-sm font-medium text-warning">
              <AlertTriangle className="w-5 h-5" />
              <span>Error</span>
            </div>
          ) : conditionsPassed ? (
            <div className="flex items-center gap-2 text-sm font-medium text-success">
              <CheckCircle className="w-5 h-5" />
              <span>Conditions Met</span>
            </div>
          ) : (
            <div className="flex items-center gap-2 text-sm font-medium text-destructive">
              <XCircle className="w-5 h-5" />
              <span>Conditions Not Met</span>
            </div>
          )}
        </div>

        {/* Error Message */}
        {hasError && (
          <div className="px-6 py-3 bg-destructive/10 border-b border-border">
            <p className="m-0 text-sm text-destructive">{result.error}</p>
          </div>
        )}

        {/* Condition Results */}
        {conditions.length > 0 && (
          <div className="px-6 py-4 border-b border-border last:border-b-0">
            <h3 className="m-0 mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Condition Results</h3>
            <div className="flex flex-col gap-2">
              {conditions.map((condition, index) => (
                <div
                  key={index}
                  className={`flex items-start gap-3 p-3 rounded-md ${condition.met ? 'bg-success/10' : 'bg-destructive/10'}`}
                >
                  <div className="flex-shrink-0">
                    {condition.met ? (
                      <CheckCircle
                        className="w-4 h-4 text-success"
                        data-testid="condition-passed"
                      />
                    ) : (
                      <XCircle
                        className="w-4 h-4 text-destructive"
                        data-testid="condition-failed"
                      />
                    )}
                  </div>
                  <div className="flex flex-col gap-1 flex-1 min-w-0">
                    <code className="text-sm font-mono text-foreground">
                      {condition.field}
                    </code>
                    <span className="text-xs text-muted-foreground">
                      {condition.operator} {String(condition.value)}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      Actual: {String(condition.actual)}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Simulated Action */}
        {conditionsPassed && simulatedAction && (
          <div className="px-6 py-4 border-b border-border last:border-b-0">
            <h3 className="m-0 mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Simulated Action</h3>
            <div className="flex flex-col gap-2">
              {!!simulatedAction.type && (
                <div className="flex items-start gap-2">
                  <span className="flex-shrink-0 text-xs font-medium text-muted-foreground">Type:</span>
                  <span className="text-sm text-foreground">
                    {String(simulatedAction.type)}
                  </span>
                </div>
              )}
              {Array.isArray(simulatedAction.channels) && (
                <div className="flex items-start gap-2">
                  <span className="flex-shrink-0 text-xs font-medium text-muted-foreground">Channels:</span>
                  <span className="text-sm text-foreground">
                    {simulatedAction.channels.map((c: string) => (
                      <span key={c} className="inline-block px-2 py-0.5 mr-1 text-xs bg-muted rounded-full">
                        {c}
                      </span>
                    ))}
                  </span>
                </div>
              )}
              {!!simulatedAction.message && (
                <div className="flex items-start gap-2">
                  <span className="flex-shrink-0 text-xs font-medium text-muted-foreground">Message:</span>
                  <span className="text-sm text-foreground">
                    {String(simulatedAction.message)}
                  </span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Footer */}
        <div className="px-6 py-4 flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm font-medium text-foreground bg-muted border border-border rounded-md cursor-pointer hover:bg-accent transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
