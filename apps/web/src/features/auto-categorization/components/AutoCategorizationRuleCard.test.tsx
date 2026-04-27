import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react'
import { AutoCategorizationRuleCard } from './AutoCategorizationRuleCard'
import type { CategorizationRule } from '../types'

// Global mocks for assertions
const mockOnToggle = mock(() => Promise.resolve())
const mockOnEdit = mock(() => {})
const mockOnDelete = mock(() => Promise.resolve())

describe('AutoCategorizationRuleCard', () => {
  const mockRule: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [
      { type: 'merchant_contains', value: 'starbucks' },
      { type: 'amount_greater_than', value: 5 },
    ],
    condition_logic: 'AND',
    target_category: ['Food & Drink', 'Coffee'],
    target_tags: ['morning', 'caffeine'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 15,
    override_count: 2,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  beforeEach(() => {
    mockOnToggle.mockClear()
    mockOnEdit.mockClear()
    mockOnDelete.mockClear()
  })

  const renderCard = (rule = mockRule, handlers = {}) =>
    render(
      <AutoCategorizationRuleCard
        rule={rule}
        onToggle={handlers.onToggle ?? mockOnToggle}
        onEdit={handlers.onEdit ?? mockOnEdit}
        onDelete={handlers.onDelete ?? mockOnDelete}
      />
    )

  describe('Rendering', () => {
    it('should render rule name and description', () => {
      renderCard()

      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Categorize coffee purchases')).toBeInTheDocument()
    })

    it('should show Active status for enabled rule', () => {
      renderCard()

      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should show Disabled status for disabled rule', () => {
      renderCard({ ...mockRule, enabled: false })

      expect(screen.getByText('Disabled')).toBeInTheDocument()
    })

    it('should render conditions with labels', () => {
      renderCard()

      expect(screen.getByText('Merchant contains')).toBeInTheDocument()
      expect(screen.getByText('starbucks')).toBeInTheDocument()
      expect(screen.getByText('Amount greater than')).toBeInTheDocument()
      expect(screen.getByText('5')).toBeInTheDocument()
    })

    it('should show condition logic', () => {
      renderCard()

      expect(screen.getByText('Conditions (AND)')).toBeInTheDocument()
    })

    it('should render target category as breadcrumb', () => {
      renderCard()

      expect(screen.getByText('Food & Drink > Coffee')).toBeInTheDocument()
    })

    it('should render target tags', () => {
      renderCard()

      expect(screen.getByText('morning')).toBeInTheDocument()
      expect(screen.getByText('caffeine')).toBeInTheDocument()
    })

    it('should show tag behavior label', () => {
      renderCard()

      expect(screen.getByText('Add tags:')).toBeInTheDocument()
    })

    it('should show Replace tags label for replace behavior', () => {
      renderCard({ ...mockRule, tag_behavior: 'replace' as const })

      expect(screen.getByText('Replace tags:')).toBeInTheDocument()
    })

    it('should render stats', () => {
      renderCard()

      expect(screen.getByText('Matched: 15')).toBeInTheDocument()
      expect(screen.getByText('Overridden: 2')).toBeInTheDocument()
    })

    it('should render priority', () => {
      renderCard()

      expect(screen.getByText('Priority: 10')).toBeInTheDocument()
    })

    it('should not render description if not provided', () => {
      renderCard({ ...mockRule, description: undefined })

      expect(screen.queryByText('Categorize coffee purchases')).not.toBeInTheDocument()
    })
  })

  describe('Toggle', () => {
    it('should call onToggle with false when disabling', async () => {
      mockOnToggle.mockResolvedValue(undefined)
      renderCard()

      const disableButton = screen.getByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      await waitFor(() => {
        expect(mockOnToggle).toHaveBeenCalledWith(false)
      })
    })

    it('should call onToggle with true when enabling', async () => {
      mockOnToggle.mockResolvedValue(undefined)
      renderCard({ ...mockRule, enabled: false })

      const enableButton = screen.getByRole('button', { name: 'Enable' })
      fireEvent.click(enableButton)

      await waitFor(() => {
        expect(mockOnToggle).toHaveBeenCalledWith(true)
      })
    })

    it('should show Saving... while toggling', async () => {
      let resolveToggle: (value?: unknown) => void
      mockOnToggle.mockImplementation(() => new Promise((resolve) => {
        resolveToggle = resolve
      }))

      renderCard()

      // Use findByRole to wait for button to appear in CI
      const disableButton = await screen.findByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      expect(screen.getByRole('button', { name: 'Saving...' })).toBeInTheDocument()

      // Wrap promise resolution in act() to ensure React processes the state update
      await act(async () => {
        resolveToggle!()
      })
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
      })
    })
  })

  describe('Edit', () => {
    it('should call onEdit when Edit button is clicked', () => {
      renderCard()

      const editButton = screen.getByRole('button', { name: 'Edit' })
      fireEvent.click(editButton)

      expect(mockOnEdit).toHaveBeenCalledTimes(1)
    })
  })

  describe('Delete', () => {
    it('should show confirmation dialog when Delete is clicked', () => {
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      expect(screen.getByText('Are you sure you want to delete "Coffee Shops"?')).toBeInTheDocument()
    })

    it('should hide confirmation dialog when Cancel is clicked', () => {
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const cancelButton = screen.getByRole('button', { name: 'Cancel' })
      fireEvent.click(cancelButton)

      expect(screen.queryByText('Are you sure you want to delete "Coffee Shops"?')).not.toBeInTheDocument()
    })

    it('should call onDelete when confirmed', async () => {
      mockOnDelete.mockResolvedValue(undefined)
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      const confirmButton = within(dialog).getByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButton)

      await waitFor(() => {
        expect(mockOnDelete).toHaveBeenCalledTimes(1)
      })
    })

    it('should disable confirm button while deleting', async () => {
      let resolveDelete: (value?: unknown) => void
      mockOnDelete.mockImplementation(() => new Promise((resolve) => {
        resolveDelete = resolve
      }))

      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      const confirmButton = within(dialog).getByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButton)

      // While deleting, the confirm button should be disabled
      expect(confirmButton).toBeDisabled()

      // Wrap promise resolution in act() to ensure React processes the state update
      await act(async () => {
        resolveDelete!()
      })
      await waitFor(() => {
        // Dialog closes after delete completes
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      })
    })
  })

  describe('Format helpers', () => {
    it('should format array values as comma-separated', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_type' as const, value: ['checking', 'savings'] }],
      })

      expect(screen.getByText('checking, savings')).toBeInTheDocument()
    })

    it('should format range objects as min - max', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { min: 10, max: 100 } }],
      })

      expect(screen.getByText('10 - 100')).toBeInTheDocument()
    })

    it('should format date range objects', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { start: '2025-01-01', end: '2025-12-31' } }],
      })

      expect(screen.getByText('from 2025-01-01 to 2025-12-31')).toBeInTheDocument()
    })
  })

  describe('All condition types display', () => {
    // Tests for all 13 condition types to ensure they display correctly

    it('should display merchant_equals condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_equals' as const, value: 'Amazon.com' }],
      })

      expect(screen.getByText('Merchant equals')).toBeInTheDocument()
      expect(screen.getByText('Amazon.com')).toBeInTheDocument()
    })

    it('should display merchant_starts_with condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_starts_with' as const, value: 'AMAZON' }],
      })

      expect(screen.getByText('Merchant starts with')).toBeInTheDocument()
      expect(screen.getByText('AMAZON')).toBeInTheDocument()
    })

    it('should display merchant_ends_with condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_ends_with' as const, value: 'CAFE' }],
      })

      expect(screen.getByText('Merchant ends with')).toBeInTheDocument()
      expect(screen.getByText('CAFE')).toBeInTheDocument()
    })

    it('should display amount_less_than condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_less_than' as const, value: 50 }],
      })

      expect(screen.getByText('Amount less than')).toBeInTheDocument()
      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should display amount_equals condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_equals' as const, value: 99.99 }],
      })

      expect(screen.getByText('Amount equals')).toBeInTheDocument()
      expect(screen.getByText('99.99')).toBeInTheDocument()
    })

    it('should display account_id condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_id' as const, value: 'acc-12345' }],
      })

      expect(screen.getByText('Account is')).toBeInTheDocument()
      expect(screen.getByText('acc-12345')).toBeInTheDocument()
    })

    it('should display payment_channel condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'payment_channel' as const, value: 'online' }],
      })

      expect(screen.getByText('Payment channel is')).toBeInTheDocument()
      expect(screen.getByText('online')).toBeInTheDocument()
    })

    it('should display existing_category condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_category' as const, value: 'Food & Drink' }],
      })

      expect(screen.getByText('Existing Category (legacy text — matches transaction.Category[0])')).toBeInTheDocument()
      expect(screen.getByText('Food & Drink')).toBeInTheDocument()
    })

    it('formatConditionValue_renders_pfc_primary_with_curated_display', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_primary' as const, value: 'FOOD_AND_DRINK' }],
      })

      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('formatConditionValue_renders_pfc_detailed_with_breadcrumb', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_detailed' as const, value: 'FOOD_AND_DRINK_GROCERIES' }],
      })

      expect(screen.getByText('Food And Drink › Groceries')).toBeInTheDocument()
    })

    it('should display date_range with only start date', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { start: '2025-01-01' } }],
      })

      expect(screen.getByText('Date between')).toBeInTheDocument()
      expect(screen.getByText('from 2025-01-01')).toBeInTheDocument()
    })

    it('should display date_range with only end date', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { end: '2025-12-31' } }],
      })

      expect(screen.getByText('Date between')).toBeInTheDocument()
      expect(screen.getByText('to 2025-12-31')).toBeInTheDocument()
    })
  })

  describe('Value formatting edge cases', () => {
    it('should handle empty string condition value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_contains' as const, value: '' }],
      })

      expect(screen.getByText('Merchant contains')).toBeInTheDocument()
      // Value container should exist but be empty or show placeholder
      const conditions = screen.getByText('Conditions (AND)').parentElement?.parentElement
      expect(conditions).toBeInTheDocument()
    })

    it('should handle empty array for account_type', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_type' as const, value: [] }],
      })

      expect(screen.getByText('Account type is')).toBeInTheDocument()
    })

    it('should handle amount_range with only min', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { min: 10 } }],
      })

      expect(screen.getByText('Amount between')).toBeInTheDocument()
    })

    it('should handle amount_range with only max', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { max: 100 } }],
      })

      expect(screen.getByText('Amount between')).toBeInTheDocument()
    })

    it('should handle zero amount value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_equals' as const, value: 0 }],
      })

      expect(screen.getByText('Amount equals')).toBeInTheDocument()
      expect(screen.getByText('0')).toBeInTheDocument()
    })

    it('should handle negative amount value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_greater_than' as const, value: -50 }],
      })

      expect(screen.getByText('Amount greater than')).toBeInTheDocument()
      expect(screen.getByText('-50')).toBeInTheDocument()
    })

    it('should handle multiple conditions with OR logic', () => {
      renderCard({
        ...mockRule,
        condition_logic: 'OR' as const,
        conditions: [
          { type: 'merchant_contains' as const, value: 'coffee' },
          { type: 'merchant_contains' as const, value: 'cafe' },
        ],
      })

      expect(screen.getByText('Conditions (OR)')).toBeInTheDocument()
    })
  })
})
