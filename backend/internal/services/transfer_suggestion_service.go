// Package services provides transfer suggestion management functionality.
// Issue #36: Rule Executors - Transfer suggestion accept/reject operations.
package services

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
)

// TransferSuggestionRepository defines the data access interface for transfer suggestions.
type TransferSuggestionRepository interface {
	GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error)
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)
	UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error
	// CancelAcceptedSuggestion atomically transitions a pending or accepted suggestion to
	// rejected, clearing matched_transaction_id. UpdateStatus cannot handle accepted rows.
	CancelAcceptedSuggestion(ctx context.Context, id string, userNotes *string) error
	// Complete marks a suggestion as completed after a transfer was detected or confirmed.
	// matchedTransactionID is nil for user-confirmed completions (no specific transaction ID).
	// Race-safe: only operates on pending or accepted suggestions.
	Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error
	// CompleteRetainingReservation completes a suggestion without touching matched_transaction_id.
	// The DB retains whatever matched_transaction_id currently holds (T if still reserved, NULL if
	// stale-cleared). Avoids the race where a stale-read id overwrites a reclaimed reservation.
	// Race-safe: only operates on pending or accepted suggestions.
	//
	// GAP AR: expectedTxnID is the matched_transaction_id the service observed at read time.
	// If the reservation was reclaimed between read and write, returns ErrTransferSuggestionAlreadyActioned.
	CompleteRetainingReservation(ctx context.Context, id string, actualAmount float64, completionSource string, completedAt time.Time, expectedTxnID *string) error
	// ClearMatchedTransactionID releases the reservation on a suggestion by setting
	// matched_transaction_id = NULL. Used for user-driven decline-match.
	//
	// GAP AV: expectedTxnID is the matched_transaction_id the caller observed at read time.
	// If the reservation was reclaimed between read and write, returns ErrTransferSuggestionAlreadyActioned.
	ClearMatchedTransactionID(ctx context.Context, id string, expectedTxnID *string) error

	// GetByUserIDStatuses retrieves suggestions filtered by status slice with pagination.
	// A nil/empty slice falls back to pending-only semantics.
	GetByUserIDStatuses(ctx context.Context, userID string, statuses []domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, int, error)
}

// TransferSuggestionService handles transfer suggestion operations.
type TransferSuggestionService interface {
	// AcceptSuggestion accepts a pending transfer suggestion.
	// Returns ErrTransferSuggestionNotFound if suggestion doesn't exist or belongs to different user.
	AcceptSuggestion(ctx context.Context, userID, suggestionID string, notes *string) (*domain.TransferSuggestion, error)

	// RejectSuggestion rejects a pending transfer suggestion.
	// Returns ErrTransferSuggestionNotFound if suggestion doesn't exist or belongs to different user.
	RejectSuggestion(ctx context.Context, userID, suggestionID string, notes *string) (*domain.TransferSuggestion, error)

	// GetPendingSuggestions returns all pending suggestions for a user.
	GetPendingSuggestions(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)

	// ConfirmTransferSuggestionMatch completes a pending/accepted suggestion via user confirmation
	// (fuzzy-match path). Used by the MCP-ready C3 endpoint.
	// Returns ErrTransferSuggestionNotFound if suggestion doesn't exist or belongs to different user.
	// Returns ErrTransferSuggestionAlreadyActioned if status is completed, rejected, or expired.
	ConfirmTransferSuggestionMatch(ctx context.Context, userID, suggestionID string, actualAmount float64) (*domain.TransferSuggestion, error)

	// DeclineTransferSuggestionMatch releases the fuzzy-match reservation on a pending/accepted
	// suggestion (user chose "No, that wasn't my transfer"). Clears matched_transaction_id so the
	// inflow can be matched by a different suggestion.
	// Returns ErrTransferSuggestionNotFound if suggestion doesn't exist or belongs to a different user.
	// Returns ErrTransferSuggestionAlreadyActioned if status is completed, rejected, or expired.
	DeclineTransferSuggestionMatch(ctx context.Context, userID, suggestionID string) (*domain.TransferSuggestion, error)

	// GetSuggestionsByStatus returns suggestions for a user filtered by status, with pagination.
	// Returns (items, totalCount, error). A nil statuses slice defaults to pending-only semantics.
	// Used by the /boosts review page for the Active Recommendations, Awaiting Match, and History tabs.
	GetSuggestionsByStatus(ctx context.Context, userID string, statuses []domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, int, error)

	// CancelAcceptedSuggestion cancels a pending or accepted suggestion, transitioning it to
	// rejected. Used by the Boosts "Cancel" button in the Awaiting Match tab (distinct from
	// DeclineTransferSuggestionMatch, which only clears the fuzzy-match reservation without
	// transitioning the status). The cancelled suggestion leaves the accepted tab on next poll.
	// Returns ErrTransferSuggestionNotFound if suggestion doesn't exist or belongs to a different user.
	// Returns ErrTransferSuggestionAlreadyActioned if status is completed, rejected, or expired.
	CancelAcceptedSuggestion(ctx context.Context, userID, suggestionID string, notes *string) (*domain.TransferSuggestion, error)
}

// transferSuggestionService implements TransferSuggestionService.
type transferSuggestionService struct {
	suggestionRepo TransferSuggestionRepository
	gdprAnalytics  phclient.GDPRAnalytics
}

// NewTransferSuggestionService creates a new TransferSuggestionService.
// Returns the concrete type so callers can invoke optional setter methods (SetGDPRAnalytics).
func NewTransferSuggestionService(suggestionRepo TransferSuggestionRepository) *transferSuggestionService {
	return &transferSuggestionService{
		suggestionRepo: suggestionRepo,
	}
}

// SetGDPRAnalytics sets the GDPR-consent-gated analytics client for user-behavior event capture.
func (s *transferSuggestionService) SetGDPRAnalytics(a phclient.GDPRAnalytics) {
	s.gdprAnalytics = a
}

// AcceptSuggestion accepts a pending transfer suggestion.
func (s *transferSuggestionService) AcceptSuggestion(
	ctx context.Context,
	userID string,
	suggestionID string,
	notes *string,
) (*domain.TransferSuggestion, error) {
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusAccepted, notes); err != nil {
		return nil, err
	}

	suggestion.Status = domain.SuggestionStatusAccepted
	if notes != nil {
		suggestion.UserNotes = notes
	}

	if s.gdprAnalytics != nil {
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.TransferSuggestionAccepted, map[string]interface{}{
			"amount":        suggestion.Amount,
			"currency_code": suggestion.CurrencyCode,
			"source":        string(suggestion.Source),
			"boost_id":      suggestion.BoostID,
		})
	}

	return suggestion, nil
}

// RejectSuggestion rejects a pending transfer suggestion.
func (s *transferSuggestionService) RejectSuggestion(
	ctx context.Context,
	userID string,
	suggestionID string,
	notes *string,
) (*domain.TransferSuggestion, error) {
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusRejected, notes); err != nil {
		return nil, err
	}

	suggestion.Status = domain.SuggestionStatusRejected
	if notes != nil {
		suggestion.UserNotes = notes
	}

	if s.gdprAnalytics != nil {
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.TransferSuggestionRejected, map[string]interface{}{
			"amount":        suggestion.Amount,
			"currency_code": suggestion.CurrencyCode,
			"source":        string(suggestion.Source),
			"boost_id":      suggestion.BoostID,
		})
	}

	return suggestion, nil
}

// GetPendingSuggestions returns all pending suggestions for a user.
func (s *transferSuggestionService) GetPendingSuggestions(
	ctx context.Context,
	userID string,
) ([]*domain.TransferSuggestion, error) {
	return s.suggestionRepo.GetPendingByUserID(ctx, userID)
}

// ConfirmTransferSuggestionMatch completes a pending/accepted suggestion via user confirmation
// (fuzzy-match path). Uses CompleteRetainingReservation to avoid race with ClearStaleReservations (GAP AK).
func (s *transferSuggestionService) ConfirmTransferSuggestionMatch(
	ctx context.Context,
	userID string,
	suggestionID string,
	actualAmount float64,
) (*domain.TransferSuggestion, error) {
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	if suggestion.Status == domain.SuggestionStatusCompleted ||
		suggestion.Status == domain.SuggestionStatusRejected ||
		suggestion.Status == domain.SuggestionStatusExpired {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	// GAP AU: a nil reservation means the fuzzy prompt is stale (stale-cleared by daily
	// worker). No legitimate confirm-with-nil path exists — ReserveMatchedTransaction always
	// sets a non-nil reservation before the fuzzy prompt fires. Reject to prevent double-count.
	if suggestion.MatchedTransactionID == nil {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	completedAt := time.Now().UTC()
	// GAP AR: pass the observed reservation so the DB rejects the update if the inflow was
	// reclaimed between this read and the write (IS NOT DISTINCT FROM predicate).
	// MatchedTransactionID is guaranteed non-nil here (guarded above by GAP AU).
	if err := s.suggestionRepo.CompleteRetainingReservation(ctx, suggestionID, actualAmount, "user_confirmed", completedAt, suggestion.MatchedTransactionID); err != nil {
		return nil, err
	}

	suggestion.Status = domain.SuggestionStatusCompleted
	suggestion.ActualAmount = &actualAmount
	suggestion.CompletedAt = &completedAt
	completionSource := "user_confirmed"
	suggestion.CompletionSource = &completionSource

	return suggestion, nil
}

// GetSuggestionsByStatus returns suggestions for a user filtered by status, with pagination.
// Delegates directly to the repository; no business logic is applied beyond ownership
// (the repo already filters by userID). A nil statuses slice uses pending-only semantics.
func (s *transferSuggestionService) GetSuggestionsByStatus(
	ctx context.Context,
	userID string,
	statuses []domain.SuggestionStatus,
	limit, offset int,
) ([]*domain.TransferSuggestion, int, error) {
	return s.suggestionRepo.GetByUserIDStatuses(ctx, userID, statuses, limit, offset)
}

// DeclineTransferSuggestionMatch releases the fuzzy-match reservation on a pending/accepted
// suggestion (user chose "No, that wasn't my transfer"). Clears matched_transaction_id so the
// inflow can be matched by a different suggestion.
func (s *transferSuggestionService) DeclineTransferSuggestionMatch(
	ctx context.Context,
	userID string,
	suggestionID string,
) (*domain.TransferSuggestion, error) {
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	if suggestion.Status == domain.SuggestionStatusCompleted ||
		suggestion.Status == domain.SuggestionStatusRejected ||
		suggestion.Status == domain.SuggestionStatusExpired {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	// GAP AV: pass the observed MatchedTransactionID so the IS NOT DISTINCT FROM predicate
	// in the repo rejects a stale decline that would otherwise release a newer reservation T2.
	if err := s.suggestionRepo.ClearMatchedTransactionID(ctx, suggestionID, suggestion.MatchedTransactionID); err != nil {
		return nil, err
	}

	suggestion.MatchedTransactionID = nil
	return suggestion, nil
}

// CancelAcceptedSuggestion transitions a pending or accepted suggestion to rejected.
// This is the Boosts "Cancel" action in the Awaiting Match tab — distinct from
// DeclineTransferSuggestionMatch (which only clears the fuzzy-match reservation without
// changing status). After cancellation the row leaves the Awaiting tab on the next poll.
func (s *transferSuggestionService) CancelAcceptedSuggestion(
	ctx context.Context,
	userID string,
	suggestionID string,
	notes *string,
) (*domain.TransferSuggestion, error) {
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	if suggestion.Status == domain.SuggestionStatusCompleted ||
		suggestion.Status == domain.SuggestionStatusRejected ||
		suggestion.Status == domain.SuggestionStatusExpired {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	if err := s.suggestionRepo.CancelAcceptedSuggestion(ctx, suggestionID, notes); err != nil {
		return nil, err
	}

	suggestion.Status = domain.SuggestionStatusRejected
	if notes != nil {
		suggestion.UserNotes = notes
	}

	if s.gdprAnalytics != nil {
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.TransferSuggestionCanceled, map[string]interface{}{
			"amount":        suggestion.Amount,
			"currency_code": suggestion.CurrencyCode,
			"source":        string(suggestion.Source),
			"rule_id":       suggestion.RuleID,
		})
	}

	return suggestion, nil
}
