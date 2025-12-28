/**
 * RuleTestResults Component
 * Issue #39: Rule Builder UI
 *
 * Displays the results of a rule test execution.
 * Shows conditions evaluated, whether they passed, and simulated actions.
 */

import { CheckCircle, XCircle, AlertTriangle, X, Loader2 } from 'lucide-react'
import type { RuleTestResult, ConditionEvalResult } from '@/api/generated/models'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'
import './RuleTestResults.css'

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
        className={`rule-test-results ${className}`.trim()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="test-results-title"
      >
        <div className="rule-test-results__content">
          <div className="rule-test-results__loading">
            <Loader2 className="rule-test-results__spinner" />
            <h2 id="test-results-title">Testing Rule...</h2>
            <p>Running test execution, please wait.</p>
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
      className={`rule-test-results ${className}`.trim()}
      role="dialog"
      aria-modal="true"
      aria-labelledby="test-results-title"
    >
      <div className="rule-test-results__content">
        {/* Header */}
        <div className="rule-test-results__header">
          <div className="rule-test-results__title-row">
            <h2 id="test-results-title" className="rule-test-results__title">
              {result.rule_name}
            </h2>
            <button
              type="button"
              onClick={onClose}
              className="rule-test-results__close-btn"
              aria-label="Close"
            >
              <X aria-hidden="true" />
            </button>
          </div>
          <span className="rule-test-results__type">{typeLabel}</span>
        </div>

        {/* Status */}
        <div className="rule-test-results__status">
          {hasError ? (
            <div className="rule-test-results__status-error">
              <AlertTriangle className="rule-test-results__status-icon" />
              <span>Error</span>
            </div>
          ) : conditionsPassed ? (
            <div className="rule-test-results__status-success">
              <CheckCircle className="rule-test-results__status-icon" />
              <span>Conditions Met</span>
            </div>
          ) : (
            <div className="rule-test-results__status-failure">
              <XCircle className="rule-test-results__status-icon" />
              <span>Conditions Not Met</span>
            </div>
          )}
        </div>

        {/* Error Message */}
        {hasError && (
          <div className="rule-test-results__error">
            <p>{result.error}</p>
          </div>
        )}

        {/* Condition Results */}
        {conditions.length > 0 && (
          <div className="rule-test-results__section">
            <h3 className="rule-test-results__section-title">Condition Results</h3>
            <div className="rule-test-results__conditions">
              {conditions.map((condition, index) => (
                <div
                  key={index}
                  className={`rule-test-results__condition ${condition.met ? 'rule-test-results__condition--passed' : 'rule-test-results__condition--failed'}`}
                >
                  <div className="rule-test-results__condition-status">
                    {condition.met ? (
                      <CheckCircle
                        className="rule-test-results__condition-icon"
                        data-testid="condition-passed"
                      />
                    ) : (
                      <XCircle
                        className="rule-test-results__condition-icon"
                        data-testid="condition-failed"
                      />
                    )}
                  </div>
                  <div className="rule-test-results__condition-details">
                    <code className="rule-test-results__condition-field">
                      {condition.field}
                    </code>
                    <span className="rule-test-results__condition-comparison">
                      {condition.operator} {String(condition.value)}
                    </span>
                    <span className="rule-test-results__condition-actual">
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
          <div className="rule-test-results__section">
            <h3 className="rule-test-results__section-title">Simulated Action</h3>
            <div className="rule-test-results__action">
              {!!simulatedAction.type && (
                <div className="rule-test-results__action-row">
                  <span className="rule-test-results__action-label">Type:</span>
                  <span className="rule-test-results__action-value">
                    {String(simulatedAction.type)}
                  </span>
                </div>
              )}
              {Array.isArray(simulatedAction.channels) && (
                <div className="rule-test-results__action-row">
                  <span className="rule-test-results__action-label">Channels:</span>
                  <span className="rule-test-results__action-value">
                    {simulatedAction.channels.map((c: string) => (
                      <span key={c} className="rule-test-results__channel-badge">
                        {c}
                      </span>
                    ))}
                  </span>
                </div>
              )}
              {!!simulatedAction.message && (
                <div className="rule-test-results__action-row">
                  <span className="rule-test-results__action-label">Message:</span>
                  <span className="rule-test-results__action-value">
                    {String(simulatedAction.message)}
                  </span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Footer */}
        <div className="rule-test-results__footer">
          <button
            type="button"
            onClick={onClose}
            className="rule-test-results__close-action"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
