import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AccountCard } from './AccountCard'
import type { Account } from './types'

describe('AccountCard', () => {
  const mockAccount: Account = {
    id: '550e8400-e29b-41d4-a716-446655440000',
    linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
    user_id: 'user_507f1f77bcf86cd799439011',
    plaid_account_id: 'plaid-acc-1',
    name: 'Chase Checking',
    official_name: 'Chase Total Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '0000',
    current_balance: 2500.50,
    available_balance: 2300.50,
    currency_code: 'USD',
    institution_name: 'Chase',
    is_hidden: false,
    status: 'active',
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
  }

  it('should render account information', () => {
    render(<AccountCard account={mockAccount} />)

    // Check institution name
    expect(screen.getByText('Chase')).toBeInTheDocument()

    // Check account name (uses official_name if available)
    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()

    // Check account type
    expect(screen.getByText('Bank Account - Checking')).toBeInTheDocument()

    // Check mask
    expect(screen.getByText('****0000')).toBeInTheDocument()

    // Check current balance
    expect(screen.getByText('$2,500.50')).toBeInTheDocument()

    // Check available balance (should show since it differs from current)
    expect(screen.getByText('$2,300.50')).toBeInTheDocument()
  })

  it('should fallback to name when official_name is not provided', () => {
    const accountWithoutOfficialName: Account = {
      ...mockAccount,
      official_name: undefined,
    }

    render(<AccountCard account={accountWithoutOfficialName} />)

    expect(screen.getByText('Chase Checking')).toBeInTheDocument()
  })

  it('should display "Unknown Bank" when institution_name is missing', () => {
    const accountWithoutInstitution: Account = {
      ...mockAccount,
      institution_name: undefined,
    }

    render(<AccountCard account={accountWithoutInstitution} />)

    expect(screen.getByText('Unknown Bank')).toBeInTheDocument()
  })

  it('should not show mask when not provided', () => {
    const accountWithoutMask: Account = {
      ...mockAccount,
      mask: undefined,
    }

    const { container } = render(<AccountCard account={accountWithoutMask} />)

    expect(container.textContent).not.toContain('****')
  })

  it('should show N/A for undefined balances', () => {
    const accountWithoutBalances: Account = {
      ...mockAccount,
      current_balance: undefined,
      available_balance: undefined,
    }

    render(<AccountCard account={accountWithoutBalances} />)

    const balanceElements = screen.getAllByText('N/A')
    expect(balanceElements.length).toBeGreaterThan(0)
  })

  it('should not show available balance when it equals current balance', () => {
    const accountWithSameBalances: Account = {
      ...mockAccount,
      current_balance: 2500.50,
      available_balance: 2500.50,
    }

    render(<AccountCard account={accountWithSameBalances} />)

    // Only one instance of $2,500.50 should appear (current balance)
    const balances = screen.getAllByText('$2,500.50')
    expect(balances).toHaveLength(1)
  })

  it('should handle credit card accounts', () => {
    const creditCardAccount: Account = {
      ...mockAccount,
      type: 'credit',
      subtype: 'credit card',
      name: 'Chase Freedom',
    }

    render(<AccountCard account={creditCardAccount} />)

    expect(screen.getByText('Credit Card - Credit card')).toBeInTheDocument()
  })

  it('should handle loan accounts', () => {
    const loanAccount: Account = {
      ...mockAccount,
      type: 'loan',
      subtype: 'mortgage',
      name: 'Home Mortgage',
    }

    render(<AccountCard account={loanAccount} />)

    expect(screen.getByText('Loan - Mortgage')).toBeInTheDocument()
  })

  it('should handle investment accounts', () => {
    const investmentAccount: Account = {
      ...mockAccount,
      type: 'investment',
      subtype: '401k',
      name: 'Retirement Account',
    }

    render(<AccountCard account={investmentAccount} />)

    expect(screen.getByText('Investment - 401k')).toBeInTheDocument()
  })

  it('should handle account without subtype', () => {
    const accountWithoutSubtype: Account = {
      ...mockAccount,
      subtype: undefined,
    }

    render(<AccountCard account={accountWithoutSubtype} />)

    expect(screen.getByText('Bank Account')).toBeInTheDocument()
  })

  it('should format non-USD currencies', () => {
    const eurAccount: Account = {
      ...mockAccount,
      currency_code: 'EUR',
      current_balance: 1500.75,
    }

    render(<AccountCard account={eurAccount} />)

    // EUR symbol should be present
    expect(screen.getByText(/€1,500.75/)).toBeInTheDocument()
  })
})
