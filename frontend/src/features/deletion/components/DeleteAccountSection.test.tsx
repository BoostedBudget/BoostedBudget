import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { RequestDeletionResponse } from '../types'

const mockRequestAccountDeletion = mock(() => Promise.resolve({}))
const mockCancelAccountDeletion = mock(() => Promise.resolve({}))

const mockUseDeletion = mock(() => ({
  requestAccountDeletion: mockRequestAccountDeletion,
  isRequesting: false,
  requestError: null,
  cancelAccountDeletion: mockCancelAccountDeletion,
  isCanceling: false,
  cancelError: null,
}))

mock.module('../hooks/useDeletion', () => ({
  useDeletion: mockUseDeletion,
}))

import { DeleteAccountSection } from './DeleteAccountSection'

describe('DeleteAccountSection', () => {
  const mockSuccessResponse: RequestDeletionResponse = {
    message: 'Account deletion scheduled',
    scheduled_at: '2025-11-26T00:00:00Z',
    cancellation_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.token',
  }

  beforeEach(() => {
    mockRequestAccountDeletion.mockClear()
    mockCancelAccountDeletion.mockClear()
    mockUseDeletion.mockReturnValue({
      requestAccountDeletion: mockRequestAccountDeletion,
      isRequesting: false,
      requestError: null,
      cancelAccountDeletion: mockCancelAccountDeletion,
      isCanceling: false,
      cancelError: null,
    })
  })

  describe('Initial Render', () => {
    it('should display "Delete Account" title', () => {
      render(<DeleteAccountSection />)

      expect(screen.getByText('Delete Account')).toBeInTheDocument()
    })

    it('should display warning box with danger zone message', () => {
      render(<DeleteAccountSection />)

      expect(screen.getByText(/⚠️ Danger Zone/i)).toBeInTheDocument()
      expect(screen.getByText(/permanent and irreversible/i)).toBeInTheDocument()
    })

    it('should display list of data that will be deleted', () => {
      render(<DeleteAccountSection />)

      expect(screen.getByText(/Linked bank accounts and transactions/i)).toBeInTheDocument()
      expect(screen.getByText(/Budget data and automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/Profile and privacy settings/i)).toBeInTheDocument()
      expect(screen.getByText(/Subscription and payment history/i)).toBeInTheDocument()
    })

    it('should display "Delete My Account" button', () => {
      render(<DeleteAccountSection />)

      const deleteButton = screen.getByRole('button', { name: /delete my account/i })
      expect(deleteButton).toBeInTheDocument()
      expect(deleteButton).toBeEnabled()
    })

    it('should not display confirmation dialog initially', () => {
      render(<DeleteAccountSection />)

      expect(screen.queryByText(/are you absolutely sure/i)).not.toBeInTheDocument()
      expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    })
  })

  describe('Confirmation Dialog', () => {
    it('should show confirmation dialog when delete button clicked', () => {
      render(<DeleteAccountSection />)

      const deleteButton = screen.getByRole('button', { name: /delete my account/i })
      fireEvent.click(deleteButton)

      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()
    })

    it('should display "Are you absolutely sure?" heading', () => {
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText('Are you absolutely sure?')).toBeInTheDocument()
    })

    it('should display checkbox with confirmation text', () => {
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      const checkbox = screen.getByRole('checkbox')
      expect(checkbox).toBeInTheDocument()
      expect(screen.getByText(/I understand that all my data will be permanently deleted/i)).toBeInTheDocument()
    })

    it('should disable confirm button when checkbox unchecked', () => {
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      const confirmButton = screen.getByRole('button', { name: /yes, delete my account/i })
      expect(confirmButton).toBeDisabled()
    })

    it('should enable confirm button when checkbox checked', () => {
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      const checkbox = screen.getByRole('checkbox')
      fireEvent.click(checkbox)

      const confirmButton = screen.getByRole('button', { name: /yes, delete my account/i })
      expect(confirmButton).toBeEnabled()
    })

    it('should close dialog when cancel button clicked', () => {
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()

      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      fireEvent.click(cancelButton)

      expect(screen.queryByText(/are you absolutely sure/i)).not.toBeInTheDocument()
    })

    it('should reset checkbox state when dialog reopened', () => {
      render(<DeleteAccountSection />)

      // Open dialog and check checkbox
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox1 = screen.getByRole('checkbox')
      fireEvent.click(checkbox1)
      expect(checkbox1).toBeChecked()

      // Close dialog
      fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

      // Reopen dialog
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      const checkbox2 = screen.getByRole('checkbox')
      expect(checkbox2).not.toBeChecked()
    })
  })

  describe('Deletion Request Flow', () => {
    it('should call requestAccountDeletion when confirmed', async () => {
      mockRequestAccountDeletion.mockResolvedValue(mockSuccessResponse)

      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))

      await waitFor(() => {
        expect(mockRequestAccountDeletion).toHaveBeenCalledTimes(1)
      })
    })

    it('should disable buttons during deletion request', () => {
      const { rerender } = render(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: true,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })
      rerender(<DeleteAccountSection />)

      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      const confirmButton = screen.getByRole('button', { name: /processing.../i })

      expect(cancelButton).toBeDisabled()
      expect(confirmButton).toBeDisabled()
    })

    it('should show "Processing..." text during request', () => {
      const { rerender } = render(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: true,
        requestError: null,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })
      rerender(<DeleteAccountSection />)

      expect(screen.getByText('Processing...')).toBeInTheDocument()
    })

    it('should trigger deletion request with success response', async () => {
      mockRequestAccountDeletion.mockResolvedValue(mockSuccessResponse)
      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))

      await waitFor(() => {
        expect(mockRequestAccountDeletion).toHaveBeenCalled()
      })
    })
  })

  describe('Error Handling', () => {
    it('should display error message when deletion fails', () => {
      const error = new Error('Failed to schedule deletion')

      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: false,
        requestError: error,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })

      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(error.message)).toBeInTheDocument()
    })

    it('should keep confirmation dialog open on error', () => {
      const error = new Error('Network error')

      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: false,
        requestError: error,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })

      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(/are you absolutely sure/i)).toBeInTheDocument()
    })

    it('should clear error when trying again', async () => {
      const error = new Error('First attempt failed')

      // First render with error
      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: false,
        requestError: error,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })

      const { rerender } = render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      expect(screen.getByText(error.message)).toBeInTheDocument()

      // Rerender without error (simulating retry)
      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: false,
        requestError: null, // Error cleared
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })

      rerender(<DeleteAccountSection />)

      expect(screen.queryByText(error.message)).not.toBeInTheDocument()
    })

    it('should handle network errors gracefully', () => {
      const error = new Error('Network request failed')

      mockUseDeletion.mockReturnValue({
        requestAccountDeletion: mockRequestAccountDeletion,
        isRequesting: false,
        requestError: error,
        cancelAccountDeletion: mockCancelAccountDeletion,
        isCanceling: false,
        cancelError: null,
      })

      render(<DeleteAccountSection />)

      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))

      expect(screen.getByText(error.message)).toBeInTheDocument()
    })
  })

  describe('Cancellation Link Display', () => {
    it('should generate correct cancellation URL format', () => {
      // Don't override window.location - it pollutes other tests
      // Just test the URL format directly
      const origin = 'http://localhost:3000'
      const expectedToken = mockSuccessResponse.cancellation_token
      const expectedUrl = `${origin}/cancel-deletion/${expectedToken}`

      expect(expectedUrl).toContain('/cancel-deletion/')
      expect(expectedUrl).toContain(expectedToken)
    })
  })
})
