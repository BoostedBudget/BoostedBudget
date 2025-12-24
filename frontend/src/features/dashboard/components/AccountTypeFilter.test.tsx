/**
 * AccountTypeFilter Component Tests
 * Issue #564: Frontend Test Coverage Improvement
 *
 * Tests the account type filter component for dashboard filtering.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AccountTypeFilter } from './AccountTypeFilter'
import type { AccountTypesResponse } from '../types'

// Mock account types data
function createMockAccountTypes(types: Array<{ key: string; label: string; count: number; total_balance: number }> = []): AccountTypesResponse {
  return {
    types: types.length > 0 ? types : [
      { key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 5000.00 },
      { key: 'credit', label: 'Credit Cards', count: 1, total_balance: -2000.00 },
      { key: 'investment', label: 'Investments', count: 3, total_balance: 25000.00 },
    ],
  }
}

describe('AccountTypeFilter', () => {
  const mockOnChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render the component with title', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('Account Types')).toBeInTheDocument()
    })

    it('should render All Types checkbox', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('All Types')).toBeInTheDocument()
    })

    it('should render all account type options', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
      expect(screen.getByText('Investments')).toBeInTheDocument()
    })

    it('should render account counts with plural form', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('2 accounts')).toBeInTheDocument()
      expect(screen.getByText('3 accounts')).toBeInTheDocument()
    })

    it('should render account count with singular form', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('1 account')).toBeInTheDocument()
    })

    it('should render empty state with no types', () => {
      render(
        <AccountTypeFilter
          accountTypes={{ types: [] }}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('Account Types')).toBeInTheDocument()
      expect(screen.getByText('All Types')).toBeInTheDocument()
    })
  })

  describe('Currency Formatting', () => {
    it('should format positive balances correctly', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'depository', label: 'Savings', count: 1, total_balance: 5000.00 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('$5,000')).toBeInTheDocument()
    })

    it('should format negative balances correctly', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'credit', label: 'Credit Card', count: 1, total_balance: -2000.00 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('-$2,000')).toBeInTheDocument()
    })

    it('should format zero balance correctly', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'empty', label: 'Empty Account', count: 1, total_balance: 0 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('$0')).toBeInTheDocument()
    })

    it('should format large balances correctly', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes([
            { key: 'investment', label: 'Investment', count: 1, total_balance: 1234567.89 },
          ])}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('$1,234,568')).toBeInTheDocument()
    })
  })

  describe('Selection State', () => {
    it('should show All Types as checked when selectedTypes is empty', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      const allTypesCheckbox = screen.getAllByRole('checkbox')[0]
      expect(allTypesCheckbox).toBeChecked()
    })

    it('should show All Types as checked when all types are selected', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository', 'credit', 'investment']}
          onChange={mockOnChange}
        />
      )

      const allTypesCheckbox = screen.getAllByRole('checkbox')[0]
      expect(allTypesCheckbox).toBeChecked()
    })

    it('should show All Types as unchecked when some types are selected', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository']}
          onChange={mockOnChange}
        />
      )

      const allTypesCheckbox = screen.getAllByRole('checkbox')[0]
      expect(allTypesCheckbox).not.toBeChecked()
    })

    it('should check individual types when they are selected', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository']}
          onChange={mockOnChange}
        />
      )

      const checkboxes = screen.getAllByRole('checkbox')
      // First checkbox is "All Types", then individual types
      expect(checkboxes[1]).toBeChecked() // depository
      expect(checkboxes[2]).not.toBeChecked() // credit
      expect(checkboxes[3]).not.toBeChecked() // investment
    })

    it('should check all individual types when selectedTypes is empty', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      const checkboxes = screen.getAllByRole('checkbox')
      // When selectedTypes is empty, all types are implicitly selected
      expect(checkboxes[1]).toBeChecked()
      expect(checkboxes[2]).toBeChecked()
      expect(checkboxes[3]).toBeChecked()
    })
  })

  describe('Toggle All Behavior', () => {
    it('should call onChange with empty array when All is clicked while all selected', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      const allTypesCheckbox = screen.getAllByRole('checkbox')[0]
      fireEvent.click(allTypesCheckbox)

      expect(mockOnChange).toHaveBeenCalledWith([])
    })

    it('should call onChange with all types when All is clicked while not all selected', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository']}
          onChange={mockOnChange}
        />
      )

      const allTypesCheckbox = screen.getAllByRole('checkbox')[0]
      fireEvent.click(allTypesCheckbox)

      expect(mockOnChange).toHaveBeenCalledWith(['depository', 'credit', 'investment'])
    })
  })

  describe('Toggle Individual Type', () => {
    it('should add type to selection when unchecked type is clicked', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository']}
          onChange={mockOnChange}
        />
      )

      const creditCheckbox = screen.getAllByRole('checkbox')[2]
      fireEvent.click(creditCheckbox)

      expect(mockOnChange).toHaveBeenCalledWith(['depository', 'credit'])
    })

    it('should remove type from selection when checked type is clicked', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={['depository', 'credit']}
          onChange={mockOnChange}
        />
      )

      const depositoryCheckbox = screen.getAllByRole('checkbox')[1]
      fireEvent.click(depositoryCheckbox)

      expect(mockOnChange).toHaveBeenCalledWith(['credit'])
    })

    it('should handle clicking when all are implicitly selected (empty array)', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      // Click on depository - when selectedTypes is empty, it doesn't include 'depository'
      // so the toggle logic adds it to the array
      const depositoryCheckbox = screen.getAllByRole('checkbox')[1]
      fireEvent.click(depositoryCheckbox)

      // Since selectedTypes is empty and depository is not in the array,
      // clicking adds it to selection
      expect(mockOnChange).toHaveBeenCalledWith(['depository'])
    })
  })

  describe('Accessibility', () => {
    it('should have checkbox role for All Types', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getAllByRole('checkbox').length).toBeGreaterThan(0)
    })

    it('should have label text for each account type', () => {
      render(
        <AccountTypeFilter
          accountTypes={createMockAccountTypes()}
          selectedTypes={[]}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
      expect(screen.getByText('Investments')).toBeInTheDocument()
    })
  })
})
