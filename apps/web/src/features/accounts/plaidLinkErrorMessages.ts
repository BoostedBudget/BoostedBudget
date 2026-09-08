/**
 * Shared error-message mapping for Plaid Link flows.
 *
 * Split out of PlaidLinkButton.tsx (Issue #1609) so the /accounts/reconnect page
 * (ReconnectPage.tsx) can import mapPlaidErrorToMessage without triggering
 * react-refresh/only-export-components — a component file may only export components.
 */

import type { PlaidLinkError } from 'react-plaid-link'

/**
 * Maps error responses to user-friendly messages
 */
export function mapErrorToMessage(error: unknown): string {
  // Type guard for error objects with response property
  const hasResponse = (err: unknown): err is { response: { status: number } } => {
    return typeof err === 'object' && err !== null && 'response' in err &&
      typeof (err as { response?: unknown }).response === 'object' &&
      (err as { response?: unknown }).response !== null &&
      'status' in ((err as { response: unknown }).response as object)
  }

  // Type guard for error objects with code property
  const hasCode = (err: unknown): err is { code: string } => {
    return typeof err === 'object' && err !== null && 'code' in err
  }

  if (hasResponse(error)) {
    if (error.response.status === 401) {
      return 'Please log in again to continue.'
    }
    if (error.response.status === 403) {
      return 'Unable to link this account. Please contact support.'
    }
    if (error.response.status === 400) {
      return 'Invalid or expired connection. Please try again.'
    }
    if (error.response.status === 503) {
      return 'Bank connection service temporarily unavailable. Please try again in a few minutes.'
    }
    if (error.response.status === 500) {
      return 'An error occurred while connecting your bank. Please try again.'
    }
  }

  if (hasCode(error) && error.code === 'ERR_NETWORK') {
    return 'Network error. Please check your internet connection.'
  }

  return 'Failed to load bank connection. Please try again.'
}

/**
 * Maps a Plaid Link error (from `onExit`) to a user-readable message.
 *
 * Issue #1609 (Item 6): a 2026-09-07 production incident (Citibank Online /
 * ins_5, error_code INSTITUTION_REGISTRATION_REQUIRED) showed only a generic
 * "Something went wrong" message in the UI — the real cause was only visible
 * by cross-referencing the Plaid Dashboard's Link Sessions view. The
 * error_code is always included below so the message stays diagnosable even
 * for codes without a specific mapping.
 *
 * Shared (Issue #1609) between PlaidLinkButton and the /accounts/reconnect page's
 * update-mode Link flow — Plaid Link errors mean the same thing regardless of which
 * flow opened Link.
 */
export function mapPlaidErrorToMessage(err: PlaidLinkError): string {
  switch (err.error_code) {
    case 'INSTITUTION_REGISTRATION_REQUIRED':
      return 'This bank is not yet fully set up for secure connections (INSTITUTION_REGISTRATION_REQUIRED). Please try again later or contact support.'
    case 'INSTITUTION_NOT_RESPONDING':
    case 'INSTITUTION_DOWN':
      return `Your bank is temporarily unavailable (${err.error_code}). Please try again in a few minutes.`
    case 'INSTITUTION_NOT_AVAILABLE':
      return `This bank isn't available right now (${err.error_code}). Please try again later.`
    default:
      return `Unable to connect your bank account (${err.error_code}). Please try again or contact support.`
  }
}
