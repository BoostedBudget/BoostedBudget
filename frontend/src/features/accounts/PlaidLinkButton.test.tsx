import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { usePlaidLink, PlaidLinkOnSuccessMetadata, PlaidLinkOnExit } from 'react-plaid-link'
import { PlaidLinkButton } from './PlaidLinkButton'
import * as plaidApi from './api/plaidApi'
import type { LinkTokenResponse, ExchangePublicTokenResponse } from './types'

// Mock the Plaid API
vi.mock('./api/plaidApi')

// Mock react-plaid-link - use vi.fn() directly in factory to avoid hoisting issues
const mockOpen = vi.fn()
vi.mock('react-plaid-link', () => ({
  usePlaidLink: vi.fn(),
}))

// Mock useNavigate from react-router-dom
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

/**
 * Test wrapper component that provides React Router context
 * Required for components using useNavigate
 */
function renderWithRouter(ui: React.ReactElement) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={ui} />
      </Routes>
    </MemoryRouter>
  )
}

describe('PlaidLinkButton', () => {
  const mockLinkToken: LinkTokenResponse = {
    link_token: 'link-sandbox-abc123-def456',
    expiration: '2025-10-20T22:00:00Z',
    request_id: 'request-xyz789',
  }

  const mockExchangeResponse: ExchangePublicTokenResponse = {
    success: true,
    item_id: 'item-xyz123',
    accounts_linked: 3,
  }

  const mockMetadata: PlaidLinkOnSuccessMetadata = {
    institution: {
      name: 'Chase',
      institution_id: 'ins_109508',
    },
    accounts: [
      {
        id: 'acc-123',
        name: 'Checking',
        mask: '1234',
        type: 'depository',
        subtype: 'checking',
        verification_status: 'verified',
      },
    ],
    link_session_id: 'session-123',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    // Default mock for usePlaidLink - ready state with all required properties
    vi.mocked(usePlaidLink).mockReturnValue({
      open: mockOpen,
      ready: true,
      error: null,
      exit: vi.fn(),
      submit: vi.fn(),
    })
  })

  describe('Rendering and initial state', () => {
    it('should render the connect bank button', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /connect bank account/i })).toBeInTheDocument()
      })
    })

    it('should fetch link token on mount', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(plaidApi.fetchLinkToken).toHaveBeenCalledTimes(1)
      })
    })

    it('should show loading state while fetching link token', () => {
      vi.mocked(plaidApi.fetchLinkToken).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      renderWithRouter(<PlaidLinkButton />)

      expect(screen.getByText(/preparing/i)).toBeInTheDocument()
    })

    it('should show error when link token fetch fails', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockRejectedValue(new Error('Network error'))

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })
    })
  })

  describe('Plaid Link integration', () => {
    it('should initialize Plaid Link with fetched token', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(vi.mocked(usePlaidLink)).toHaveBeenCalledWith(
          expect.objectContaining({
            token: mockLinkToken.link_token,
            onSuccess: expect.any(Function),
            onExit: expect.any(Function),
          })
        )
      })
    })

    it('should open Plaid Link when button is clicked', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)

      renderWithRouter(<PlaidLinkButton />)

      const button = await screen.findByRole('button', { name: /connect bank account/i })
      await userEvent.click(button)

      expect(mockOpen).toHaveBeenCalledTimes(1)
    })

    it('should disable button when Plaid SDK is not ready', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(usePlaidLink).mockReturnValue({
        open: mockOpen,
        ready: false, // SDK not ready
        error: null,
        exit: vi.fn(),
        submit: vi.fn(),
      })

      renderWithRouter(<PlaidLinkButton />)

      const button = await screen.findByRole('button', { name: /connect bank account/i })
      expect(button).toBeDisabled()
    })
  })

  describe('Token exchange flow', () => {
    it('should call exchangePublicToken on Plaid success', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockResolvedValue(mockExchangeResponse)

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(plaidApi.exchangePublicToken).toHaveBeenCalledWith({
          public_token: publicToken,
          institution_id: mockMetadata.institution?.institution_id || '',
          institution_name: mockMetadata.institution?.name || '',
        })
      })
    })

    it('should show loading state during token exchange', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(screen.getByText(/connecting/i)).toBeInTheDocument()
      })
    })

    it('should navigate to /accounts after successful exchange', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockResolvedValue(mockExchangeResponse)

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith('/accounts')
      })
    })
  })

  describe('Error handling', () => {
    it('should show error message on 400 exchange failure', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockRejectedValue({
        response: { status: 400 },
      })

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(screen.getByText(/invalid or expired connection/i)).toBeInTheDocument()
      })
    })

    it('should show subscription limit message on 403 error', async () => {
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockRejectedValue({
        response: { status: 403 },
      })

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(screen.getByText(/account limit reached.*upgrade/i)).toBeInTheDocument()
      })
    })
  })

  describe('Callback props', () => {
    it('should call onSuccess callback when provided', async () => {
      const onSuccess = vi.fn()
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)
      vi.mocked(plaidApi.exchangePublicToken).mockResolvedValue(mockExchangeResponse)

      const publicToken = 'public-sandbox-abc123-def456'
      let capturedOnSuccess: ((public_token: string, metadata: PlaidLinkOnSuccessMetadata) => void) | null = null
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnSuccess = config.onSuccess
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton onSuccess={onSuccess} />)

      await waitFor(() => {
        expect(capturedOnSuccess).toBeDefined()
      })

      // Simulate Plaid Link success
      capturedOnSuccess!(publicToken, mockMetadata)

      await waitFor(() => {
        expect(onSuccess).toHaveBeenCalledWith(mockExchangeResponse)
      })
    })

    it('should call onExit callback when user exits Plaid Link', async () => {
      const onExit = vi.fn()
      vi.mocked(plaidApi.fetchLinkToken).mockResolvedValue(mockLinkToken)

      // Capture the onExit handler for testing
      let capturedOnExit: PlaidLinkOnExit | undefined
      vi.mocked(usePlaidLink).mockImplementation((config) => {
        capturedOnExit = config.onExit
        return { open: mockOpen, ready: true, error: null, exit: vi.fn(), submit: vi.fn() }
      })

      renderWithRouter(<PlaidLinkButton onExit={onExit} />)

      await waitFor(() => {
        expect(capturedOnExit).toBeDefined()
      })

      // Simulate user exiting Plaid Link (provides error and metadata per Plaid API)
      capturedOnExit!(null, { institution: null, status: 'user_exit', link_session_id: 'test-session', request_id: 'test-request' })

      expect(onExit).toHaveBeenCalledTimes(1)
    })
  })
})
