// Package services provides transfer correlation functionality.
// Issue #972: Savings transfer automation — B3 transfer correlation service.
package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
)

// correlationLookbackDays is the number of days of inflow history to load.
// 37 days covers the 30-day suggestion window plus 7-day match window with buffer.
const correlationLookbackDays = 37

// correlationDateWindowDays is the number of days after the reference date within
// which a transaction must fall to be eligible for matching.
const correlationDateWindowDays = 7

// correlationFuzzyMaxDiffRatio is the maximum proportional difference allowed for a fuzzy match.
// 0.50 means the detected amount must be within 50% of the suggestion amount.
const correlationFuzzyMaxDiffRatio = 0.50

// ActiveSuggestionRepository is the subset of TransferSuggestionRepository needed
// by TransferCorrelationService.
type ActiveSuggestionRepository interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)
	Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error
}

// RecentTransactionRepository is the subset of TransactionRepository needed
// by TransferCorrelationService.
type RecentTransactionRepository interface {
	GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error)
}

// CorrelationNotificationRepository is the subset of NotificationRepository needed
// by TransferCorrelationService.
type CorrelationNotificationRepository interface {
	Create(ctx context.Context, notification *domain.Notification) error
	GetOpenByTransferSuggestionID(ctx context.Context, transferSuggestionID string) (*domain.Notification, error)
}

// TransferCorrelationService detects when users have followed through on transfer suggestions.
type TransferCorrelationService interface {
	// CorrelateForUser checks recent inflow transactions against active transfer suggestions
	// and marks suggestions as completed (exact match) or queues a fuzzy-match validation prompt.
	CorrelateForUser(ctx context.Context, userID string) error
}

// transferCorrelationService implements TransferCorrelationService.
type transferCorrelationService struct {
	suggestionRepo ActiveSuggestionRepository
	txnRepo        RecentTransactionRepository
	notifRepo      CorrelationNotificationRepository
	analytics      phclient.Analytics
}

// NewTransferCorrelationService creates a new TransferCorrelationService.
// Returns the concrete type so callers can invoke optional setter methods (SetAnalytics).
func NewTransferCorrelationService(
	suggestionRepo ActiveSuggestionRepository,
	txnRepo RecentTransactionRepository,
	notifRepo CorrelationNotificationRepository,
) *transferCorrelationService {
	return &transferCorrelationService{
		suggestionRepo: suggestionRepo,
		txnRepo:        txnRepo,
		notifRepo:      notifRepo,
	}
}

// SetAnalytics sets the analytics client for event capture.
func (s *transferCorrelationService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// CorrelateForUser loads active suggestions and recent inflow transactions for the
// user and attempts to match them, completing exact matches and creating
// accept_reject prompts for fuzzy matches.
func (s *transferCorrelationService) CorrelateForUser(ctx context.Context, userID string) error {
	// 1. Load all active suggestions (pending + accepted), oldest first
	suggestions, err := s.suggestionRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active suggestions: %w", err)
	}
	if len(suggestions) == 0 {
		return nil
	}

	// 2. Load recent inflow transactions (last correlationLookbackDays days)
	since := time.Now().Add(-correlationLookbackDays * 24 * time.Hour)
	txns, err := s.txnRepo.GetRecentInflowsByUserID(ctx, userID, since)
	if err != nil {
		return fmt.Errorf("failed to get recent inflow transactions: %w", err)
	}

	// Track which transaction IDs have been claimed so we don't double-match.
	usedTxnIDs := make(map[string]bool)

	// 3. For each active suggestion (oldest first — GetActiveByUserID already orders by created_at ASC)
	for _, suggestion := range suggestions {
		// Skip if already queued for processing (MatchedTransactionID already set)
		if suggestion.MatchedTransactionID != nil {
			continue
		}

		for _, txn := range txns {
			// Skip pending (un-posted) transactions
			if txn.Pending {
				continue
			}

			// Skip if this transaction is already claimed by another suggestion
			if usedTxnIDs[txn.ID] {
				continue
			}

			// Check match
			matchKind := s.classifyMatch(txn, suggestion)
			if matchKind == matchNone {
				continue
			}

			actualAmount := math.Abs(txn.Amount)

			if matchKind == matchExact {
				// Complete the suggestion
				txnID := txn.ID
			if err := s.suggestionRepo.Complete(ctx, suggestion.ID, &txnID, actualAmount, "exact_match", time.Now()); err != nil {
					return fmt.Errorf("failed to complete suggestion %s: %w", suggestion.ID, err)
				}
				usedTxnIDs[txn.ID] = true

				// Create acknowledgeable notification
				notif := &domain.Notification{
					UserID:             userID,
					Type:               "transfer_suggestion",
					InteractionPattern: "acknowledgeable",
					Title:              "Transfer matched ✓",
					Message:            fmt.Sprintf("We matched your $%.2f transfer to your savings account.", actualAmount),
				}
				if err := s.notifRepo.Create(ctx, notif); err != nil {
					return fmt.Errorf("failed to create match notification: %w", err)
				}

				// Fire analytics event for matched transfer suggestion
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventTransferSuggestionMatched, map[string]interface{}{
						"suggestion_id": suggestion.ID,
						"match_kind":    "exact",
						"actual_amount": actualAmount,
					})
				}
			} else {
				// matchFuzzy — check if there's already an open validation prompt
				existing, err := s.notifRepo.GetOpenByTransferSuggestionID(ctx, suggestion.ID)
				if err != nil {
					return fmt.Errorf("failed to check open notification for suggestion %s: %w", suggestion.ID, err)
				}
				if existing != nil {
					// Already prompted — skip
					break
				}

				// Mark this transaction as used so it doesn't prompt multiple suggestions.
				usedTxnIDs[txn.ID] = true

				// Create accept_reject notification prompting user to confirm
				suggID := suggestion.ID
				notif := &domain.Notification{
					UserID:               userID,
					Type:                 "transfer_suggestion",
					InteractionPattern:   "accept_reject",
					TransferSuggestionID: &suggID,
					Title:                "Did you transfer a different amount?",
					Message:              fmt.Sprintf("We suggested $%.2f but detected $%.2f — was this your transfer?", suggestion.Amount, actualAmount),
				}
				if err := s.notifRepo.Create(ctx, notif); err != nil {
					return fmt.Errorf("failed to create fuzzy-match notification: %w", err)
				}

				// Fire analytics event for fuzzy-matched transfer suggestion
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventTransferSuggestionMatched, map[string]interface{}{
						"suggestion_id": suggestion.ID,
						"match_kind":    "fuzzy",
						"actual_amount": actualAmount,
					})
				}
			}

			break // one suggestion per run
		}
	}

	return nil
}

// matchKind classifies the relationship between a transaction and a suggestion.
type matchKind int

const (
	matchNone  matchKind = iota
	matchExact           // amount within $0.01
	matchFuzzy           // account+timing match, amount differs but within 50%
)

// classifyMatch returns the match classification for the given txn/suggestion pair.
func (s *transferCorrelationService) classifyMatch(txn *domain.Transaction, suggestion *domain.TransferSuggestion) matchKind {
	// Must be an inflow
	if !txn.IsInflow() {
		return matchNone
	}

	// Must have a destination reference
	if suggestion.ToAccountID == nil && suggestion.ToUserAccountID == nil {
		return matchNone
	}

	// Account must match
	if !accountsMatch(txn.AccountID, txn.UserAccountID, suggestion.ToAccountID, suggestion.ToUserAccountID) {
		return matchNone
	}

	// Date must be within the 7-day window after the reference date
	referenceDate := s.referenceDate(suggestion)
	windowEnd := referenceDate.Add(correlationDateWindowDays * 24 * time.Hour)
	if txn.Date.Before(referenceDate) || txn.Date.After(windowEnd) {
		return matchNone
	}

	// Compare amounts
	actualAmount := math.Abs(txn.Amount)
	diff := math.Abs(actualAmount - suggestion.Amount)

	if diff <= DefaultAmountTolerance {
		return matchExact
	}

	// Fuzzy: differ by more than $0.01 but within 50% of suggestion amount
	if suggestion.Amount > 0 && (diff/suggestion.Amount) <= correlationFuzzyMaxDiffRatio {
		return matchFuzzy
	}

	return matchNone
}

// referenceDate returns the date from which the match window starts:
// UserActionAt (accepted timestamp) if the suggestion was accepted, otherwise CreatedAt.
func (s *transferCorrelationService) referenceDate(suggestion *domain.TransferSuggestion) time.Time {
	if suggestion.Status == domain.SuggestionStatusAccepted && suggestion.UserActionAt != nil {
		return *suggestion.UserActionAt
	}
	return suggestion.CreatedAt
}
