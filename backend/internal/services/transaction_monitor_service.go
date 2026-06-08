// Package services provides transaction monitoring functionality.
// Issue #36: Rule Executors - Auto-detect when suggested transfers complete.
package services

import (
	"context"
	"math"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

// TransactionMonitorService is a no-op stub retained for API compatibility.
// Transfer detection is now handled by TransferCorrelationService.CorrelateForUser,
// which is wired into the transaction sync pipeline (Issue #972 Task B4).
type TransactionMonitorService interface {
	// MonitorForMatches is a no-op stub preserved for interface compatibility.
	// It no longer performs auto-accept; correlation is handled by
	// TransferCorrelationService instead.
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

// MonitorForMatches is a no-op stub retained for interface compatibility.
// Auto-accept logic has been removed in Issue #972: transfer correlation is now
// handled by TransferCorrelationService.CorrelateForUser, which is wired in via
// the transaction sync pipeline (Task B4).
func (s *transactionMonitorService) MonitorForMatches(
	ctx context.Context,
	userID string,
	newTransactions []*domain.Transaction,
) error {
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
