// Package domain provides core domain models and types for BoostedBudget.
// This file defines types for Plaid TransactionsSync API integration.
package domain

import (
	"fmt"

	"github.com/plaid/plaid-go/v23/plaid"
)

// TransactionSyncResult aggregates transaction data from all pagination pages.
// Returned by TransactionSyncService.SyncTransactions after fetching all pages
// from Plaid's /transactions/sync API until has_more=false.
//
// Issue #260: Plaid TransactionsSync API Integration
type TransactionSyncResult struct {
	// Added contains new transactions from Plaid since last sync
	Added []plaid.Transaction

	// Modified contains transactions that changed (e.g., pending → posted)
	Modified []plaid.Transaction

	// Removed contains transactions deleted from Plaid (bank corrections)
	Removed []plaid.RemovedTransaction

	// NewCursor is the cursor after all pages fetched (for next sync)
	NewCursor string
}

// PlaidErrorType classifies Plaid API errors for retry strategy.
// Used by WebhookProcessor to determine if webhook should be retried.
//
// Issue #260: Error handling for INVALID_ACCESS_TOKEN and RATE_LIMIT_EXCEEDED
type PlaidErrorType int

const (
	// PlaidErrorTemporary indicates transient errors (network, server errors)
	// that should be retried with exponential backoff.
	PlaidErrorTemporary PlaidErrorType = iota

	// PlaidErrorPermanent indicates errors requiring user action (reauth)
	// and should NOT be retried automatically.
	// Examples: INVALID_ACCESS_TOKEN, ITEM_LOGIN_REQUIRED
	PlaidErrorPermanent

	// PlaidErrorRateLimit indicates rate limiting by Plaid API.
	// Should be retried with exponential backoff (longer delays).
	PlaidErrorRateLimit

	// PlaidErrorInvalidCursor indicates the sync cursor is corrupted or expired.
	// Issue #171: Auto-recovery by resetting cursor and triggering full re-sync.
	PlaidErrorInvalidCursor
)

// PlaidSyncError wraps Plaid API errors with classification metadata.
// Enables TransactionSyncService to communicate error type to caller
// without coupling to Plaid SDK error types.
type PlaidSyncError struct {
	// OriginalError is the underlying error from Plaid SDK
	OriginalError error

	// ErrorType classifies the error for retry strategy
	ErrorType PlaidErrorType

	// ErrorCode is the Plaid error code (e.g., "INVALID_ACCESS_TOKEN")
	// Empty string for non-Plaid errors
	ErrorCode string
}

// Error implements the error interface for PlaidSyncError.
func (e *PlaidSyncError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("plaid error %s: %v", e.ErrorCode, e.OriginalError)
	}
	return fmt.Sprintf("plaid sync error: %v", e.OriginalError)
}

// ClassifyPlaidError analyzes a Plaid API error and returns classification
// for retry strategy. Uses plaid.ToPlaidError() to extract Plaid error codes.
//
// Classification rules:
//   - INVALID_ACCESS_TOKEN, ITEM_LOGIN_REQUIRED → Permanent (user must reauth)
//   - RATE_LIMIT_EXCEEDED → RateLimit (retry with longer backoff)
//   - All other Plaid errors → Temporary (retry with normal backoff)
//   - Non-Plaid errors (network, etc.) → Temporary
//
// Issue #260: Handles INVALID_ACCESS_TOKEN as permanent, RATE_LIMIT_EXCEEDED with retry
func ClassifyPlaidError(err error) *PlaidSyncError {
	// Attempt to extract Plaid-specific error
	plaidErr, convErr := plaid.ToPlaidError(err)
	if convErr != nil {
		// Not a Plaid API error (network error, etc.)
		return &PlaidSyncError{
			OriginalError: err,
			ErrorType:     PlaidErrorTemporary,
			ErrorCode:     "",
		}
	}

	// Classify based on Plaid error code
	switch plaidErr.ErrorCode {
	case "INVALID_ACCESS_TOKEN", "ITEM_LOGIN_REQUIRED":
		// User must reauth in Plaid Link - don't retry
		return &PlaidSyncError{
			OriginalError: err,
			ErrorType:     PlaidErrorPermanent,
			ErrorCode:     plaidErr.ErrorCode,
		}

	case "RATE_LIMIT_EXCEEDED":
		// Plaid rate limit hit - retry with backoff
		return &PlaidSyncError{
			OriginalError: err,
			ErrorType:     PlaidErrorRateLimit,
			ErrorCode:     plaidErr.ErrorCode,
		}

	case "INVALID_CURSOR":
		// Issue #171: Cursor is corrupted or expired - reset and full re-sync
		return &PlaidSyncError{
			OriginalError: err,
			ErrorType:     PlaidErrorInvalidCursor,
			ErrorCode:     plaidErr.ErrorCode,
		}

	default:
		// Other Plaid errors (INTERNAL_SERVER_ERROR, etc.) - retry
		return &PlaidSyncError{
			OriginalError: err,
			ErrorType:     PlaidErrorTemporary,
			ErrorCode:     plaidErr.ErrorCode,
		}
	}
}
