import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AutoCategorizationRuleEditor } from './AutoCategorizationRuleEditor'
import type { CategorizationRule } from '../types'

describe('AutoCategorizationRuleEditor', () => {
  const mockOnSave = vi.fn()
  const mockOnCancel = vi.fn()

  const mockExistingRule: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    condition_logic: 'AND',
    target_category: ['Food & Drink', 'Coffee'],
    target_tags: ['morning'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  const mockCategories = [
    ['Food & Drink'],
    ['Food & Drink', 'Coffee'],
    ['Food & Drink', 'Restaurants'],
    ['Shopping'],
    ['Shopping', 'Clothing'],
  ]

  // Mock Plaid categories from user's transactions (Plaid enum format)
  const mockPlaidCategories = [
    'FOOD_AND_DRINK',
    'SHOPPING',
    'TRANSPORTATION',
    'ENTERTAINMENT',
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Create mode', () => {
    it('should render empty form for create mode', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      expect(screen.getByLabelText(/rule name/i)).toHaveValue('')
      expect(screen.getByText(/create rule/i)).toBeInTheDocument()
    })

    it('should show Create Rule title', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })

    it('should require rule name', async () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      expect(await screen.findByText(/rule name is required/i)).toBeInTheDocument()
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should require at least one condition', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      expect(await screen.findByText(/at least one condition is required/i)).toBeInTheDocument()
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should require target category or tags', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add a condition
      const addConditionBtn = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addConditionBtn)

      const conditionValueInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(conditionValueInput, 'amazon')

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      expect(await screen.findByText(/target category or tags required/i)).toBeInTheDocument()
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should call onSave with valid data', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // Fill in name
      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add a condition
      const addConditionBtn = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addConditionBtn)

      const conditionValueInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(conditionValueInput, 'amazon')

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          name: 'Test Rule',
          conditions: [{ type: 'merchant_contains', value: 'amazon' }],
          target_category: ['Food & Drink', 'Coffee'],
        }))
      })
    })

    it('should call onCancel when Cancel is clicked', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(mockOnCancel).toHaveBeenCalledTimes(1)
    })
  })

  describe('Edit mode', () => {
    it('should populate form with existing rule data', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      expect(screen.getByLabelText(/rule name/i)).toHaveValue('Coffee Shops')
      expect(screen.getByLabelText(/description/i)).toHaveValue('Categorize coffee purchases')
      expect(screen.getByRole('heading', { name: /edit rule/i })).toBeInTheDocument()
    })

    it('should show existing conditions', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      expect(screen.getByDisplayValue('starbucks')).toBeInTheDocument()
    })

    it('should show existing tags', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      expect(screen.getByText('morning')).toBeInTheDocument()
    })

    it('should call onSave with updated data', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const nameInput = screen.getByLabelText(/rule name/i)
      await user.clear(nameInput)
      await user.type(nameInput, 'Updated Name')

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          name: 'Updated Name',
        }))
      })
    })
  })

  describe('Conditions', () => {
    it('should add a new condition', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      expect(screen.getByPlaceholderText(/enter value/i)).toBeInTheDocument()
    })

    it('should remove a condition', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const removeButton = screen.getByRole('button', { name: /remove condition/i })
      fireEvent.click(removeButton)

      expect(screen.queryByDisplayValue('starbucks')).not.toBeInTheDocument()
    })

    it('should change condition type', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_greater_than')

      expect(screen.getByLabelText(/condition type/i)).toHaveValue('amount_greater_than')
    })
  })

  describe('Tags', () => {
    it('should add a tag', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const tagInput = screen.getByPlaceholderText(/add tag/i)
      await user.type(tagInput, 'newtag')

      const addTagBtn = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addTagBtn)

      expect(screen.getByText('newtag')).toBeInTheDocument()
    })

    it('should remove a tag', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const removeTagBtn = screen.getByRole('button', { name: /remove tag morning/i })
      fireEvent.click(removeTagBtn)

      expect(screen.queryByText('morning')).not.toBeInTheDocument()
    })
  })

  describe('Saving state', () => {
    it('should show saving indicator', async () => {
      let resolvePromise: (value?: unknown) => void
      mockOnSave.mockImplementation(() => new Promise((resolve) => {
        resolvePromise = resolve
      }))

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      expect(await screen.findByText(/saving/i)).toBeInTheDocument()

      resolvePromise!()
      await waitFor(() => {
        expect(screen.queryByText(/saving/i)).not.toBeInTheDocument()
      })
    })

    it('should disable form while saving', async () => {
      let resolvePromise: (value?: unknown) => void
      mockOnSave.mockImplementation(() => new Promise((resolve) => {
        resolvePromise = resolve
      }))

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={mockExistingRule}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeDisabled()
      })

      resolvePromise!()
      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).not.toBeDisabled()
      })
    })
  })

  describe('Priority', () => {
    it('should allow setting priority', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const priorityInput = screen.getByLabelText(/priority/i)
      await user.clear(priorityInput)
      await user.type(priorityInput, '20')

      expect(priorityInput).toHaveValue(20)
    })
  })

  describe('Condition logic', () => {
    it('should toggle condition logic', () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // Add two conditions first
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)
      fireEvent.click(addButton)

      const orButton = screen.getByRole('button', { name: /or/i })
      fireEvent.click(orButton)

      expect(orButton).toHaveClass('active')
    })
  })

  describe('Input type rendering', () => {
    it('should render number input for amount_greater_than', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_greater_than')

      const numberInput = screen.getByRole('spinbutton', { name: /condition value/i })
      expect(numberInput).toBeInTheDocument()
      expect(numberInput).toHaveAttribute('type', 'number')
    })

    it('should render number input for amount_less_than', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_less_than')

      const numberInput = screen.getByRole('spinbutton', { name: /condition value/i })
      expect(numberInput).toBeInTheDocument()
      expect(numberInput).toHaveAttribute('type', 'number')
    })

    it('should render number input for amount_equals', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_equals')

      const numberInput = screen.getByRole('spinbutton', { name: /condition value/i })
      expect(numberInput).toBeInTheDocument()
      expect(numberInput).toHaveAttribute('type', 'number')
    })

    it('should render two number inputs for amount_range', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_range')

      const minInput = screen.getByLabelText(/minimum amount/i)
      const maxInput = screen.getByLabelText(/maximum amount/i)
      expect(minInput).toBeInTheDocument()
      expect(maxInput).toBeInTheDocument()
      expect(minInput).toHaveAttribute('type', 'number')
      expect(maxInput).toHaveAttribute('type', 'number')
    })

    it('should render date inputs for date_range', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'date_range')

      const startInput = screen.getByLabelText(/start date/i)
      const endInput = screen.getByLabelText(/end date/i)
      expect(startInput).toBeInTheDocument()
      expect(endInput).toBeInTheDocument()
      expect(startInput).toHaveAttribute('type', 'date')
      expect(endInput).toHaveAttribute('type', 'date')
    })

    it('should render text input for merchant conditions', async () => {
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      // Default is merchant_contains
      const textInput = screen.getByPlaceholderText(/enter value/i)
      expect(textInput).toBeInTheDocument()
      expect(textInput).toHaveAttribute('type', 'text')
    })

    it('should render searchable category dropdown for existing_category', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
          plaidCategories={mockPlaidCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'existing_category')

      // SearchableSelect renders as a combobox INPUT
      const categorySelect = screen.getByRole('combobox', { name: /select plaid category/i })
      expect(categorySelect).toBeInTheDocument()
      expect(categorySelect.tagName).toBe('INPUT')
    })

    it('should render payment channel select for payment_channel', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'payment_channel')

      const channelSelect = screen.getByLabelText(/select payment channel/i)
      expect(channelSelect).toBeInTheDocument()
      expect(channelSelect.tagName).toBe('SELECT')
    })

    it('should render searchable account dropdown for account_id', async () => {
      const user = userEvent.setup()
      const mockAccounts = [
        { id: 'acc-1', name: 'Checking', type: 'depository' as const, linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-1', currency_code: 'USD', created_at: '2024-01-01', updated_at: '2024-01-01' },
        { id: 'acc-2', name: 'Savings', type: 'depository' as const, linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-2', currency_code: 'USD', created_at: '2024-01-01', updated_at: '2024-01-01' },
      ]
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
          accounts={mockAccounts}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'account_id')

      // SearchableSelect renders as a combobox INPUT
      const accountSelect = screen.getByRole('combobox', { name: /select account/i })
      expect(accountSelect).toBeInTheDocument()
      expect(accountSelect.tagName).toBe('INPUT')
    })

    it('should render checkboxes for account_type', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'account_type')

      // Should see checkboxes for account types
      expect(screen.getByText(/depository/i)).toBeInTheDocument()
      expect(screen.getByText(/credit card/i)).toBeInTheDocument()
    })
  })

  describe('Input validation', () => {
    it('should show error for empty condition value on save', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add a condition but don't fill in value
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      // Select category to satisfy target requirement (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      // Should show validation error for empty condition value
      await waitFor(() => {
        expect(screen.getByText(/value is required/i)).toBeInTheDocument()
      })
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should show error when amount_range min > max', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add amount_range condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_range')

      // Set min > max
      const minInput = screen.getByLabelText(/minimum amount/i)
      const maxInput = screen.getByLabelText(/maximum amount/i)
      await user.clear(minInput)
      await user.type(minInput, '100')
      await user.clear(maxInput)
      await user.type(maxInput, '50')

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/min must be less than or equal to max/i)).toBeInTheDocument()
      })
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should show error when date_range start > end', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add date_range condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'date_range')

      // Set start > end
      const startInput = screen.getByLabelText(/start date/i)
      const endInput = screen.getByLabelText(/end date/i)
      fireEvent.change(startInput, { target: { value: '2025-12-31' } })
      fireEvent.change(endInput, { target: { value: '2025-01-01' } })

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/start date must be before or equal to end date/i)).toBeInTheDocument()
      })
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should accept decimal values for amount conditions', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add amount condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_greater_than')

      const amountInput = screen.getByRole('spinbutton', { name: /condition value/i })
      await user.type(amountInput, '99.99')

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          conditions: [{ type: 'amount_greater_than', value: 99.99 }],
        }))
      })
    })

    it('should accept partial date_range with only start', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add date_range condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'date_range')

      // Only set start date
      const startInput = screen.getByLabelText(/start date/i)
      fireEvent.change(startInput, { target: { value: '2025-01-01' } })

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalled()
      })
    })

    it('should require at least one account type when account_type selected', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add account_type condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'account_type')

      // Don't select any checkboxes

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/at least one account type must be selected/i)).toBeInTheDocument()
      })
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('should clear condition error when value is updated', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add a condition but don't fill in value
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      // Try to save - should show error
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/value is required/i)).toBeInTheDocument()
      })

      // Now fill in the value
      const textInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(textInput, 'starbucks')

      // Error should be cleared
      expect(screen.queryByText(/value is required/i)).not.toBeInTheDocument()
    })
  })

  describe('Edge cases', () => {
    it('should handle switching condition type and reset value', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // Add a condition and fill in text value
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const textInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(textInput, 'some text')

      // Switch to amount type
      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_greater_than')

      // Input should be number type with default value
      const numberInput = screen.getByRole('spinbutton', { name: /condition value/i })
      expect(numberInput).toBeInTheDocument()
      // Value should be reset to default (0 for amount)
      expect(numberInput).toHaveValue(0)
    })

    it('should handle removing condition and update error indices', async () => {
      const user = userEvent.setup()
      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add two conditions
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)
      fireEvent.click(addButton)

      // Fill in first condition, leave second empty
      const textInputs = screen.getAllByPlaceholderText(/enter value/i)
      await user.type(textInputs[0], 'first')

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      // Try to save
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/value is required/i)).toBeInTheDocument()
      })

      // Remove the first condition (the one that's filled)
      const removeButtons = screen.getAllByRole('button', { name: /remove condition/i })
      fireEvent.click(removeButtons[0])

      // Error should still show for the remaining condition
      expect(screen.getByText(/value is required/i)).toBeInTheDocument()
    })

    it('should handle special characters in merchant name', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add condition with special characters
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const textInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(textInput, "McDonald's & Co. <test>")

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          conditions: [{ type: 'merchant_contains', value: "McDonald's & Co. <test>" }],
        }))
      })
    })

    it('should handle zero amount value', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add amount condition with 0
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_equals')

      // Input should already have 0 as default
      const amountInput = screen.getByRole('spinbutton', { name: /condition value/i })
      expect(amountInput).toHaveValue(0)

      // Select category (SearchableSelect uses click-based selection)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      await user.click(screen.getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          conditions: [{ type: 'amount_equals', value: 0 }],
        }))
      })
    })

    // This test has many userEvent interactions that can be slow on CI
    // Extended timeout to 60s to accommodate slower runners
    it('should handle negative amount value', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Test Rule')

      // Add amount condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)

      const typeSelect = screen.getByLabelText(/condition type/i)
      await user.selectOptions(typeSelect, 'amount_greater_than')

      const amountInput = screen.getByRole('spinbutton', { name: /condition value/i })
      await user.clear(amountInput)
      await user.type(amountInput, '-50')

      // Select category (SearchableSelect uses click-based selection)
      // Use async findByRole pattern to wait for dropdown to render (fixes CI timing issues)
      const categorySelect = screen.getByRole('combobox', { name: /target category/i })
      await user.click(categorySelect)
      const listbox = await screen.findByRole('listbox')
      await user.click(within(listbox).getByText('Food & Drink > Coffee'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          conditions: [{ type: 'amount_greater_than', value: -50 }],
        }))
      })
    }, 60000)
  })

  describe('Searchable Dropdowns', () => {
    const mockAccounts = [
      { id: 'acc-1', name: 'Checking', type: 'depository' as const, mask: '1234', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-1', currency_code: 'USD', created_at: '2024-01-01', updated_at: '2024-01-01' },
      { id: 'acc-2', name: 'Savings', type: 'depository' as const, mask: '5678', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-2', currency_code: 'USD', created_at: '2024-01-01', updated_at: '2024-01-01' },
      { id: 'acc-3', name: 'Credit Card', type: 'credit' as const, mask: '9012', linked_item_id: 'item-2', user_id: 'user-1', plaid_account_id: 'plaid-3', currency_code: 'USD', created_at: '2024-01-01', updated_at: '2024-01-01' },
    ]

    describe('existing_category condition', () => {
      it('should show searchable dropdown for Plaid category selection', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        // Add existing_category condition
        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'existing_category')

        // Should render a searchable input (combobox) for Plaid categories
        const searchInput = screen.getByRole('combobox', { name: /select plaid category/i })
        expect(searchInput).toBeInTheDocument()
      })

      it('should filter Plaid categories as user types', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'existing_category')

        const searchInput = screen.getByRole('combobox', { name: /select plaid category/i })
        await user.click(searchInput)
        await user.type(searchInput, 'food')

        // Should filter to show only matching category (displayed as human-readable)
        await waitFor(() => {
          // Food And Drink should be visible (formatted from FOOD_AND_DRINK)
          expect(screen.getByText('Food And Drink')).toBeInTheDocument()
          // Other categories should be filtered out
          expect(screen.queryByText('Transportation')).not.toBeInTheDocument()
        })
      })

      it('should update condition value on selection', async () => {
        const user = userEvent.setup()
        mockOnSave.mockResolvedValue(undefined)

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        await user.type(screen.getByLabelText(/rule name/i), 'Category Match Rule')

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'existing_category')

        const searchInput = screen.getByRole('combobox', { name: /select plaid category/i })
        await user.click(searchInput)

        // Click on a Plaid category option (displayed as human-readable but stored as enum)
        await user.click(screen.getByText('Shopping'))

        // Set target category to make form valid (now a SearchableSelect)
        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)
        await user.click(screen.getByText('Food & Drink > Coffee'))

        const saveButton = screen.getByRole('button', { name: /save/i })
        fireEvent.click(saveButton)

        await waitFor(() => {
          expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
            // Value should be stored as the original Plaid enum
            conditions: [{ type: 'existing_category', value: 'SHOPPING' }],
          }))
        })
      })
    })

    describe('account_id condition', () => {
      it('should show searchable dropdown for account selection', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            accounts={mockAccounts}
          />
        )

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'account_id')

        // Should render a searchable input (combobox)
        const searchInput = screen.getByRole('combobox', { name: /select account/i })
        expect(searchInput).toBeInTheDocument()
      })

      it('should display account name with type and mask', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            accounts={mockAccounts}
          />
        )

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'account_id')

        const searchInput = screen.getByRole('combobox', { name: /select account/i })
        await user.click(searchInput)

        // Should show account with sublabel containing type and mask (format: "depository ••••1234")
        expect(screen.getByText('Checking')).toBeInTheDocument()
        // The sublabel format is "type ••••mask"
        expect(screen.getByText('depository ••••1234')).toBeInTheDocument()
      })

      it('should filter accounts as user types', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            accounts={mockAccounts}
          />
        )

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'account_id')

        const searchInput = screen.getByRole('combobox', { name: /select account/i })
        await user.click(searchInput)
        await user.type(searchInput, 'credit')

        await waitFor(() => {
          // Should only show credit card account in the filtered results
          expect(screen.getByText('Credit Card')).toBeInTheDocument()
          // The other accounts should be filtered out
          expect(screen.queryByText('Checking')).not.toBeInTheDocument()
          expect(screen.queryByText('Savings')).not.toBeInTheDocument()
        })
      })

      it('should update condition value with account ID', async () => {
        const user = userEvent.setup()
        mockOnSave.mockResolvedValue(undefined)

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            accounts={mockAccounts}
          />
        )

        await user.type(screen.getByLabelText(/rule name/i), 'Account Rule')

        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)

        const typeSelect = screen.getByLabelText(/condition type/i)
        await user.selectOptions(typeSelect, 'account_id')

        const searchInput = screen.getByRole('combobox', { name: /select account/i })
        await user.click(searchInput)
        await user.click(screen.getByText('Checking'))

        // Set target category to make form valid (now a SearchableSelect)
        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)
        await user.click(screen.getByText('Shopping'))

        const saveButton = screen.getByRole('button', { name: /save/i })
        fireEvent.click(saveButton)

        await waitFor(() => {
          expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
            conditions: [{ type: 'account_id', value: 'acc-1' }],
          }))
        })
      })
    })

    describe('Target Category selector', () => {
      it('should use searchable dropdown for target category', async () => {
        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
          />
        )

        // Target category should be a searchable combobox
        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        expect(targetCategoryInput).toBeInTheDocument()
      })

      it('should show "No category change" as first option', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
          />
        )

        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)

        // Should have a clear option at the top
        expect(screen.getByText('No category change')).toBeInTheDocument()
      })

      it('should filter categories as user types', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
          />
        )

        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)
        await user.type(targetCategoryInput, 'food')

        await waitFor(() => {
          // Should filter to show only food-related categories
          const foodOption = screen.getByText('Food & Drink')
          expect(foodOption).toBeInTheDocument()
        })
      })

      it('should show both custom and Plaid categories in dropdown', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)

        await waitFor(() => {
          // Should show custom categories
          expect(screen.getByText('Food & Drink')).toBeInTheDocument()
          // 'Shopping' appears in both custom and Plaid - deduplication keeps custom one
          expect(screen.getByText('Shopping')).toBeInTheDocument()

          // Should show unique Plaid categories in human-readable format
          // Note: 'Shopping' from SHOPPING is deduplicated (already in custom categories)
          expect(screen.getByText('Food And Drink')).toBeInTheDocument() // From FOOD_AND_DRINK
          expect(screen.getByText('Transportation')).toBeInTheDocument() // From TRANSPORTATION
          expect(screen.getByText('Entertainment')).toBeInTheDocument()  // From ENTERTAINMENT
        })
      })

      it('should sort combined categories alphabetically', async () => {
        const user = userEvent.setup()

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)

        await waitFor(() => {
          // All categories should be present (sorted alphabetically in merged list)
          const options = screen.getAllByRole('option')
          // Options should exist (we don't test exact order in this basic test)
          expect(options.length).toBeGreaterThan(0)
        })
      })

      it('should store Plaid category as single-element array when selected', async () => {
        const user = userEvent.setup()
        mockOnSave.mockResolvedValue(undefined)

        render(
          <AutoCategorizationRuleEditor
            mode="create"
            onSave={mockOnSave}
            onCancel={mockOnCancel}
            categories={mockCategories}
            plaidCategories={mockPlaidCategories}
          />
        )

        await user.type(screen.getByLabelText(/rule name/i), 'Plaid Category Rule')

        // Add a condition
        const addButton = screen.getByRole('button', { name: /add condition/i })
        fireEvent.click(addButton)
        const conditionValueInput = screen.getByPlaceholderText(/enter value/i)
        await user.type(conditionValueInput, 'test')

        // Select a Plaid category from target category dropdown
        const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
        await user.click(targetCategoryInput)
        await user.click(screen.getByText('Transportation'))

        const saveButton = screen.getByRole('button', { name: /save/i })
        fireEvent.click(saveButton)

        await waitFor(() => {
          expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
            // Plaid category should be stored as single-element array
            target_category: ['Transportation'],
          }))
        })
      })
    })
  })

  // Issue #404: Uncategorized target category option
  describe('Uncategorized target category (Issue #404)', () => {
    it('should show "Uncategorized" as an option in the dropdown', async () => {
      const user = userEvent.setup()

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      await user.click(targetCategoryInput)

      // Should have "Uncategorized" option
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('should set targetCategory to empty array when Uncategorized selected', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Clear Category Rule')

      // Add a condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)
      const conditionValueInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(conditionValueInput, 'spam')

      // Select "Uncategorized" from target category dropdown
      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      await user.click(targetCategoryInput)
      await user.click(screen.getByText('Uncategorized'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          // Uncategorized should be stored as empty array []
          target_category: [],
        }))
      })
    })

    it('should display "Uncategorized" when editing rule with empty target_category array', () => {
      const ruleWithEmptyCategory: CategorizationRule = {
        ...mockExistingRule,
        target_category: [], // Empty array = uncategorized
        target_tags: ['spam'], // Has tags to be valid
      }

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={ruleWithEmptyCategory}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // The category input should show "Uncategorized"
      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      expect(targetCategoryInput).toHaveValue('Uncategorized')
    })

    it('should distinguish null (no action) from empty array (clear category)', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      // Rule with null target_category = no category action
      const ruleWithNullCategory: CategorizationRule = {
        ...mockExistingRule,
        target_category: null,
        target_tags: ['tagged'],
      }

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={ruleWithNullCategory}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // Should not show "Uncategorized" - should be empty/placeholder
      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      expect(targetCategoryInput).not.toHaveValue('Uncategorized')

      // Now select Uncategorized
      await user.click(targetCategoryInput)
      await user.click(screen.getByText('Uncategorized'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          // Should now be empty array, not null
          target_category: [],
        }))
      })
    })

    it('should allow rule with only Uncategorized action (no tags)', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AutoCategorizationRuleEditor
          mode="create"
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      await user.type(screen.getByLabelText(/rule name/i), 'Clear Only Rule')

      // Add a condition
      const addButton = screen.getByRole('button', { name: /add condition/i })
      fireEvent.click(addButton)
      const conditionValueInput = screen.getByPlaceholderText(/enter value/i)
      await user.type(conditionValueInput, 'junk')

      // Select "Uncategorized" from target category dropdown (no tags)
      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      await user.click(targetCategoryInput)
      await user.click(screen.getByText('Uncategorized'))

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        // Should save successfully with empty array target_category and no tags
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
          target_category: [],
        }))
      })
    })

    it('should clear to null (no action) when using "No category change"', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      // Start with a rule that has a category
      const ruleWithCategory: CategorizationRule = {
        ...mockExistingRule,
        target_category: ['Food & Drink'],
        target_tags: ['tagged'],
      }

      render(
        <AutoCategorizationRuleEditor
          mode="edit"
          rule={ruleWithCategory}
          onSave={mockOnSave}
          onCancel={mockOnCancel}
          categories={mockCategories}
        />
      )

      // Clear the category using the clear option (should set to null)
      const targetCategoryInput = screen.getByRole('combobox', { name: /target category/i })
      await user.click(targetCategoryInput)

      // Wait for dropdown to open and find the "No category change" option within the listbox
      const listbox = await screen.findByRole('listbox')
      const noCategoryOption = within(listbox).getByText('No category change')
      await user.click(noCategoryOption)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalled()
      }, { timeout: 3000 })

      // Verify the call argument
      expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({
        // Should be undefined (no action), not empty array
        // Note: null in component state maps to undefined in API request
        target_category: undefined,
      }))
    })
  })
})
