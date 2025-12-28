/**
 * TransferActionForm Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the account transfer action configuration form.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransferActionForm } from './TransferActionForm'
import type { TransferAction } from '../../types'

// Mock accounts
const mockAccounts = [
  {
    id: 'acc-1',
    name: 'Primary Checking',
    official_name: 'Plaid Checking',
    institution_name: 'Chase',
    type: 'depository',
    subtype: 'checking',
  },
  {
    id: 'acc-2',
    name: 'Savings Account',
    official_name: 'High Yield Savings',
    institution_name: 'Ally Bank',
    type: 'depository',
    subtype: 'savings',
  },
  {
    id: 'acc-3',
    name: 'Credit Card',
    official_name: 'Platinum Rewards',
    institution_name: 'Capital One',
    type: 'credit',
    subtype: 'credit card',
  },
]

vi.mock('@/api/generated/endpoints/accounts/accounts', () => ({
  useGetAccounts: () => ({
    data: { accounts: mockAccounts },
    isLoading: false,
    error: null,
  }),
}))

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = createQueryClient()
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

const defaultAction: TransferAction = {
  from_account_id: 'acc-1',
  to_account_id: 'acc-2',
  amount_type: 'fixed',
  amount: 100,
}

describe('TransferActionForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render from account selector', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
    })

    it('should render to account selector', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/to account/i)).toBeInTheDocument()
    })

    it('should render amount type selector', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/amount type/i)).toBeInTheDocument()
    })

    it('should show all amount type options', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/amount type/i)
      expect(select).toContainHTML('Fixed')
      expect(select).toContainHTML('Percentage')
      expect(select).toContainHTML('Excess')
    })

    it('should show all accounts in from dropdown', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/from account/i)
      expect(select).toContainHTML('Primary Checking')
      expect(select).toContainHTML('Savings Account')
      expect(select).toContainHTML('Credit Card')
    })

    it('should show all accounts in to dropdown', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      const select = screen.getByLabelText(/to account/i)
      expect(select).toContainHTML('Primary Checking')
      expect(select).toContainHTML('Savings Account')
      expect(select).toContainHTML('Credit Card')
    })
  })

  describe('Account Selection', () => {
    it('should call onChange when from account is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/from account/i), {
        target: { value: 'acc-3' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          from_account_id: 'acc-3',
        })
      )
    })

    it('should call onChange when to account is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/to account/i), {
        target: { value: 'acc-3' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          to_account_id: 'acc-3',
        })
      )
    })

    it('should display selected from account', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/from account/i)).toHaveValue('acc-1')
    })

    it('should display selected to account', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/to account/i)).toHaveValue('acc-2')
    })
  })

  describe('Amount Type: Fixed', () => {
    it('should show amount input when fixed type is selected', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/^amount$/i)).toBeInTheDocument()
    })

    it('should display amount value', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/^amount$/i)).toHaveValue(100)
    })

    it('should call onChange when amount is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/^amount$/i), {
        target: { value: '250' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          amount: 250,
        })
      )
    })

    it('should show dollar sign for fixed amount', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText('$')).toBeInTheDocument()
    })

    it('should not show threshold input for fixed type', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.queryByLabelText(/threshold/i)).not.toBeInTheDocument()
    })
  })

  describe('Amount Type: Percentage', () => {
    const percentageAction: TransferAction = {
      ...defaultAction,
      amount_type: 'percentage',
      amount: 50,
    }

    it('should show percentage input when percentage type is selected', () => {
      renderWithProviders(
        <TransferActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/^amount$/i)).toBeInTheDocument()
    })

    it('should show percentage symbol', () => {
      renderWithProviders(
        <TransferActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByText('%')).toBeInTheDocument()
    })

    it('should validate percentage between 0 and 100', () => {
      renderWithProviders(
        <TransferActionForm action={percentageAction} onChange={() => {}} />
      )

      const input = screen.getByLabelText(/^amount$/i)
      expect(input).toHaveAttribute('min', '0')
      expect(input).toHaveAttribute('max', '100')
    })
  })

  describe('Amount Type: Excess', () => {
    const excessAction: TransferAction = {
      ...defaultAction,
      amount_type: 'excess',
      amount: 0,
      threshold: 1000,
    }

    it('should show threshold input when excess type is selected', () => {
      renderWithProviders(
        <TransferActionForm action={excessAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/threshold/i)).toBeInTheDocument()
    })

    it('should display threshold value', () => {
      renderWithProviders(
        <TransferActionForm action={excessAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/threshold/i)).toHaveValue(1000)
    })

    it('should call onChange when threshold is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <TransferActionForm action={excessAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/threshold/i), {
        target: { value: '2000' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          threshold: 2000,
        })
      )
    })

    it('should show description for excess type', () => {
      renderWithProviders(
        <TransferActionForm action={excessAction} onChange={() => {}} />
      )

      // Get all matching elements and check at least one exists
      const descriptions = screen.getAllByText(/transfer any amount above/i)
      expect(descriptions.length).toBeGreaterThan(0)
    })
  })

  describe('Amount Type Switching', () => {
    it('should call onChange when amount type is changed', () => {
      const onChange = vi.fn()

      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={onChange} />
      )

      fireEvent.change(screen.getByLabelText(/amount type/i), {
        target: { value: 'percentage' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          amount_type: 'percentage',
        })
      )
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} disabled />
      )

      expect(screen.getByLabelText(/from account/i)).toBeDisabled()
      expect(screen.getByLabelText(/to account/i)).toBeDisabled()
      expect(screen.getByLabelText(/amount type/i)).toBeDisabled()
      expect(screen.getByLabelText(/^amount$/i)).toBeDisabled()
    })
  })

  describe('Description', () => {
    it('should show description for fixed amount type', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer a specific dollar amount/i)).toBeInTheDocument()
    })

    it('should show description for percentage amount type', () => {
      const percentageAction: TransferAction = {
        ...defaultAction,
        amount_type: 'percentage',
        amount: 50,
      }

      renderWithProviders(
        <TransferActionForm action={percentageAction} onChange={() => {}} />
      )

      expect(screen.getByText(/transfer a percentage of the balance/i)).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      renderWithProviders(
        <TransferActionForm action={defaultAction} onChange={() => {}} />
      )

      expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/to account/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/amount type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/^amount$/i)).toBeInTheDocument()
    })
  })
})
