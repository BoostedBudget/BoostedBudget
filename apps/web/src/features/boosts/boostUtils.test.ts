/**
 * Rule formatting utility tests
 * Issue #1267 R3-3: direct unit coverage for formatBoostConditionsSummary + formatBoostActionSummary
 */

import { describe, it, expect } from 'bun:test'
import { formatBoostConditionsSummary, formatBoostActionSummary } from './boostUtils'
import type { BoostCondition } from '@boostedbudget/api-client/models'

// ---------------------------------------------------------------------------
// formatBoostConditionsSummary
// ---------------------------------------------------------------------------

describe('formatBoostConditionsSummary', () => {
  it('returns empty string for empty conditions array', () => {
    expect(formatBoostConditionsSummary([])).toBe('')
  })

  it('returns empty string for null conditions', () => {
    expect(formatBoostConditionsSummary(null)).toBe('')
  })

  it('returns empty string for undefined conditions', () => {
    expect(formatBoostConditionsSummary(undefined)).toBe('')
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
      const conds: BoostCondition[] = [
        { field: 'budget.percent_used', operator, value },
      ]
      expect(formatBoostConditionsSummary(conds)).toBe(expected)
    })
  })

  it('falls back to raw operator string for unknown operators', () => {
    const conds: BoostCondition[] = [
      { field: 'budget.percent_used', operator: 'between', value: 50 },
    ]
    const result = formatBoostConditionsSummary(conds)
    expect(result).toContain('between')
  })

  it('uses raw field name as fallback for unknown field', () => {
    const conds: BoostCondition[] = [
      { field: 'account.balance', operator: 'gte', value: 1000 },
    ]
    const result = formatBoostConditionsSummary(conds)
    expect(result).toContain('account.balance')
    expect(result).toContain('≥')
    expect(result).toContain('1000')
  })

  it('joins multiple conditions with AND by default', () => {
    const conds: BoostCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'budget.percent_used', operator: 'lt', value: 100 },
    ]
    expect(formatBoostConditionsSummary(conds)).toBe(
      'When Budget % Used ≥ 80 AND Budget % Used < 100'
    )
  })

  it('joins multiple conditions with OR when specified', () => {
    const conds: BoostCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'budget.percent_used', operator: 'lt', value: 100 },
    ]
    expect(formatBoostConditionsSummary(conds, 'OR')).toBe(
      'When Budget % Used ≥ 80 OR Budget % Used < 100'
    )
  })

  it('handles null value gracefully', () => {
    const conds: BoostCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: null as unknown as number },
    ]
    const result = formatBoostConditionsSummary(conds)
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
      const conds: BoostCondition[] = [{ field, operator: 'gte', value: 10 }]
      const result = formatBoostConditionsSummary(conds)
      expect(result).toContain(expectedLabel)
    })
  })
})

// ---------------------------------------------------------------------------
// formatBoostActionSummary
// ---------------------------------------------------------------------------

describe('formatBoostActionSummary', () => {
  it('returns transfer suggestion with amount when actions.amount is a number', () => {
    expect(formatBoostActionSummary('transfer_suggestion', { amount: 200 })).toBe(
      'Suggest transfer $200'
    )
  })

  it('returns transfer suggestion without amount when actions.amount is absent', () => {
    expect(formatBoostActionSummary('transfer_suggestion', {})).toBe('Suggest transfer')
  })

  it('returns transfer suggestion without amount when actions is null', () => {
    expect(formatBoostActionSummary('transfer_suggestion', null)).toBe('Suggest transfer')
  })

  it('returns transfer suggestion without amount when actions.amount is not a number', () => {
    expect(formatBoostActionSummary('transfer_suggestion', { amount: 'lots' })).toBe(
      'Suggest transfer'
    )
  })

  it('formats budget_alert rule type', () => {
    expect(formatBoostActionSummary('budget_alert', {})).toBe(
      'Send budget alert notification'
    )
  })

  it('formats category_rollover rule type', () => {
    expect(formatBoostActionSummary('category_rollover', {})).toBe('Roll over category funds')
  })

  it('formats savings_goal rule type', () => {
    expect(formatBoostActionSummary('savings_goal', {})).toBe(
      'Auto-contribute to savings goal'
    )
  })

  it('returns default message for unknown rule type', () => {
    expect(formatBoostActionSummary('custom_unknown', {})).toBe('Execute boost action')
  })

  it('formats large amounts with locale separators', () => {
    const result = formatBoostActionSummary('transfer_suggestion', { amount: 1500 })
    // Should contain $1,500 (locale-formatted)
    expect(result).toContain('$1,500')
  })
})
