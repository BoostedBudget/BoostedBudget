import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AccountTypeFilter } from './AccountTypeFilter'
import type { AccountTypesResponse } from '../types'

function createMockAccountTypes(types: Array<{ key: string; label: string; count: number; total_balance: number }> = []): AccountTypesResponse {
  return {
    types: types.length > 0 ? types : [
      { key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 5000.00 },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.00 },
      { key: 'investment', label: 'Investments', count: 3, total_balance: 25000.00 },
    ],
  }
}

const mockOnChange = mock(() => {})

describe('AccountTypeFilter', () => {
  beforeEach(() => {
    mockOnChange.mockClear()
  })

  const renderComponent = (selectedTypes: string[] = []) =>
    render(
      <AccountTypeFilter
        accountTypes={createMockAccountTypes()}
        selectedTypes={selectedTypes}
        onChange={mockOnChange}
      />
    )

  const openDropdown = async () => {
    const user = userEvent.setup()
    await user.click(screen.getByRole('button'))
  }

  describe('Trigger Button', () => {
    it('should show "All Account Types" when no filter selected', () => {
      renderComponent([])
      expect(screen.getByText('All Account Types')).toBeInTheDocument()
    })

    it('should show "All Account Types" when all types selected', () => {
      renderComponent(['depository', 'credit', 'investment'])
      expect(screen.getByText('All Account Types')).toBeInTheDocument()
    })

    it('should show "N of M types" when partial selection', () => {
      renderComponent(['depository'])
      expect(screen.getByText('1 of 3 types')).toBeInTheDocument()
    })

    it('should show correct count for multiple partial selection', () => {
      renderComponent(['depository', 'credit'])
      expect(screen.getByText('2 of 3 types')).toBeInTheDocument()
    })
  })

  describe('Dropdown Content', () => {
    it('should show All Types and account type options when opened', async () => {
      renderComponent()
      await openDropdown()

      expect(screen.getByText('All Types')).toBeInTheDocument()
      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
      expect(screen.getByText('Investments')).toBeInTheDocument()
    })

    it('should show account counts when opened', async () => {
      renderComponent()
      await openDropdown()

      expect(screen.getByText('2 accounts')).toBeInTheDocument()
      expect(screen.getByText('3 accounts')).toBeInTheDocument()
      expect(screen.getByText('1 account')).toBeInTheDocument()
    })

    it('should render with empty account types', () => {
      render(
        <AccountTypeFilter
          accountTypes={{ types: [] }}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )
      expect(screen.getByText('All Account Types')).toBeInTheDocument()
    })
  })

  describe('Currency Formatting', () => {
    it('should format positive balances correctly', async () => {
      const user = userEvent.setup()
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'depository', label: 'Savings', count: 1, total_balance: 5000.00 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )
      await user.click(screen.getByRole('button'))
      expect(screen.getByText('$5,000')).toBeInTheDocument()
    })

    it('should format negative balances correctly', async () => {
      const user = userEvent.setup()
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'credit', label: 'Credit Card', count: 1, total_balance: -2000.00 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )
      await user.click(screen.getByRole('button'))
      expect(screen.getByText('-$2,000')).toBeInTheDocument()
    })

    it('should format zero balance correctly', async () => {
      const user = userEvent.setup()
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'empty', label: 'Empty Account', count: 1, total_balance: 0 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )
      await user.click(screen.getByRole('button'))
      expect(screen.getByText('$0')).toBeInTheDocument()
    })

    it('should format large balances correctly', async () => {
      const user = userEvent.setup()
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'investment', label: 'Investment', count: 1, total_balance: 1234567.89 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )
      await user.click(screen.getByRole('button'))
      expect(screen.getByText('$1,234,568')).toBeInTheDocument()
    })
  })

  describe('Selection State', () => {
    it('should show All Types as checked when selectedTypes is empty', async () => {
      renderComponent([])
      await openDropdown()
      const allTypesItem = screen.getByRole('menuitemcheckbox', { name: /All Types/ })
      expect(allTypesItem).toHaveAttribute('data-state', 'checked')
    })

    it('should show All Types as checked when all types are selected', async () => {
      renderComponent(['depository', 'credit', 'investment'])
      await openDropdown()
      const allTypesItem = screen.getByRole('menuitemcheckbox', { name: /All Types/ })
      expect(allTypesItem).toHaveAttribute('data-state', 'checked')
    })

    it('should show All Types as unchecked when some types are selected', async () => {
      renderComponent(['depository'])
      await openDropdown()
      const allTypesItem = screen.getByRole('menuitemcheckbox', { name: /All Types/ })
      expect(allTypesItem).toHaveAttribute('data-state', 'unchecked')
    })

    it('should check individual type when it is selected', async () => {
      renderComponent(['depository'])
      await openDropdown()
      const depositoryItem = screen.getByRole('menuitemcheckbox', { name: /Checking & Savings/ })
      const creditItem = screen.getByRole('menuitemcheckbox', { name: /Credit Cards/ })
      expect(depositoryItem).toHaveAttribute('data-state', 'checked')
      expect(creditItem).toHaveAttribute('data-state', 'unchecked')
    })

    it('should check all individual types when selectedTypes is empty', async () => {
      renderComponent([])
      await openDropdown()
      const items = screen.getAllByRole('menuitemcheckbox')
      // All Types + 3 individual types = 4 items, all checked
      items.forEach(item => expect(item).toHaveAttribute('data-state', 'checked'))
    })
  })

  describe('Toggle All Behavior', () => {
    it('should call onChange with empty array when All is clicked while all selected', async () => {
      const user = userEvent.setup()
      renderComponent([])
      await user.click(screen.getByRole('button'))
      await user.click(screen.getByRole('menuitemcheckbox', { name: /All Types/ }))
      expect(mockOnChange).toHaveBeenCalledWith([])
    })

    it('should call onChange with all types when All is clicked while not all selected', async () => {
      const user = userEvent.setup()
      renderComponent(['depository'])
      await user.click(screen.getByRole('button'))
      await user.click(screen.getByRole('menuitemcheckbox', { name: /All Types/ }))
      expect(mockOnChange).toHaveBeenCalledWith(['depository', 'credit', 'investment'])
    })
  })

  describe('Toggle Individual Type', () => {
    it('should add type to selection when unchecked type is clicked', async () => {
      const user = userEvent.setup()
      renderComponent(['depository'])
      await user.click(screen.getByRole('button'))
      await user.click(screen.getByRole('menuitemcheckbox', { name: /Credit Cards/ }))
      expect(mockOnChange).toHaveBeenCalledWith(['depository', 'credit'])
    })

    it('should remove type from selection when checked type is clicked', async () => {
      const user = userEvent.setup()
      renderComponent(['depository', 'credit'])
      await user.click(screen.getByRole('button'))
      await user.click(screen.getByRole('menuitemcheckbox', { name: /Checking & Savings/ }))
      expect(mockOnChange).toHaveBeenCalledWith(['credit'])
    })

    it('should handle clicking when all are implicitly selected (empty array)', async () => {
      const user = userEvent.setup()
      renderComponent([])
      await user.click(screen.getByRole('button'))
      await user.click(screen.getByRole('menuitemcheckbox', { name: /Checking & Savings/ }))
      expect(mockOnChange).toHaveBeenCalledWith(['depository'])
    })
  })

  describe('Accessibility', () => {
    it('should have label text for each account type in dropdown', async () => {
      renderComponent()
      await openDropdown()
      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
      expect(screen.getByText('Investments')).toBeInTheDocument()
    })

    it('should have menuitemcheckbox role for items in dropdown', async () => {
      renderComponent()
      await openDropdown()
      expect(screen.getAllByRole('menuitemcheckbox').length).toBeGreaterThan(0)
    })
  })
})
