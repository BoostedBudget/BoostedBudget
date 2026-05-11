// Package services provides transaction monitoring functionality.
// Issue #36: Rule Executors - Auto-detect when suggested transfers complete.
package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// Default matching parameters.
const (
	// DefaultAmountTolerance is the maximum difference in cents for amount matching.
	DefaultAmountTolerance = 0.01 // 1 cent

	// DefaultDateRangeDays is the number of days a transaction can be from suggestion creation.
	DefaultDateRangeDays = 7
)

// PendingSuggestionRepository defines the data access interface needed for monitoring.
type PendingSuggestionRepository interface {
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)
	UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error
}

// TransactionMonitorService monitors transactions to auto-accept matching suggestions.
type TransactionMonitorService interface {
	// MonitorForMatches checks new transactions against pending suggestions.
	// If a match is found, the suggestion is auto-accepted.
	// Match criteria:
	// - Transaction account matches suggestion's ToAccountID
	// - Amount within tolerance (default 1 cent)
	// - Transaction date within range of suggestion creation (default 7 days)
	// - Negative transaction amount (inflow/credit/deposit under PFCv2)
	MonitorForMatches(ctx context.Context, userID string, newTransactions []*domain.Transaction) error
}

// transactionMonitorService implements TransactionMonitorService.
type transactionMonitorService struct {
	suggestionRepo  PendingSuggestionRepository
	amountTolerance float64
	dateRangeDays   int
}

// NewTransactionMonitorService creates a new TransactionMonitorService with default parameters.
func NewTransactionMonitorService(suggestionRepo PendingSuggestionRepository) TransactionMonitorService {
	return &transactionMonitorService{
		suggestionRepo:  suggestionRepo,
		amountTolerance: DefaultAmountTolerance,
		dateRangeDays:   DefaultDateRangeDays,
	}
}

// MonitorForMatches checks new transactions against pending suggestions.
func (s *transactionMonitorService) MonitorForMatches(
	ctx context.Context,
	userID string,
	newTransactions []*domain.Transaction,
) error {
	// Early return if no transactions
	if len(newTransactions) == 0 {
		return nil
	}

	// Get pending suggestions for this user
	suggestions, err := s.suggestionRepo.GetPendingByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get pending suggestions: %w", err)
	}

	// No pending suggestions to match against
	if len(suggestions) == 0 {
		return nil
	}

	// Check each transaction against each suggestion
	// Canary counter: each call that resolves suggestions uses chain-aware account matching (Issue #1109).
	observability.AccountsReplacedFiltered.WithLabelValues("transaction_match").Inc()

	for _, txn := range newTransactions {
		for _, suggestion := range suggestions {
			if s.isMatch(txn, suggestion) {
				// Auto-accept the suggestion
				// Use ExternalTransactionID as fallback (batch inserts don't return IDs)
				txnIdentifier := txn.ID
				if txnIdentifier == "" && txn.ExternalTransactionID != nil {
					txnIdentifier = *txn.ExternalTransactionID
				}
				note := fmt.Sprintf("Auto-matched with transaction %s", txnIdentifier)
				if err := s.suggestionRepo.UpdateStatus(ctx, suggestion.ID, domain.SuggestionStatusAccepted, &note); err != nil {
					return fmt.Errorf("failed to auto-accept suggestion %s: %w", suggestion.ID, err)
				}
				// Mark suggestion as matched so we don't match it again
				suggestion.Status = domain.SuggestionStatusAccepted
			}
		}
	}

	return nil
}

// accountsMatch returns true when the transaction account matches the suggestion
// destination. Prefers stable UserAccountID comparison when both sides have one
// (post-relink scenario); falls back to raw account_id for legacy data. (Issue #1109)
func accountsMatch(txnAccountID string, txnUserAccountID *string, suggAccountID *string, suggUserAccountID *string) bool {
	if suggUserAccountID != nil && txnUserAccountID != nil {
		return *txnUserAccountID == *suggUserAccountID
	}
	if suggAccountID != nil {
		return txnAccountID == *suggAccountID
	}
	return false
}

// isMatch checks if a transaction matches a suggestion.
func (s *transactionMonitorService) isMatch(txn *domain.Transaction, suggestion *domain.TransferSuggestion) bool {
	// Skip if suggestion has no destination reference at all
	if suggestion.ToAccountID == nil && suggestion.ToUserAccountID == nil {
		return false
	}

	// Skip if suggestion is no longer pending (already matched in this batch)
	if suggestion.Status != domain.SuggestionStatusPending {
		return false
	}

	// 1. Check account match — chain-aware: prefer UserAccountID when available
	if !accountsMatch(txn.AccountID, txn.UserAccountID, suggestion.ToAccountID, suggestion.ToUserAccountID) {
		return false
	}

	// 2. Check inflow (negative amount = credit/deposit under PFCv2)
	if !txn.IsInflow() {
		return false
	}

	// 3. Check amount magnitude within tolerance (txn.Amount is negative for inflows under PFCv2)
	if math.Abs(math.Abs(txn.Amount)-suggestion.Amount) > s.amountTolerance {
		return false
	}

	// 4. Check date range (within dateRangeDays of suggestion creation)
	dateLimit := suggestion.CreatedAt.Add(time.Duration(s.dateRangeDays) * 24 * time.Hour)
	if txn.Date.After(dateLimit) || txn.Date.Before(suggestion.CreatedAt) {
		return false
	}

	return true
}
