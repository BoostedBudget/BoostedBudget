import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AutoCategorizationRuleCard } from './AutoCategorizationRuleCard'
import type { CategorizationRule } from '../types'

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

  const mockOnToggle = vi.fn()
  const mockOnEdit = vi.fn()
  const mockOnDelete = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render rule name and description', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Categorize coffee purchases')).toBeInTheDocument()
    })

    it('should show Active status for enabled rule', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should show Disabled status for disabled rule', () => {
      const disabledRule = { ...mockRule, enabled: false }
      render(
        <AutoCategorizationRuleCard
          rule={disabledRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Disabled')).toBeInTheDocument()
    })

    it('should render conditions with labels', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Merchant contains')).toBeInTheDocument()
      expect(screen.getByText('starbucks')).toBeInTheDocument()
      expect(screen.getByText('Amount greater than')).toBeInTheDocument()
      expect(screen.getByText('5')).toBeInTheDocument()
    })

    it('should show condition logic', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Conditions (AND)')).toBeInTheDocument()
    })

    it('should render target category as breadcrumb', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Food & Drink > Coffee')).toBeInTheDocument()
    })

    it('should render target tags', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('morning')).toBeInTheDocument()
      expect(screen.getByText('caffeine')).toBeInTheDocument()
    })

    it('should show tag behavior label', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Add tags:')).toBeInTheDocument()
    })

    it('should show Replace tags label for replace behavior', () => {
      const replaceRule = { ...mockRule, tag_behavior: 'replace' as const }
      render(
        <AutoCategorizationRuleCard
          rule={replaceRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Replace tags:')).toBeInTheDocument()
    })

    it('should render stats', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Matched: 15')).toBeInTheDocument()
      expect(screen.getByText('Overridden: 2')).toBeInTheDocument()
    })

    it('should render priority', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Priority: 10')).toBeInTheDocument()
    })

    it('should not render description if not provided', () => {
      const ruleWithoutDesc = { ...mockRule, description: undefined }
      render(
        <AutoCategorizationRuleCard
          rule={ruleWithoutDesc}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.queryByText('Categorize coffee purchases')).not.toBeInTheDocument()
    })
  })

  describe('Toggle', () => {
    it('should call onToggle with false when disabling', async () => {
      mockOnToggle.mockResolvedValue(undefined)
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const disableButton = screen.getByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      await waitFor(() => {
        expect(mockOnToggle).toHaveBeenCalledWith(false)
      })
    })

    it('should call onToggle with true when enabling', async () => {
      const disabledRule = { ...mockRule, enabled: false }
      mockOnToggle.mockResolvedValue(undefined)
      render(
        <AutoCategorizationRuleCard
          rule={disabledRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

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

      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const disableButton = screen.getByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      expect(screen.getByRole('button', { name: 'Saving...' })).toBeInTheDocument()

      resolveToggle!()
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
      })
    })
  })

  describe('Edit', () => {
    it('should call onEdit when Edit button is clicked', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const editButton = screen.getByRole('button', { name: 'Edit' })
      fireEvent.click(editButton)

      expect(mockOnEdit).toHaveBeenCalledTimes(1)
    })
  })

  describe('Delete', () => {
    it('should show confirmation dialog when Delete is clicked', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      expect(screen.getByText('Are you sure you want to delete "Coffee Shops"?')).toBeInTheDocument()
    })

    it('should hide confirmation dialog when Cancel is clicked', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const cancelButton = screen.getByRole('button', { name: 'Cancel' })
      fireEvent.click(cancelButton)

      expect(screen.queryByText('Are you sure you want to delete "Coffee Shops"?')).not.toBeInTheDocument()
    })

    it('should call onDelete when confirmed', async () => {
      mockOnDelete.mockResolvedValue(undefined)
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const confirmButton = screen.getAllByRole('button', { name: 'Delete' })[1]
      fireEvent.click(confirmButton)

      await waitFor(() => {
        expect(mockOnDelete).toHaveBeenCalledTimes(1)
      })
    })

    it('should show Deleting... while deleting', async () => {
      let resolveDelete: (value?: unknown) => void
      mockOnDelete.mockImplementation(() => new Promise((resolve) => {
        resolveDelete = resolve
      }))

      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const confirmButton = screen.getAllByRole('button', { name: 'Delete' })[1]
      fireEvent.click(confirmButton)

      expect(screen.getByRole('button', { name: 'Deleting...' })).toBeInTheDocument()

      resolveDelete!()
      await waitFor(() => {
        expect(screen.queryByText('Deleting...')).not.toBeInTheDocument()
      })
    })
  })

  describe('Format helpers', () => {
    it('should format array values as comma-separated', () => {
      const ruleWithArrayCondition = {
        ...mockRule,
        conditions: [{ type: 'account_type' as const, value: ['checking', 'savings'] }],
      }
      render(
        <AutoCategorizationRuleCard
          rule={ruleWithArrayCondition}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('checking, savings')).toBeInTheDocument()
    })

    it('should format range objects as min - max', () => {
      const ruleWithRange = {
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { min: 10, max: 100 } }],
      }
      render(
        <AutoCategorizationRuleCard
          rule={ruleWithRange}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('10 - 100')).toBeInTheDocument()
    })

    it('should format date range objects', () => {
      const ruleWithDateRange = {
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { start: '2025-01-01', end: '2025-12-31' } }],
      }
      render(
        <AutoCategorizationRuleCard
          rule={ruleWithDateRange}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('from 2025-01-01 to 2025-12-31')).toBeInTheDocument()
    })
  })
})
