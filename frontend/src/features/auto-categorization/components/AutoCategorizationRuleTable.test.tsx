/**
 * AutoCategorizationRuleTable Component Tests
 * Issue #584: Unified Card/Table View System
 *
 * Tests for the auto-categorization rule table display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AutoCategorizationRuleTable } from './AutoCategorizationRuleTable'
import type { CategorizationRule } from '../types'

const mockRules: CategorizationRule[] = [
  {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 1,
    conditions: [{ type: 'merchant_contains', value: 'coffee' }],
    condition_logic: 'AND',
    target_category: ['Food and Drink', 'Coffee'],
    target_tags: ['daily', 'caffeine'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 42,
    override_count: 5,
    last_matched_at: '2025-01-15T10:30:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:30:00Z',
  },
  {
    id: 'rule-2',
    user_id: 'user-1',
    name: 'Grocery Stores',
    description: null,
    enabled: false,
    priority: 2,
    conditions: [
      { type: 'merchant_contains', value: 'grocery' },
      { type: 'amount_less_than', value: 200 },
    ],
    condition_logic: 'OR',
    target_category: ['Shopping', 'Groceries'],
    target_tags: null,
    tag_behavior: 'add',
    source: 'ai',
    match_count: 15,
    override_count: 0,
    last_matched_at: null,
    created_at: '2025-01-02T00:00:00Z',
    updated_at: '2025-01-02T00:00:00Z',
  },
  {
    id: 'rule-3',
    user_id: 'user-1',
    name: 'Subscriptions',
    enabled: true,
    priority: 3,
    conditions: [{ type: 'amount_equals', value: 9.99 }],
    condition_logic: 'AND',
    target_category: ['Bills', 'Subscription'],
    tag_behavior: 'replace',
    source: 'template',
    match_count: 0,
    override_count: 0,
    created_at: '2025-01-03T00:00:00Z',
    updated_at: '2025-01-03T00:00:00Z',
  },
]

describe('AutoCategorizationRuleTable', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render a table', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
      expect(screen.getByText('Priority')).toBeInTheDocument()
      expect(screen.getByText('Conditions')).toBeInTheDocument()
      expect(screen.getByText('Category')).toBeInTheDocument()
      expect(screen.getByText('Matches')).toBeInTheDocument()
    })

    it('should render all rules', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Grocery Stores')).toBeInTheDocument()
      expect(screen.getByText('Subscriptions')).toBeInTheDocument()
    })

    it('should display rule descriptions when available', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('Categorize coffee purchases')).toBeInTheDocument()
    })

    it('should display enabled/disabled status', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      const enabledBadges = screen.getAllByText('Enabled')
      const disabledBadges = screen.getAllByText('Disabled')
      expect(enabledBadges.length).toBe(2) // rule-1 and rule-3
      expect(disabledBadges.length).toBe(1) // rule-2
    })

    it('should display priority values', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('1')).toBeInTheDocument()
      expect(screen.getByText('2')).toBeInTheDocument()
      expect(screen.getByText('3')).toBeInTheDocument()
    })

    it('should display condition count with logic', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      // Rules 1 and 3: 1 condition each (AND logic, no suffix shown for single)
      const singleConditions = screen.getAllByText('1 condition')
      expect(singleConditions.length).toBe(2)
      // Rule 2: 2 conditions OR
      expect(screen.getByText('2 conditions (OR)')).toBeInTheDocument()
    })

    it('should display target category', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('Food and Drink')).toBeInTheDocument()
      expect(screen.getByText('Shopping')).toBeInTheDocument()
      expect(screen.getByText('Bills')).toBeInTheDocument()
    })

    it('should display match count', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('42')).toBeInTheDocument()
      expect(screen.getByText('15')).toBeInTheDocument()
    })

    it('should show empty state when no rules', () => {
      render(<AutoCategorizationRuleTable rules={[]} />)

      expect(screen.getByText(/no rules found/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} loading />)

      expect(screen.getByRole('table')).toHaveClass('autocat-table--loading')
    })
  })

  describe('Tags Display', () => {
    it('should display target tags when present', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByText('daily')).toBeInTheDocument()
      expect(screen.getByText('caffeine')).toBeInTheDocument()
    })
  })

  describe('Source Badge', () => {
    it('should display source badge for manual rules', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      const manualBadges = screen.getAllByText('Manual')
      expect(manualBadges.length).toBeGreaterThan(0)
    })

    it('should display source badge for AI rules', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      const aiBadges = screen.getAllByText('AI')
      expect(aiBadges.length).toBeGreaterThan(0)
    })

    it('should display source badge for template rules', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      const templateBadges = screen.getAllByText('Template')
      expect(templateBadges.length).toBeGreaterThan(0)
    })
  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn()
      render(<AutoCategorizationRuleTable rules={mockRules} onEdit={onEdit} />)

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockRules[0])
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = vi.fn()
      render(<AutoCategorizationRuleTable rules={mockRules} onDelete={onDelete} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      fireEvent.click(deleteButtons[0])

      expect(onDelete).toHaveBeenCalledWith(mockRules[0])
    })

    it('should call onToggle when toggle switch is clicked', () => {
      const onToggle = vi.fn()
      render(<AutoCategorizationRuleTable rules={mockRules} onToggle={onToggle} />)

      const toggleButtons = screen.getAllByRole('switch')
      fireEvent.click(toggleButtons[0])

      expect(onToggle).toHaveBeenCalledWith(mockRules[0], false) // Toggle from enabled to disabled
    })

    it('should not show actions column when no handlers provided', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onEdit={() => {}} />
      )

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      render(
        <AutoCategorizationRuleTable
          rules={mockRules}
          onEdit={() => {}}
          onDelete={() => {}}
          loading
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })

      editButtons.forEach((btn) => expect(btn).toBeDisabled())
      deleteButtons.forEach((btn) => expect(btn).toBeDisabled())
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = vi.fn()
      render(
        <AutoCategorizationRuleTable rules={mockRules} onRowClick={onRowClick} />
      )

      const rows = screen.getAllByRole('row')
      // Skip header row
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockRules[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onRowClick={() => {}} />
      )

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('autocat-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('should make name column sortable', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} sortable />)

      const nameHeader = screen.getByText('Name')
      expect(nameHeader.closest('button')).toBeInTheDocument()
    })

    it('should call onSort when clicking sortable header', () => {
      const onSort = vi.fn()
      render(
        <AutoCategorizationRuleTable
          rules={mockRules}
          sortable
          onSort={onSort}
        />
      )

      const nameHeader = screen.getByText('Name').closest('button')!
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'asc')
    })

    it('should toggle sort direction on second click', () => {
      const onSort = vi.fn()
      render(
        <AutoCategorizationRuleTable
          rules={mockRules}
          sortable
          onSort={onSort}
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Name').closest('button')!
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'desc')
    })

    it('should show sort indicator on sorted column', () => {
      render(
        <AutoCategorizationRuleTable
          rules={mockRules}
          sortable
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Name').closest('button')!
      expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      const rowgroups = screen.getAllByRole('rowgroup')
      expect(rowgroups.length).toBe(2) // thead and tbody
      expect(screen.getAllByRole('row').length).toBe(4) // 1 header + 3 data rows
    })

    it('should have accessible column headers', () => {
      render(<AutoCategorizationRuleTable rules={mockRules} />)

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
      render(
        <AutoCategorizationRuleTable
          rules={mockRules}
          onEdit={() => {}}
          onDelete={() => {}}
          onToggle={() => {}}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit rule/i })
      const deleteButtons = screen.getAllByRole('button', { name: /delete rule/i })
      const toggleButtons = screen.getAllByRole('switch')

      expect(editButtons.length).toBe(3)
      expect(deleteButtons.length).toBe(3)
      expect(toggleButtons.length).toBe(3)
    })

    it('should have aria-checked on toggle switches', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onToggle={() => {}} />
      )

      const toggleButtons = screen.getAllByRole('switch')
      // First rule is enabled
      expect(toggleButtons[0]).toHaveAttribute('aria-checked', 'true')
      // Second rule is disabled
      expect(toggleButtons[1]).toHaveAttribute('aria-checked', 'false')
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on edit button', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onEdit={vi.fn()} />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit rule/i })
      editButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit rule')
      })
    })

    it('should have tooltip on delete button', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onDelete={vi.fn()} />
      )

      const deleteButtons = screen.getAllByRole('button', { name: /delete rule/i })
      deleteButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Delete rule')
      })
    })

    it('should have tooltip on toggle switch', () => {
      render(
        <AutoCategorizationRuleTable rules={mockRules} onToggle={vi.fn()} />
      )

      const toggleButtons = screen.getAllByRole('switch')
      // Enabled rule should have "Disable rule" tooltip
      expect(toggleButtons[0]).toHaveAttribute('title', 'Disable rule')
      // Disabled rule should have "Enable rule" tooltip
      expect(toggleButtons[1]).toHaveAttribute('title', 'Enable rule')
    })
  })
})
