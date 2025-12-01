import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RuleEditor } from './RuleEditor'
import type { CategorizationRule } from '../types'

describe('RuleEditor', () => {
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

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Create mode', () => {
    it('should render empty form for create mode', () => {
      render(
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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

      // Select category
      const categorySelect = screen.getByLabelText(/target category/i)
      await user.selectOptions(categorySelect, 'Food & Drink > Coffee')

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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
        <RuleEditor
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
})
