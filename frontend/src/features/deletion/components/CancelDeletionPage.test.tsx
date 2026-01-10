/**
 * CancelDeletionPage Component Tests
 *
 * Tests for the public account deletion cancellation page.
 * Tests auto-processing, success/error states, and navigation.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 * Issue #575: Migrated to bun:test with centralized API mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CancelDeletionPage } from './CancelDeletionPage'
import { mockCancelDeletion } from '@/test/mocks/user-api'

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })

function renderWithProviders(token?: string) {
  const queryClient = createTestQueryClient()
  const path = token ? `/cancel-deletion/${token}` : '/cancel-deletion/'
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/cancel-deletion/:token?" element={<CancelDeletionPage />} />
          <Route path="/profile" element={<div>Profile Page</div>} />
          <Route path="/login" element={<div>Login Page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('CancelDeletionPage', () => {
  const testToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token'

  beforeEach(() => {
    mockCancelDeletion.mockClear()
    mockCancelDeletion.mockResolvedValue({
      message: 'Account deletion canceled successfully',
    })
  })

  describe('Auto-Processing', () => {
    it('should auto-process cancellation on mount with valid token', async () => {
      renderWithProviders(testToken)

      await waitFor(() => {
        expect(mockCancelDeletion).toHaveBeenCalledWith(testToken)
      })
    })

    it('should show loading spinner during processing', () => {
      mockCancelDeletion.mockImplementation(() => new Promise(() => {}))

      renderWithProviders(testToken)

      expect(screen.getByText(/processing cancellation/i)).toBeInTheDocument()
      expect(screen.getByText(/please wait while we cancel your account deletion/i)).toBeInTheDocument()
    })

    it('should NOT auto-process if token is missing', () => {
      renderWithProviders(undefined)
      expect(mockCancelDeletion).not.toHaveBeenCalled()
    })
  })

  describe('Success State', () => {
    it('should display success state with heading, message, and button', async () => {
      renderWithProviders(testToken)

      expect(await screen.findByText('✓', {}, { timeout: 5000 })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /account deletion canceled/i })).toBeInTheDocument()
      expect(screen.getByText(/your account and all your data are safe/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /go to profile/i })).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should display error message and "Try Again" button on failure', async () => {
      mockCancelDeletion.mockRejectedValue(new Error('Network error'))

      renderWithProviders(testToken)

      await waitFor(() => {
        expect(screen.getByText(/cancellation failed/i)).toBeInTheDocument()
        expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
      })
    })

    it('should retry cancellation when "Try Again" clicked', async () => {
      mockCancelDeletion.mockRejectedValueOnce(new Error('Network error'))
      renderWithProviders(testToken)

      const retryButton = await screen.findByRole('button', { name: /try again/i })
      mockCancelDeletion.mockResolvedValue({
        message: 'Account deletion canceled successfully',
      })

      fireEvent.click(retryButton)

      await waitFor(() => {
        expect(mockCancelDeletion).toHaveBeenCalledTimes(2)
      })
    })

    it('should display error reasons list on error', async () => {
      mockCancelDeletion.mockRejectedValue(new Error('Network error'))
      renderWithProviders(testToken)

      expect(await screen.findByText(/cancellation link has expired/i)).toBeInTheDocument()
    })
  })

  describe('Invalid Token', () => {
    it('should show error and "Go to Login" button for missing token', () => {
      renderWithProviders(undefined)
      expect(screen.getByText(/invalid cancellation link/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /go to login/i })).toBeInTheDocument()
    })

    it('should show error when API rejects token', async () => {
      mockCancelDeletion.mockRejectedValue(new Error('Invalid token'))

      renderWithProviders('invalid-token')

      await waitFor(() => {
        expect(screen.getByText(/cancellation failed/i)).toBeInTheDocument()
      })
    })
  })
})
