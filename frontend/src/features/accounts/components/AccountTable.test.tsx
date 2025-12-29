/**
 * AccountTable Component Tests
 * Issue #584: Unified Card/Table View System
 *
 * Tests for the account table display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AccountTable } from './AccountTable'
import type { Account } from '@/api/generated/models'

const mockAccounts: Account[] = [
  {
    id: 'acc-1',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-1',
    name: 'Primary Checking',
    nickname: 'My Checking',
    official_name: 'Chase Total Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 2500.5,
    available_balance: 2400.0,
    currency_code: 'USD',
    institution_name: 'Chase',
    custom_color: '#3B82F6',
    custom_emoji: '💰',
    is_hidden: false,
    display_order: 0,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'acc-2',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-2',
    name: 'Savings Account',
    type: 'depository',
    subtype: 'savings',
    mask: '5678',
    current_balance: 15000.0,
    currency_code: 'USD',
    institution_name: 'Chase',
    is_hidden: false,
    display_order: 1,
    created_at: '2024-01-02T00:00:00Z',
    updated_at: '2024-01-02T00:00:00Z',
  },
  {
    id: 'acc-3',
    user_id: 'user-1',
    linked_item_id: 'item-2',
    plaid_account_id: 'plaid-3',
    name: 'Credit Card',
    type: 'credit',
    subtype: 'credit card',
    mask: '9012',
    current_balance: -1250.75,
    currency_code: 'USD',
    institution_name: 'American Express',
    is_hidden: true,
    display_order: 2,
    created_at: '2024-01-03T00:00:00Z',
    updated_at: '2024-01-03T00:00:00Z',
  },
]

describe('AccountTable', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render a table', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('Institution')).toBeInTheDocument()
      expect(screen.getByText('Account')).toBeInTheDocument()
      expect(screen.getByText('Type')).toBeInTheDocument()
      expect(screen.getByText('Mask')).toBeInTheDocument()
      expect(screen.getByText('Balance')).toBeInTheDocument()
    })

    it('should render all accounts', () => {
      render(<AccountTable accounts={mockAccounts} />)

      // Chase appears twice (for two accounts)
      const chaseLabels = screen.getAllByText('Chase')
      expect(chaseLabels.length).toBe(2)
      expect(screen.getByText('American Express')).toBeInTheDocument()
    })

    it('should display nickname when available, otherwise account name', () => {
      render(<AccountTable accounts={mockAccounts} />)

      // acc-1 has nickname "My Checking"
      expect(screen.getByText('My Checking')).toBeInTheDocument()
      // acc-2 has no nickname, should show account name
      expect(screen.getByText('Savings Account')).toBeInTheDocument()
    })

    it('should display account type with human-readable label', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('Checking')).toBeInTheDocument()
      expect(screen.getByText('Savings')).toBeInTheDocument()
      // Note: "Credit Card" is the label for subtype "credit card"
      const creditCardLabels = screen.getAllByText('Credit Card')
      expect(creditCardLabels.length).toBeGreaterThan(0)
    })

    it('should display masked account number', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('••••1234')).toBeInTheDocument()
      expect(screen.getByText('••••5678')).toBeInTheDocument()
      expect(screen.getByText('••••9012')).toBeInTheDocument()
    })

    it('should display formatted balance', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('$2,500.50')).toBeInTheDocument()
      expect(screen.getByText('$15,000.00')).toBeInTheDocument()
    })

    it('should show hidden badge for hidden accounts', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const hiddenBadges = screen.getAllByText('Hidden')
      expect(hiddenBadges.length).toBe(1) // Only acc-3 is hidden
    })

    it('should display custom emoji when available', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('💰')).toBeInTheDocument()
    })

    it('should show empty state when no accounts', () => {
      render(<AccountTable accounts={[]} />)

      expect(screen.getByText(/no accounts found/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      render(<AccountTable accounts={mockAccounts} loading />)

      expect(screen.getByRole('table')).toHaveClass('account-table--loading')
    })
  })

  describe('Balance Styling', () => {
    it('should apply positive class to positive balances', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const positiveBalance = screen.getByText('$2,500.50')
      expect(positiveBalance).toHaveClass('account-table__balance--positive')
    })

    it('should apply negative class to negative balances', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const negativeBalance = screen.getByText('-$1,250.75')
      expect(negativeBalance).toHaveClass('account-table__balance--negative')
    })
  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn()
      render(<AccountTable accounts={mockAccounts} onEdit={onEdit} />)

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = vi.fn()
      render(<AccountTable accounts={mockAccounts} onDelete={onDelete} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      fireEvent.click(deleteButtons[0])

      expect(onDelete).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should not show actions column when no handlers provided', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(<AccountTable accounts={mockAccounts} onEdit={() => {}} />)

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
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
      render(<AccountTable accounts={mockAccounts} onRowClick={onRowClick} />)

      const rows = screen.getAllByRole('row')
      // Skip header row
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(<AccountTable accounts={mockAccounts} onRowClick={() => {}} />)

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('account-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('should make name column sortable', () => {
      render(<AccountTable accounts={mockAccounts} sortable />)

      const nameHeader = screen.getByText('Account')
      expect(nameHeader.closest('button')).toBeInTheDocument()
    })

    it('should call onSort when clicking sortable header', () => {
      const onSort = vi.fn()
      render(<AccountTable accounts={mockAccounts} sortable onSort={onSort} />)

      const nameHeader = screen.getByText('Account').closest('button')!
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'asc')
    })

    it('should toggle sort direction on second click', () => {
      const onSort = vi.fn()
      render(
        <AccountTable
          accounts={mockAccounts}
          sortable
          onSort={onSort}
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Account').closest('button')!
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'desc')
    })

    it('should show sort indicator on sorted column', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
          sortable
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Account').closest('button')!
      expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      const rowgroups = screen.getAllByRole('rowgroup')
      expect(rowgroups.length).toBe(2) // thead and tbody
      expect(screen.getAllByRole('row').length).toBe(4) // 1 header + 3 data rows
    })

    it('should have accessible column headers', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
          onEdit={() => {}}
          onDelete={() => {}}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit account/i })
      const deleteButtons = screen.getAllByRole('button', { name: /delete account/i })

      expect(editButtons.length).toBe(3)
      expect(deleteButtons.length).toBe(3)
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on edit button', () => {
      render(<AccountTable accounts={mockAccounts} onEdit={vi.fn()} />)

      const editButtons = screen.getAllByRole('button', { name: /edit account/i })
      editButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit account')
      })
    })

    it('should have tooltip on delete button', () => {
      render(<AccountTable accounts={mockAccounts} onDelete={vi.fn()} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete account/i })
      deleteButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Delete account')
      })
    })
  })
})
