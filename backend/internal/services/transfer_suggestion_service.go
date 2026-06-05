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
	// Complete marks a suggestion as completed after a transfer was detected or confirmed.
	// Race-safe: only operates on pending or accepted suggestions.
	Complete(ctx context.Context, id string, matchedTransactionID string, actualAmount float64, completionSource string, completedAt time.Time) error
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
	// 1. Get the suggestion
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	// 2. Verify ownership (return not found for security - don't reveal existence)
	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	// 3. Check if already processed (optimistic guard — the DB has the authoritative guard via AND status='pending')
	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	// 4. Update status to accepted (the DB-level AND status='pending' guard handles races)
	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusAccepted, notes); err != nil {
		return nil, err
	}

	// 5. Update local copy and return
	suggestion.Status = domain.SuggestionStatusAccepted
	if notes != nil {
		suggestion.UserNotes = notes
	}

	// 6. Fire analytics event (GDPR consent-gated: user-initiated action)
	if s.gdprAnalytics != nil {
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.TransferSuggestionAccepted, map[string]interface{}{
			"amount":        suggestion.Amount,
			"currency_code": suggestion.CurrencyCode,
			"source":        string(suggestion.Source),
			"rule_id":       suggestion.RuleID,
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
	// 1. Get the suggestion
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	// 2. Verify ownership (return not found for security - don't reveal existence)
	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	// 3. Check if already processed (optimistic guard — the DB has the authoritative guard via AND status='pending')
	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	// 4. Update status to rejected (the DB-level AND status='pending' guard handles races)
	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusRejected, notes); err != nil {
		return nil, err
	}

	// 5. Update local copy and return
	suggestion.Status = domain.SuggestionStatusRejected
	if notes != nil {
		suggestion.UserNotes = notes
	}

	// 6. Fire analytics event (GDPR consent-gated: user-initiated action)
	if s.gdprAnalytics != nil {
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.TransferSuggestionRejected, map[string]interface{}{
			"amount":        suggestion.Amount,
			"currency_code": suggestion.CurrencyCode,
			"source":        string(suggestion.Source),
			"rule_id":       suggestion.RuleID,
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
// (fuzzy-match path). completionSource is always "user_confirmed".
func (s *transferSuggestionService) ConfirmTransferSuggestionMatch(
	ctx context.Context,
	userID string,
	suggestionID string,
	actualAmount float64,
) (*domain.TransferSuggestion, error) {
	// 1. Get the suggestion.
	suggestion, err := s.suggestionRepo.GetByID(ctx, suggestionID)
	if err != nil {
		return nil, err
	}

	// 2. Verify ownership (return not found for security — don't reveal existence).
	if suggestion.UserID != userID {
		return nil, domain.ErrTransferSuggestionNotFound
	}

	// 3. Guard: only pending or accepted can be completed.
	if suggestion.Status == domain.SuggestionStatusCompleted ||
		suggestion.Status == domain.SuggestionStatusRejected ||
		suggestion.Status == domain.SuggestionStatusExpired {
		return nil, domain.ErrTransferSuggestionAlreadyActioned
	}

	// 4. Complete in the database (race-safe: repo checks status IN ('pending','accepted')).
	completedAt := time.Now().UTC()
	if err := s.suggestionRepo.Complete(ctx, suggestionID, "", actualAmount, "user_confirmed", completedAt); err != nil {
		return nil, err
	}

	// 5. Update local copy and return.
	suggestion.Status = domain.SuggestionStatusCompleted
	suggestion.ActualAmount = &actualAmount
	suggestion.CompletedAt = &completedAt
	completionSource := "user_confirmed"
	suggestion.CompletionSource = &completionSource

	return suggestion, nil
}
