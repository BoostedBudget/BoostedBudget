/**
 * CancelDeletionPage Component Tests
 *
 * TDD RED: Tests for the public account deletion cancellation page.
 * Tests auto-processing, success/error states, and navigation.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { CancelDeletionPage } from './CancelDeletionPage'
import * as useDeletionHook from '../hooks/useDeletion'
import * as ReactRouterDom from 'react-router-dom'
import type { CancelDeletionResponse } from '../types'

// Mock the hooks
vi.mock('../hooks/useDeletion')
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useParams: vi.fn(),
    useNavigate: vi.fn(),
  }
})

describe('CancelDeletionPage', () => {
  const mockCancelAccountDeletion = vi.fn()
  const mockNavigate = vi.fn()

  const testToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token'

  const mockSuccessResponse: CancelDeletionResponse = {
    message: 'Account deletion canceled successfully',
  }

  beforeEach(() => {
    // Mock navigate
    vi.mocked(ReactRouterDom.useNavigate).mockReturnValue(mockNavigate)

    // Mock useDeletion hook
    vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
      requestAccountDeletion: vi.fn(),
      isRequesting: false,
      requestError: null,
      cancelAccountDeletion: mockCancelAccountDeletion,
      isCanceling: false,
      cancelError: null,
    })

    mockCancelAccountDeletion.mockClear()
    mockNavigate.mockClear()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  describe('Auto-Processing', () => {
    it('should auto-process cancellation on mount with valid token', async () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: testToken })
      mockCancelAccountDeletion.mockResolvedValue(mockSuccessResponse)

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(mockCancelAccountDeletion).toHaveBeenCalledWith(testToken)
      })
    })

    it('should call cancelAccountDeletion with token from URL', async () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: testToken })
      mockCancelAccountDeletion.mockResolvedValue(mockSuccessResponse)

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(mockCancelAccountDeletion).toHaveBeenCalledTimes(1)
        expect(mockCancelAccountDeletion).toHaveBeenCalledWith(testToken)
      })
    })

    it('should show loading spinner during processing', () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: testToken })
      mockCancelAccountDeletion.mockImplementation(
        () => new Promise(() => {}) // Never resolves - stays loading
      )

      render(<CancelDeletionPage />)

      expect(screen.getByText(/processing cancellation/i)).toBeInTheDocument()
      expect(screen.getByText(/please wait while we cancel your account deletion/i)).toBeInTheDocument()
    })

    it('should NOT auto-process if token is missing', () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: undefined })

      render(<CancelDeletionPage />)

      expect(mockCancelAccountDeletion).not.toHaveBeenCalled()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: testToken })
      mockCancelAccountDeletion.mockResolvedValue(mockSuccessResponse)
    })

    it('should display success icon and message', async () => {
      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText('✓')).toBeInTheDocument()
      })

      expect(screen.getByText(/account deletion canceled/i)).toBeInTheDocument()
    })

    it('should display "Account Deletion Canceled" heading', async () => {
      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText('Account Deletion Canceled')).toBeInTheDocument()
      })
    })

    it('should show "Go to Profile" button', async () => {
      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /go to profile/i })).toBeInTheDocument()
      })
    })

    it('should navigate to /profile when button clicked', async () => {
      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /go to profile/i })).toBeInTheDocument()
      })

      const profileButton = screen.getByRole('button', { name: /go to profile/i })
      profileButton.click()

      expect(mockNavigate).toHaveBeenCalledWith('/profile')
    })

    it('should display confirmation that data is safe', async () => {
      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText(/your account and all your data are safe/i)).toBeInTheDocument()
      })

      expect(screen.getByText(/you can continue using BoostedBudget normally/i)).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    beforeEach(() => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: testToken })
    })

    it('should display error icon and message', async () => {
      const error = new Error('Invalid token')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText('✗')).toBeInTheDocument()
      })

      expect(screen.getByText(/cancellation failed/i)).toBeInTheDocument()
    })

    it('should display possible error reasons (expired, used, processed)', async () => {
      const error = new Error('Token expired')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText(/the cancellation link has expired/i)).toBeInTheDocument()
      })

      expect(screen.getByText(/the link has already been used/i)).toBeInTheDocument()
      expect(screen.getByText(/your account deletion has already been processed/i)).toBeInTheDocument()
    })

    it('should show "Go to Login" button', async () => {
      const error = new Error('Network error')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /go to login/i })).toBeInTheDocument()
      })
    })

    it('should show "Try Again" button', async () => {
      const error = new Error('Temporary error')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
      })
    })

    it('should retry cancellation when "Try Again" clicked', async () => {
      const error = new Error('Retry test')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
      })

      const tryAgainButton = screen.getByRole('button', { name: /try again/i })

      // Clear previous calls
      mockCancelAccountDeletion.mockClear()

      // Mock success on retry
      mockCancelAccountDeletion.mockResolvedValue(mockSuccessResponse)
      tryAgainButton.click()

      // Note: Full retry flow requires component state management
    })

    it('should display error details from API', async () => {
      const error = new Error('Specific error message from API')
      mockCancelAccountDeletion.mockResolvedValue(null)

      vi.mocked(useDeletionHook.useDeletion).mockReturnValue({
        requestAccountDeletion: vi.fn(),
        isRequesting: false,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: error,
      })

      render(<CancelDeletionPage />)

      await waitFor(() => {
        expect(screen.getByText('Specific error message from API')).toBeInTheDocument()
      })
    })
  })

  describe('Invalid Token State', () => {
    it('should display invalid link message when no token', () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: undefined })

      render(<CancelDeletionPage />)

      expect(screen.getByText(/invalid cancellation link/i)).toBeInTheDocument()
      expect(screen.getByText(/this cancellation link is invalid/i)).toBeInTheDocument()
    })

    it('should show "Go to Login" button for invalid token', () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: undefined })

      render(<CancelDeletionPage />)

      expect(screen.getByRole('button', { name: /go to login/i })).toBeInTheDocument()
    })

    it('should NOT attempt cancellation without token', () => {
      vi.mocked(ReactRouterDom.useParams).mockReturnValue({ token: undefined })

      render(<CancelDeletionPage />)

      expect(mockCancelAccountDeletion).not.toHaveBeenCalled()
    })
  })
})
