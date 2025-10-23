import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { AccountsPage } from './AccountsPage'
import * as plaidApi from './api/plaidApi'
import type { GetAccountsResponse, Account } from './types'

// Mock the Plaid API
vi.mock('./api/plaidApi')

// Mock PlaidLinkButton component to simplify testing
vi.mock('./PlaidLinkButton', () => ({
  PlaidLinkButton: ({ onSuccess }: { onSuccess?: () => void }) => (
    <button onClick={() => onSuccess?.()} data-testid="plaid-link-button">
      Connect Bank Account
    </button>
  ),
}))

/**
 * Test wrapper component that provides React Router context
 */
function renderWithRouter(ui: React.ReactElement) {
  return render(
    <MemoryRouter>
      {ui}
    </MemoryRouter>
  )
}

describe('AccountsPage', () => {
  const mockAccounts: Account[] = [
    {
      id: '550e8400-e29b-41d4-a716-446655440000',
      linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
      user_id: 'auth0|507f1f77bcf86cd799439011',
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
      created_at: '2025-10-18T14:30:00Z',
      updated_at: '2025-10-20T10:15:00Z',
    },
    {
      id: '660f9511-f3ac-52e5-b827-557766551111',
      linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
      user_id: 'auth0|507f1f77bcf86cd799439011',
      plaid_account_id: 'plaid-acc-2',
      name: 'Wells Fargo Savings',
      type: 'depository',
      subtype: 'savings',
      mask: '1234',
      current_balance: 10000.00,
      available_balance: 10000.00,
      currency_code: 'USD',
      institution_name: 'Wells Fargo',
      created_at: '2025-10-19T09:20:00Z',
      updated_at: '2025-10-20T10:15:00Z',
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should show loading state initially', () => {
    vi.mocked(plaidApi.fetchAccounts).mockImplementation(() => new Promise(() => {}))

    renderWithRouter(<AccountsPage />)

    expect(screen.getByText('Loading your accounts...')).toBeInTheDocument()
  })

  it('should fetch and display accounts on mount', async () => {
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    renderWithRouter(<AccountsPage />)

    // Wait for loading to finish
    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    // Check that accounts are displayed
    expect(screen.getByText('My Accounts')).toBeInTheDocument()
    expect(screen.getByText('2 accounts connected')).toBeInTheDocument()
    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
    expect(screen.getByText('Wells Fargo Savings')).toBeInTheDocument()
  })

  it('should display empty state when no accounts exist', async () => {
    const emptyResponse: GetAccountsResponse = {
      accounts: [],
      total_count: 0,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(emptyResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    expect(screen.getByText(/No accounts connected yet/i)).toBeInTheDocument()
    expect(screen.getByText(/Click "Connect Bank Account" above to get started/i)).toBeInTheDocument()
  })

  it('should display error state when fetch fails', async () => {
    vi.mocked(plaidApi.fetchAccounts).mockRejectedValue(new Error('Network error'))

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    expect(screen.getByText('Failed to load accounts. Please try again.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('should retry loading when Retry button is clicked', async () => {
    const user = userEvent.setup()

    // First call fails
    vi.mocked(plaidApi.fetchAccounts).mockRejectedValueOnce(new Error('Network error'))

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText('Failed to load accounts. Please try again.')).toBeInTheDocument()
    })

    // Second call succeeds
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    // Click retry button
    const retryButton = screen.getByRole('button', { name: 'Retry' })
    await user.click(retryButton)

    // Wait for accounts to load (loading state may resolve too quickly to test)
    await waitFor(() => {
      expect(screen.getByText('2 accounts connected')).toBeInTheDocument()
    })

    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
  })

  it('should reload accounts after successful Plaid Link flow', async () => {
    const user = userEvent.setup()

    // Initial load with no accounts
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValueOnce({
      accounts: [],
      total_count: 0,
    })

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText(/No accounts connected yet/i)).toBeInTheDocument()
    })

    // After PlaidLink success, reload with accounts
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
      accounts: mockAccounts,
      total_count: 2,
    })

    // Simulate PlaidLink success
    const plaidButton = screen.getByTestId('plaid-link-button')
    await user.click(plaidButton)

    // Should reload and show accounts
    await waitFor(() => {
      expect(screen.getByText('2 accounts connected')).toBeInTheDocument()
    })
  })

  it('should display single account correctly', async () => {
    const singleAccountResponse: GetAccountsResponse = {
      accounts: [mockAccounts[0]],
      total_count: 1,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(singleAccountResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    // Singular form
    expect(screen.getByText('1 account connected')).toBeInTheDocument()
  })

  it('should render PlaidLinkButton for adding more accounts', async () => {
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText('2 accounts connected')).toBeInTheDocument()
    })

    // PlaidLinkButton should be visible even with existing accounts
    expect(screen.getByTestId('plaid-link-button')).toBeInTheDocument()
  })
})
