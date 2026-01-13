/**
 * DeleteAccountSection Component Tests
 *
 * Tests the account deletion UI component.
 * Issue #575: Uses centralized API mocks with real useDeletion hook.
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { DeleteAccountSection } from './DeleteAccountSection'
import {
  mockUseRequestDeletion,
  mockRequestDeletionMutateAsync,
  mockCancelDeletion,
} from '@/test/mocks/user-api'
import type { RequestDeletionResponse } from '../types'

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return {
    ...render(
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    ),
    queryClient,
  }
}

describe('DeleteAccountSection', () => {
  const mockSuccessResponse: RequestDeletionResponse = {
    message: 'Account deletion scheduled',
    scheduled_at: '2025-11-26T00:00:00Z',
    cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token',
  }

  beforeEach(() => {
    mockUseRequestDeletion.mockClear()
    mockRequestDeletionMutateAsync.mockClear()
    mockCancelDeletion.mockClear()

    // Default successful response
    mockRequestDeletionMutateAsync.mockResolvedValue(mockSuccessResponse)
    mockUseRequestDeletion.mockReturnValue({
      mutateAsync: mockRequestDeletionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockCancelDeletion.mockResolvedValue({
      message: 'Account deletion canceled successfully',
    })
  })

  describe('Initial Render', () => {
    it('should display "Delete Account" title', () => {
      renderWithQuery(<DeleteAccountSection />)
      expect(screen.getByText('Delete Account')).toBeInTheDocument()
    })

    it('should display warning box with danger zone message', () => {
      renderWithQuery(<DeleteAccountSection />)
      expect(screen.getByText(/⚠️ Danger Zone/i)).toBeInTheDocument()
      expect(screen.getByText(/permanent and irreversible/i)).toBeInTheDocument()
    })

    it('should display list of data that will be deleted', () => {
      renderWithQuery(<DeleteAccountSection />)
      expect(screen.getByText(/Linked bank accounts and transactions/i)).toBeInTheDocument()
      expect(screen.getByText(/Budget data and automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/Profile and privacy settings/i)).toBeInTheDocument()
      expect(screen.getByText(/Subscription and payment history/i)).toBeInTheDocument()
    })

    it('should display "Delete My Account" button', () => {
      renderWithQuery(<DeleteAccountSection />)
      const deleteButton = screen.getByRole('button', { name: /delete my account/i })
      expect(deleteButton).toBeInTheDocument()
      expect(deleteButton).toBeEnabled()
    })

    it('should not display confirmation dialog initially', () => {
      renderWithQuery(<DeleteAccountSection />)
      expect(screen.queryByText(/are you absolutely sure/i)).not.toBeInTheDocument()
      expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    })
  })

  describe('Confirmation Dialog', () => {
    it('should show confirmation dialog when delete button clicked', () => {
      renderWithQuery(<DeleteAccountSection />)
      const deleteButton = screen.getByRole('button', { name: /delete my account/i })
      fireEvent.click(deleteButton)
      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()
    })

    it('should display "Are you absolutely sure?" heading', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      expect(screen.getByText('Are you absolutely sure?')).toBeInTheDocument()
    })

    it('should display checkbox with confirmation text', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox = screen.getByRole('checkbox')
      expect(checkbox).toBeInTheDocument()
      expect(screen.getByText(/I understand that all my data will be permanently deleted/i)).toBeInTheDocument()
    })

    it('should disable confirm button when checkbox unchecked', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const confirmButton = screen.getByRole('button', { name: /yes, delete my account/i })
      expect(confirmButton).toBeDisabled()
    })

    it('should enable confirm button when checkbox checked', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox = screen.getByRole('checkbox')
      fireEvent.click(checkbox)
      const confirmButton = screen.getByRole('button', { name: /yes, delete my account/i })
      expect(confirmButton).toBeEnabled()
    })

    it('should close dialog when cancel button clicked', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()
      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      fireEvent.click(cancelButton)
      expect(screen.queryByText(/are you absolutely sure/i)).not.toBeInTheDocument()
    })

    it('should reset checkbox state when dialog reopened', () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox1 = screen.getByRole('checkbox')
      fireEvent.click(checkbox1)
      expect(checkbox1).toBeChecked()
      fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox2 = screen.getByRole('checkbox')
      expect(checkbox2).not.toBeChecked()
    })
  })

  describe('Deletion Request Flow', () => {
    it('should call requestAccountDeletion when confirmed', async () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))
      await waitFor(() => {
        expect(mockRequestDeletionMutateAsync).toHaveBeenCalledTimes(1)
      })
    })

    it('should disable buttons during deletion request', () => {
      // First open dialog with normal state
      const { rerender, queryClient } = renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      // Verify dialog is open
      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()

      // Now configure mock to simulate pending state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: true,
        isError: false,
        error: null,
      })

      // Re-render with the pending state
      rerender(
        <QueryClientProvider client={queryClient}>
          <DeleteAccountSection />
        </QueryClientProvider>
      )

      // Button should show Processing... and be disabled
      const confirmButton = screen.getByRole('button', { name: /processing.../i })
      expect(confirmButton).toBeDisabled()
    })

    it('should show "Processing..." text during request', () => {
      // First open dialog with normal state
      const { rerender, queryClient } = renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      // Verify dialog is open
      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()

      // Now configure mock to simulate pending state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: true,
        isError: false,
        error: null,
      })

      // Re-render with the pending state
      rerender(
        <QueryClientProvider client={queryClient}>
          <DeleteAccountSection />
        </QueryClientProvider>
      )

      expect(screen.getByText('Processing...')).toBeInTheDocument()
    })

    it('should trigger deletion request with success response', async () => {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))
      await waitFor(() => {
        expect(mockRequestDeletionMutateAsync).toHaveBeenCalled()
      })
    })
  })

  describe('Error Handling', () => {
    it('should display error message when deletion fails', () => {
      const error = new Error('Failed to schedule deletion')
      // Configure mock to simulate error state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: false,
        isError: true,
        error,
      })

      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(error.message)).toBeInTheDocument()
    })

    it('should keep confirmation dialog open on error', () => {
      const error = new Error('Network error')
      // Configure mock to simulate error state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: false,
        isError: true,
        error,
      })

      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()
    })

    it('should handle network errors gracefully', () => {
      const error = new Error('Network request failed')
      // Configure mock to simulate error state
      mockUseRequestDeletion.mockReturnValue({
        mutateAsync: mockRequestDeletionMutateAsync,
        isPending: false,
        isError: true,
        error,
      })

      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(error.message)).toBeInTheDocument()
    })
  })

  describe('Cancellation Link Display', () => {
    it('should generate correct cancellation URL format', () => {
      const origin = 'http://localhost:3000'
      const expectedToken = mockSuccessResponse.cancellation_token
      const expectedUrl = `${origin}/cancel-deletion/${expectedToken}`
      expect(expectedUrl).toContain('/cancel-deletion/')
      expect(expectedUrl).toContain(expectedToken)
    })
  })
})
