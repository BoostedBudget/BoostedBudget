// Package services provides transfer suggestion management functionality.
// Issue #36: Rule Executors - Transfer suggestion accept/reject operations.
package services

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TransferSuggestionRepository defines the data access interface for transfer suggestions.
type TransferSuggestionRepository interface {
	GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error)
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)
	UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error
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
}

// transferSuggestionService implements TransferSuggestionService.
type transferSuggestionService struct {
	suggestionRepo TransferSuggestionRepository
}

// NewTransferSuggestionService creates a new TransferSuggestionService.
func NewTransferSuggestionService(suggestionRepo TransferSuggestionRepository) TransferSuggestionService {
	return &transferSuggestionService{
		suggestionRepo: suggestionRepo,
	}
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

	// 3. Check if already processed
	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, fmt.Errorf("suggestion already processed with status: %s", suggestion.Status)
	}

	// 4. Update status to accepted
	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusAccepted, notes); err != nil {
		return nil, fmt.Errorf("failed to update suggestion status: %w", err)
	}

	// 5. Update local copy and return
	suggestion.Status = domain.SuggestionStatusAccepted
	if notes != nil {
		suggestion.UserNotes = notes
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

	// 3. Check if already processed
	if suggestion.Status != domain.SuggestionStatusPending {
		return nil, fmt.Errorf("suggestion already processed with status: %s", suggestion.Status)
	}

	// 4. Update status to rejected
	if err := s.suggestionRepo.UpdateStatus(ctx, suggestionID, domain.SuggestionStatusRejected, notes); err != nil {
		return nil, fmt.Errorf("failed to update suggestion status: %w", err)
	}

	// 5. Update local copy and return
	suggestion.Status = domain.SuggestionStatusRejected
	if notes != nil {
		suggestion.UserNotes = notes
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
