/**
 * Rule formatting utility tests
 * Issue #1267 R3-3: direct unit coverage for formatRuleConditionsSummary + formatRuleActionSummary
 */

import { describe, it, expect } from 'bun:test'
import { formatRuleConditionsSummary, formatRuleActionSummary } from './utils'
import type { AutomationRuleCondition } from '@boostedbudget/api-client/models'

// ---------------------------------------------------------------------------
// formatRuleConditionsSummary
// ---------------------------------------------------------------------------

describe('formatRuleConditionsSummary', () => {
  it('returns empty string for empty conditions array', () => {
    expect(formatRuleConditionsSummary([])).toBe('')
  })

  it('returns empty string for null conditions', () => {
    expect(formatRuleConditionsSummary(null)).toBe('')
  })

  it('returns empty string for undefined conditions', () => {
    expect(formatRuleConditionsSummary(undefined)).toBe('')
  })

  const operatorCases: Array<[string, string, number | string, string]> = [
    ['eq', '=', 100, 'When Budget % Used = 100'],
    ['neq', '≠', 50, 'When Budget % Used ≠ 50'],
    ['gt', '>', 75, 'When Budget % Used > 75'],
    ['gte', '≥', 80, 'When Budget % Used ≥ 80'],
    ['lt', '<', 30, 'When Budget % Used < 30'],
    ['lte', '≤', 60, 'When Budget % Used ≤ 60'],
    ['contains', 'contains', 'foo', 'When Budget % Used contains foo'],
  ]

  operatorCases.forEach(([operator, symbol, value, expected]) => {
    it(`renders ${operator} operator as ${symbol}`, () => {
      const conds: AutomationRuleCondition[] = [
        { field: 'budget.percent_used', operator, value },
      ]
      expect(formatRuleConditionsSummary(conds)).toBe(expected)
    })
  })

  it('falls back to raw operator string for unknown operators', () => {
    const conds: AutomationRuleCondition[] = [
      { field: 'budget.percent_used', operator: 'between', value: 50 },
    ]
    const result = formatRuleConditionsSummary(conds)
    expect(result).toContain('between')
  })

  it('uses raw field name as fallback for unknown field', () => {
    const conds: AutomationRuleCondition[] = [
      { field: 'account.balance', operator: 'gte', value: 1000 },
    ]
    const result = formatRuleConditionsSummary(conds)
    expect(result).toContain('account.balance')
    expect(result).toContain('≥')
    expect(result).toContain('1000')
  })

  it('joins multiple conditions with AND by default', () => {
    const conds: AutomationRuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'budget.percent_used', operator: 'lt', value: 100 },
    ]
    expect(formatRuleConditionsSummary(conds)).toBe(
      'When Budget % Used ≥ 80 AND Budget % Used < 100'
    )
  })

  it('joins multiple conditions with OR when specified', () => {
    const conds: AutomationRuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'budget.percent_used', operator: 'lt', value: 100 },
    ]
    expect(formatRuleConditionsSummary(conds, 'OR')).toBe(
      'When Budget % Used ≥ 80 OR Budget % Used < 100'
    )
  })

  it('handles null value gracefully', () => {
    const conds: AutomationRuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: null as unknown as number },
    ]
    const result = formatRuleConditionsSummary(conds)
    expect(result).toContain('When')
    expect(result).toContain('≥')
  })

  // R4-4: label-map regression guard — every CONDITION_FIELD_OPTIONS entry must produce its human label
  const fieldLabelCases: Array<[string, string]> = [
    ['budget.percent_used', 'Budget % Used'],
    ['budget.total_spent', 'Budget Total Spent'],
    ['budget.remaining_amount', 'Budget Remaining'],
    ['budget_category.percent_used', 'Category % Used'],
    ['budget_category.spent_amount', 'Category Spent'],
    ['budget_category.remaining_amount', 'Category Remaining'],
    ['budget_category.allocated', 'Category Allocated'],
  ]

  fieldLabelCases.forEach(([field, expectedLabel]) => {
    it(`renders human label "${expectedLabel}" for field "${field}"`, () => {
      const conds: AutomationRuleCondition[] = [{ field, operator: 'gte', value: 10 }]
      const result = formatRuleConditionsSummary(conds)
      expect(result).toContain(expectedLabel)
    })
  })
})

// ---------------------------------------------------------------------------
// formatRuleActionSummary
// ---------------------------------------------------------------------------

describe('formatRuleActionSummary', () => {
  it('returns transfer suggestion with amount when actions.amount is a number', () => {
    expect(formatRuleActionSummary('transfer_suggestion', { amount: 200 })).toBe(
      'Suggest transfer $200'
    )
  })

  it('returns transfer suggestion without amount when actions.amount is absent', () => {
    expect(formatRuleActionSummary('transfer_suggestion', {})).toBe('Suggest transfer')
  })

  it('returns transfer suggestion without amount when actions is null', () => {
    expect(formatRuleActionSummary('transfer_suggestion', null)).toBe('Suggest transfer')
  })

  it('returns transfer suggestion without amount when actions.amount is not a number', () => {
    expect(formatRuleActionSummary('transfer_suggestion', { amount: 'lots' })).toBe(
      'Suggest transfer'
    )
  })

  it('formats budget_alert rule type', () => {
    expect(formatRuleActionSummary('budget_alert', {})).toBe(
      'Send budget alert notification'
    )
  })

  it('formats category_rollover rule type', () => {
    expect(formatRuleActionSummary('category_rollover', {})).toBe('Roll over category funds')
  })

  it('formats savings_goal rule type', () => {
    expect(formatRuleActionSummary('savings_goal', {})).toBe(
      'Auto-contribute to savings goal'
    )
  })

  it('returns default message for unknown rule type', () => {
    expect(formatRuleActionSummary('custom_unknown', {})).toBe('Execute rule action')
  })

  it('formats large amounts with locale separators', () => {
    const result = formatRuleActionSummary('transfer_suggestion', { amount: 1500 })
    // Should contain $1,500 (locale-formatted)
    expect(result).toContain('$1,500')
  })
})
