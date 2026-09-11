import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConditionBuilder } from './ConditionBuilder'
import type { BoostConditionLocal } from '../types'
import type { Budget } from '../../budgets/types'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

const sampleConditions: BoostConditionLocal[] = [
  { field: 'budget.percent_used', operator: 'gte', value: 80 },
  { field: 'budget.remaining_amount', operator: 'lt', value: 100 },
]

// Two budgets with their own categories for budget→category cascade tests
const sampleBudgets: Budget[] = [
  {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Budget One',
    period: 'monthly',
    start_date: '2024-01-01',
    amount: 1000,
    currency_code: 'USD',
    is_active: true,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category_kind: 'pfc',
        pfc_primary: 'Food and Drink',
        pfc_primary_display: 'Food & Drink',
        allocated_amount: 500,
        spent_amount: 200,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category_kind: 'pfc',
        pfc_primary: 'Travel',
        pfc_primary_display: 'Travel',
        allocated_amount: 300,
        spent_amount: 100,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
    ],
  },
  {
    id: 'budget-2',
    user_id: 'user-1',
    name: 'Budget Two',
    period: 'monthly',
    start_date: '2024-01-01',
    amount: 500,
    currency_code: 'USD',
    is_active: true,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
    categories: [
      {
        id: 'cat-3',
        budget_id: 'budget-2',
        category_kind: 'pfc',
        pfc_primary: 'Entertainment',
        pfc_primary_display: 'Entertainment',
        allocated_amount: 200,
        spent_amount: 50,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
    ],
  },
]

const renderConditionBuilder = (props: Partial<Parameters<typeof ConditionBuilder>[0]> = {}) =>
  render(
    <ConditionBuilder
      conditions={[]}
      logic="AND"
      onConditionsChange={() => {}}
      onLogicChange={() => {}}
      triggerEvent="budget_updated"
      {...props}
    />
  )

describe('ConditionBuilder', () => {
  it('should show empty state when no conditions', () => {
    renderConditionBuilder()
    expect(screen.getByText(/no conditions/i)).toBeInTheDocument()
  })

  it('should show add condition button', () => {
    renderConditionBuilder()
    expect(screen.getByRole('button', { name: /add condition/i })).toBeInTheDocument()
  })

  it('should call onConditionsChange when add button is clicked', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({ onConditionsChange })
    fireEvent.click(screen.getByRole('button', { name: /add condition/i }))

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({
        field: expect.any(String),
        operator: expect.any(String),
        value: expect.anything(),
      }),
    ])
  })

  it('should display all conditions', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    const fieldSelects = screen.getAllByLabelText(/field/i)
    expect(fieldSelects).toHaveLength(2)
  })

  it('should show logic selector when multiple conditions exist', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    expect(screen.getByRole('button', { name: /and/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /or/i })).toBeInTheDocument()
  })

  it('should not show logic selector for single condition', () => {
    renderConditionBuilder({ conditions: [sampleConditions[0]] })

    expect(screen.queryByRole('button', { name: /^and$/i })).not.toBeInTheDocument()
  })

  it('should call onConditionsChange when field is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/field/i), {
      target: { value: 'budget.total_spent' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ field: 'budget.total_spent' }),
    ])
  })

  it('should call onConditionsChange when operator is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/operator/i), {
      target: { value: 'lt' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ operator: 'lt' }),
    ])
  })

  it('should call onConditionsChange when value is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/value/i), {
      target: { value: '90' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ value: 90 }),
    ])
  })

  it('should show remove button for each condition', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    expect(removeButtons).toHaveLength(2)
  })

  it('should call onConditionsChange when remove is clicked', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: sampleConditions,
      onConditionsChange,
    })

    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    fireEvent.click(removeButtons[0])

    expect(onConditionsChange).toHaveBeenCalledWith([sampleConditions[1]])
  })

  it('should highlight AND when logic is AND', () => {
    renderConditionBuilder({ conditions: sampleConditions, logic: 'AND' })

    expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'false')
  })

  it('should highlight OR when logic is OR', () => {
    renderConditionBuilder({ conditions: sampleConditions, logic: 'OR' })

    expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'true')
  })

  it('should call onLogicChange when logic is toggled', () => {
    const onLogicChange = mock(() => {})

    renderConditionBuilder({
      conditions: sampleConditions,
      onLogicChange,
    })

    fireEvent.click(screen.getByRole('button', { name: /or/i }))

    expect(onLogicChange).toHaveBeenCalledWith('OR')
  })

  it('should disable all controls when disabled prop is true', () => {
    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      disabled: true,
    })

    expect(screen.getByLabelText(/field/i)).toBeDisabled()
    expect(screen.getByLabelText(/operator/i)).toBeDisabled()
    expect(screen.getByLabelText(/value/i)).toBeDisabled()
  })

  it('should have accessible labels for all controls', () => {
    renderConditionBuilder({ conditions: [sampleConditions[0]] })

    expect(screen.getByLabelText(/field/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/operator/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/value/i)).toBeInTheDocument()
  })

  // Task 6: per-event field filtering and category picker tests

  it('should only show fields valid for the given trigger event (budget_updated)', () => {
    const budgetCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }
    renderConditionBuilder({
      conditions: [budgetCondition],
      triggerEvent: 'budget_updated',
    })

    const fieldSelect = screen.getByLabelText(/field/i)
    // budget.percent_used should be present
    expect(fieldSelect).toBeInTheDocument()
    const options = Array.from(fieldSelect.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).toContain('budget.percent_used')
    expect(options).toContain('budget.remaining_amount')
    expect(options).toContain('budget.total_spent')
    expect(options).toContain('budget_category.percent_used')
    expect(options).toContain('budget_category.spent_amount')
    expect(options).toContain('budget_category.remaining_amount')
    // account.balance and transaction fields should NOT appear
    expect(options).not.toContain('account.balance')
    expect(options).not.toContain('transaction.amount')
  })

  it('should show category picker when selected field starts with budget_category.', () => {
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      category_id: undefined,
      budget_id: 'budget-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    expect(screen.getByLabelText(/category/i)).toBeInTheDocument()
  })

  it('should not show category picker when selected field is not budget_category.*', () => {
    const budgetCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }
    renderConditionBuilder({
      conditions: [budgetCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    expect(screen.queryByLabelText(/category/i)).not.toBeInTheDocument()
  })

  it('should write category_id to condition when category picker changes', () => {
    const onConditionsChange = mock(() => {})
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      category_id: undefined,
      budget_id: 'budget-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/category/i), {
      target: { value: 'cat-1' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ category_id: 'cat-1' }),
    ])
  })

  it('should clear category_id when field changes away from budget_category.*', () => {
    const onConditionsChange = mock(() => {})
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      category_id: 'cat-1',
      budget_id: 'budget-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/field/i), {
      target: { value: 'budget.percent_used' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ field: 'budget.percent_used', category_id: undefined }),
    ])
  })

  it('should not include between operator in the operator select', () => {
    const budgetCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }
    renderConditionBuilder({
      conditions: [budgetCondition],
      triggerEvent: 'budget_updated',
    })

    const operatorSelect = screen.getByLabelText(/operator/i)
    const options = Array.from(operatorSelect.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).not.toContain('between')
  })

  it('should reset condition field when triggerEvent changes and existing field is no longer valid', () => {
    const onConditionsChange = mock(() => {})
    // Use a synthetic unknown field that won't appear in any event's field list
    const unknownFieldCondition: BoostConditionLocal = { field: 'account.balance', operator: 'gte', value: 100 }

    render(
      <ConditionBuilder
        conditions={[unknownFieldCondition]}
        logic="AND"
        onConditionsChange={onConditionsChange}
        onLogicChange={() => {}}
        triggerEvent="budget_updated"
      />
    )

    // account.balance is not in CONDITION_FIELDS_BY_EVENT['budget_updated'], so useEffect
    // should fire onConditionsChange with the first valid field
    expect(onConditionsChange).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({
          field: 'budget.percent_used',
          category_id: undefined,
        }),
      ])
    )
  })

  it('should not call onConditionsChange when all conditions have valid fields', () => {
    const onConditionsChange = mock(() => {})
    const validCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }

    render(
      <ConditionBuilder
        conditions={[validCondition]}
        logic="AND"
        onConditionsChange={onConditionsChange}
        onLogicChange={() => {}}
        triggerEvent="budget_updated"
      />
    )

    // budget.percent_used is valid for budget_updated — no reset should occur
    expect(onConditionsChange).not.toHaveBeenCalled()
  })

  // Item 9: budget→category cascade tests

  it('should show budget picker for budget_category.* fields', () => {
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    expect(screen.getByLabelText(/budget/i)).toBeInTheDocument()
  })

  it('should not show budget picker for non-category fields', () => {
    const budgetCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }
    renderConditionBuilder({
      conditions: [budgetCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    expect(screen.queryByLabelText(/^budget$/i)).not.toBeInTheDocument()
  })

  it('should scope category options to the selected budget', () => {
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    const categorySelect = screen.getByLabelText(/category/i)
    const options = Array.from(categorySelect.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).toContain('cat-1')
    expect(options).toContain('cat-2')
    expect(options).not.toContain('cat-3')
  })

  it('should clear category_id when budget changes', () => {
    const onConditionsChange = mock(() => {})
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-1',
      category_id: 'cat-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/budget/i), {
      target: { value: 'budget-2' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ budget_id: 'budget-2', category_id: undefined }),
    ])
  })

  it('should clear both budget_id and category_id when field changes away from budget_category.*', () => {
    const onConditionsChange = mock(() => {})
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-1',
      category_id: 'cat-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/field/i), {
      target: { value: 'budget.percent_used' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({
        field: 'budget.percent_used',
        budget_id: undefined,
        category_id: undefined,
      }),
    ])
  })

  it('should render disabled re-select option for stale budget_id', () => {
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-DELETED',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    const budgetSelect = screen.getByLabelText(/budget/i)
    const deletedOption = Array.from(budgetSelect.querySelectorAll('option')).find(
      (o) => (o as HTMLOptionElement).value === 'budget-DELETED'
    ) as HTMLOptionElement | undefined
    expect(deletedOption).toBeDefined()
    expect(deletedOption!.disabled).toBe(true)
    expect(deletedOption!.textContent).toMatch(/deleted/i)
  })

  it('should render disabled re-select option for stale category_id', () => {
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-1',
      category_id: 'cat-DELETED',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
    })

    const categorySelect = screen.getByLabelText(/category/i)
    const deletedOption = Array.from(categorySelect.querySelectorAll('option')).find(
      (o) => (o as HTMLOptionElement).value === 'cat-DELETED'
    ) as HTMLOptionElement | undefined
    expect(deletedOption).toBeDefined()
    expect(deletedOption!.disabled).toBe(true)
    expect(deletedOption!.textContent).toMatch(/deleted/i)
  })

  it('should include budget_id and category_id in onChange callback', () => {
    const onConditionsChange = mock(() => {})
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: sampleBudgets,
      onConditionsChange,
    })

    // First select the budget
    fireEvent.change(screen.getByLabelText(/budget/i), {
      target: { value: 'budget-1' },
    })

    // The callback should have budget_id set and category_id cleared
    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ budget_id: 'budget-1', category_id: undefined }),
    ])
  })

  it('should include budget_category.allocated as a selectable field option', () => {
    const budgetCondition: BoostConditionLocal = { field: 'budget.percent_used', operator: 'gte', value: 80 }
    renderConditionBuilder({
      conditions: [budgetCondition],
      triggerEvent: 'budget_updated',
    })

    const fieldSelect = screen.getByLabelText(/field/i)
    const options = Array.from(fieldSelect.querySelectorAll('option')).map((o) => (o as HTMLOptionElement).value)
    expect(options).toContain('budget_category.allocated')
  })

  // Issue #1514 (fix round 3): the budget picker's <option> text is the raw budget name,
  // and formatBudgetCategoryLabel() returns the user-authored savings-goal name for
  // savings-kind categories — both on the policy's PII list, both rendered as unmasked
  // <option> TEXT (the value attributes are already opaque ids). <option> cannot contain
  // a <span>, so the fix is data-ph-mask on the <option> element itself, not a wrapped
  // child — assert with the widened scanner so it covers text, not just attributes.
  it('masks the budget name and savings-goal name in the budget/category picker options', () => {
    const budgetsWithSavingsCategory: Budget[] = [
      {
        ...sampleBudgets[0],
        categories: [
          ...sampleBudgets[0].categories!,
          {
            id: 'cat-savings-1',
            budget_id: 'budget-1',
            category_kind: 'savings',
            savings_goal_name: 'My Vacation Fund',
            allocated_amount: 100,
            spent_amount: 0,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            tags: [],
            match_logic: 'category_only',
          },
        ],
      },
    ]
    const categoryCondition: BoostConditionLocal = {
      field: 'budget_category.percent_used',
      operator: 'gte',
      value: 80,
      budget_id: 'budget-1',
    }
    renderConditionBuilder({
      conditions: [categoryCondition],
      triggerEvent: 'budget_updated',
      budgets: budgetsWithSavingsCategory,
    })

    const sentinels: PIISentinel[] = [
      { value: 'Budget One', fieldDescription: 'budget.name in ConditionBuilder budget picker <option>' },
      { value: 'My Vacation Fund', fieldDescription: 'savings_goal_name in ConditionBuilder category picker <option>' },
    ]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})
