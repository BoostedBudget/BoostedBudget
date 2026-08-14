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
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

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

  // Issue #1514 (fix round 5): the cancellation token is a BEARER CAPABILITY — the
  // backend's CancelDeletion endpoint is public and validates the request by the token
  // alone (backend/internal/huma/operations/deletion.go:79-87), so anyone who obtains it
  // can cancel a scheduled GDPR erasure. Rendering it as `href` (an attribute — data-ph-mask
  // cannot cover it) or as link text (a token PostHog session replay would also capture)
  // exposes that capability to session replay. This is a SECRET/capability exposure, not
  // PII — a different risk class than the rest of this file's masking work; see the note
  // added to docs/security/posthog-pii-masking.md.
  //
  // The OLD version of this describe block ("Cancellation Link Display") never actually
  // rendered the deletion-scheduled screen — it just asserted a string template built
  // inline in the test itself, so it exercised nothing and encoded no assertion about the
  // real DOM. There was no existing "the URL must be visible" assertion to update.
  //
  // Investigation: the backend (backend/internal/services/deletion_service.go's
  // RequestDeletion) already calls `s.emailService.SendDeletionConfirmation(...)`, which
  // is a real, fully-implemented SendGrid send (backend/internal/services/email_service.go)
  // containing the same cancellation link — the frontend's old inline comment ("When
  // SendGrid is integrated...") was stale; email delivery already exists. So the on-screen
  // token is a redundant convenience for the CURRENT session, not the only delivery
  // channel — except that the email send is explicitly best-effort ("Email failure is not
  // critical"), so a same-session cancel affordance still has real value as a fallback.
  // The fix: an in-page button that calls cancelAccountDeletion(token) directly — the
  // token lives only in React state / JS memory and is passed straight to the API client,
  // never serialized into an attribute or text node.
  describe('Cancellation (token never rendered)', () => {
    async function scheduleDeletionAndReachCancelScreen() {
      renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))
      await waitFor(() => {
        expect(screen.getByText(/account deletion scheduled/i)).toBeInTheDocument()
      })
    }

    it('never puts the cancellation token into any DOM attribute or text node', async () => {
      const { container } = renderWithQuery(<DeleteAccountSection />)
      fireEvent.click(screen.getByRole('button', { name: /delete my account/i }))
      fireEvent.click(screen.getByRole('checkbox'))
      fireEvent.click(screen.getByRole('button', { name: /yes, delete my account/i }))
      await waitFor(() => {
        expect(screen.getByText(/account deletion scheduled/i)).toBeInTheDocument()
      })

      const sentinels: PIISentinel[] = [
        { value: mockSuccessResponse.cancellation_token, fieldDescription: 'cancellation_token (bearer capability, #1514 round 5)' },
      ]
      expect(scanForUnmaskedPII(container, sentinels)).toEqual([])
    })

    it('shows a "Cancel Account Deletion" button that calls cancelAccountDeletion with the in-memory token', async () => {
      await scheduleDeletionAndReachCancelScreen()

      const cancelBtn = screen.getByRole('button', { name: /cancel account deletion/i })
      fireEvent.click(cancelBtn)

      await waitFor(() => {
        expect(mockCancelDeletion).toHaveBeenCalledWith(mockSuccessResponse.cancellation_token)
      })
    })

    it('shows a success message after the cancel button succeeds', async () => {
      await scheduleDeletionAndReachCancelScreen()

      fireEvent.click(screen.getByRole('button', { name: /cancel account deletion/i }))

      await waitFor(() => {
        expect(screen.getByText(/deletion.*cancel/i)).toBeInTheDocument()
      })
    })

    it('shows an error message if the cancel button fails, without leaking the token', async () => {
      mockCancelDeletion.mockRejectedValueOnce(new Error('Invalid or expired cancellation token'))
      await scheduleDeletionAndReachCancelScreen()

      fireEvent.click(screen.getByRole('button', { name: /cancel account deletion/i }))

      await waitFor(() => {
        expect(screen.getByText(/invalid or expired cancellation token/i)).toBeInTheDocument()
      })

      const sentinels: PIISentinel[] = [
        { value: mockSuccessResponse.cancellation_token, fieldDescription: 'cancellation_token (bearer capability, #1514 round 5)' },
      ]
      expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
    })
  })
})
