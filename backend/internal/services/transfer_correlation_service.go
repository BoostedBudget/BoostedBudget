// Package services provides transfer correlation functionality.
// Issue #972: Savings transfer automation — B3 transfer correlation service.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	// ReserveMatchedTransaction pins matched_transaction_id = txnID (GAP AH reserve step).
	// The partial unique index prevents the same inflow from being reserved by two suggestions.
	ReserveMatchedTransaction(ctx context.Context, id string, txnID string) error
	// ClearMatchedTransactionID releases a reservation (HIGH#2 atomicity rollback; reject path).
	// GAP AV: expectedTxnID is the matched_transaction_id the caller observed at read time.
	ClearMatchedTransactionID(ctx context.Context, id string, expectedTxnID *string) error
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
	GetOpenByTransferSuggestionID(ctx context.Context, transferSuggestionID string, notificationType string) (*domain.Notification, error)
}

// TransferCorrelationService detects when users have followed through on transfer suggestions.
type TransferCorrelationService interface {
	// CorrelateForUser checks recent inflow transactions against active transfer suggestions
	// and marks suggestions as completed (exact match) or queues a fuzzy-match validation prompt.
	CorrelateForUser(ctx context.Context, userID string) error
}

// CorrelationGoalResolver finds active savings goals so boost-match can resolve
// which goal a completed suggestion targets (M19 D6 source-c / Issue #1300 RG1).
// Defined here to avoid import cycles; satisfies the same contract as SyncContributionsBasisGoalReader.
type CorrelationGoalResolver interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
}

// CorrelationConfirmedContributionUpserter creates or promotes a confirmed contribution
// when a boost-match completes (M19 D6 source-c / Issue #1300 RG1).
// An existing proposed row for the same (txn, goal) is promoted to confirmed via
// the ON CONFLICT clause (not duplicated).
type CorrelationConfirmedContributionUpserter interface {
	UpsertFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, source string) error
}

// transferCorrelationService implements TransferCorrelationService.
type transferCorrelationService struct {
	suggestionRepo   ActiveSuggestionRepository
	txnRepo          RecentTransactionRepository
	notifRepo        CorrelationNotificationRepository
	analytics        phclient.Analytics
	// Issue #1300 M19: optional boost-match contribution wiring (all nil = skip)
	correlationGoalResolver   CorrelationGoalResolver
	correlationContribUpserter CorrelationConfirmedContributionUpserter
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

// SetBoostMatchContributionServices wires in the D6 source-c boost-match contribution services.
// Both parameters are optional; if either is nil, boost-match contribution creation is skipped.
// Issue #1300 M19 / RG1.
func (s *transferCorrelationService) SetBoostMatchContributionServices(
	goalResolver CorrelationGoalResolver,
	contribUpserter CorrelationConfirmedContributionUpserter,
) {
	s.correlationGoalResolver = goalResolver
	s.correlationContribUpserter = contribUpserter
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

	// usedTxnIDs is the intra-run dedup guard. It prevents two suggestions in the
	// same CorrelateForUser call from claiming the same inflow. The cross-run guard
	// is the matched_transaction_id reservation + the partial unique index
	// (WHERE matched_transaction_id IS NOT NULL), which persists across syncs.
	usedTxnIDs := make(map[string]bool)

	// 3. For each active suggestion (oldest first — GetActiveByUserID already orders by created_at ASC)
	for _, suggestion := range suggestions {
		// Skip if already reserved or completed (matched_transaction_id already set).
		// This is the cross-run dedup guard: a reserved suggestion can't claim a second inflow,
		// and the existing reservation is still live until released (reject/stale-clear).
		if suggestion.MatchedTransactionID != nil {
			continue
		}

		for _, txn := range txns {
			// Skip pending (un-posted) transactions
			if txn.Pending {
				continue
			}

			// Skip if this transaction is already claimed by another suggestion in this run.
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
				// Complete the suggestion with the matched transaction id.
				// MEDIUM#5: if the partial unique index rejects our matched_transaction_id
				// (SQLSTATE 23505), the inflow was already claimed by another suggestion
				// (reservation or prior completion). Mark it used and continue to the next txn.
				txnID := txn.ID
				if err := s.suggestionRepo.Complete(ctx, suggestion.ID, &txnID, actualAmount, "exact_match", time.Now()); err != nil {
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == "23505" {
						slog.Warn("exact-match: inflow already claimed by another suggestion; skipping",
							"suggestion_id", suggestion.ID, "txn_id", txn.ID)
						usedTxnIDs[txn.ID] = true
						continue
					}
					return fmt.Errorf("failed to complete suggestion %s: %w", suggestion.ID, err)
				}
				usedTxnIDs[txn.ID] = true

				// Create acknowledgeable notification (exact match — no confirmation needed)
				notif := &domain.Notification{
					UserID:             userID,
					Type:               domain.NotificationTypeTransferSuggestion,
					InteractionPattern: "acknowledgeable",
					Title:              "Transfer matched ✓",
					Message:            fmt.Sprintf("We matched your $%.2f transfer to your savings account.", actualAmount),
				}
				if err := s.notifRepo.Create(ctx, notif); err != nil {
					return fmt.Errorf("failed to create match notification: %w", err)
				}

				// Issue #1300 M19 / D6 source-c: create/promote a confirmed contribution for
				// the savings goal linked to this suggestion's destination account (non-fatal).
				s.createBoostMatchContribution(ctx, userID, suggestion, txn.ID, actualAmount)

				// Fire analytics event
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventTransferSuggestionMatched, map[string]interface{}{
						"suggestion_id": suggestion.ID,
						"match_kind":    "exact",
						"actual_amount": actualAmount,
					})
				}
			} else {
				// matchFuzzy — check if there's already an open validation prompt.
				// The open-prompt guard uses type scoping (domain.NotificationTypeTransferSuggestionMatch)
				// so an unread original transfer_suggestion prompt does NOT block the fuzzy prompt.
				existing, err := s.notifRepo.GetOpenByTransferSuggestionID(ctx, suggestion.ID, domain.NotificationTypeTransferSuggestionMatch)
				if err != nil {
					return fmt.Errorf("failed to check open notification for suggestion %s: %w", suggestion.ID, err)
				}
				if existing != nil {
					// Already prompted — skip to next suggestion.
					break
				}

				// Mark this transaction as used in this run (intra-run guard).
				usedTxnIDs[txn.ID] = true

				// GAP AH reserve: set matched_transaction_id = txn.ID on the suggestion before
				// creating the notification. The partial unique index prevents two suggestions
				// from reserving the same inflow across syncs.
				// Skip cases: (a) SQLSTATE 23505 — another suggestion holds the txn via the
				// partial unique index; (b) ErrAlreadyActioned — suggestion is already reserved,
				// already actioned, or does not exist (zero-row race).
				if err := s.suggestionRepo.ReserveMatchedTransaction(ctx, suggestion.ID, txn.ID); err != nil {
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == "23505" {
						slog.Warn("fuzzy-match: inflow already claimed by another suggestion; skipping",
							"suggestion_id", suggestion.ID, "txn_id", txn.ID)
						continue
					}
					if errors.Is(err, domain.ErrTransferSuggestionAlreadyActioned) {
						// Suggestion was already reserved or actioned in a race — skip quietly.
						continue
					}
					return fmt.Errorf("failed to reserve transaction %s for suggestion %s: %w", txn.ID, suggestion.ID, err)
				}

				// Create accept_reject notification prompting user to confirm the amount.
				// Type must be NotificationTypeTransferSuggestionMatch so the frontend
				// renders the amount-confirm UI (NotificationConfirmMatch), not the
				// generic accept/reject buttons.
				suggID := suggestion.ID
				notif := &domain.Notification{
					UserID:               userID,
					Type:                 domain.NotificationTypeTransferSuggestionMatch,
					InteractionPattern:   "accept_reject",
					TransferSuggestionID: &suggID,
					Title:                "Did you transfer a different amount?",
					Message:              fmt.Sprintf("We suggested $%.2f but detected $%.2f — was this your transfer?", suggestion.Amount, actualAmount),
				}
				// HIGH#2 atomicity: if the notification insert fails after a successful reserve,
				// clear the reservation so the next sync retries rather than leaving the inflow
				// permanently pinned with no prompt for the user.
				// GAP AV: pass txn.ID (the reservation we just set) as expectedTxnID so that a
				// concurrent stale-clear that ran between reserve and this rollback does not cause
				// the rollback to incorrectly release a different reservation.
				txnIDCopy := txn.ID
				if err := s.notifRepo.Create(ctx, notif); err != nil {
					if clearErr := s.suggestionRepo.ClearMatchedTransactionID(ctx, suggestion.ID, &txnIDCopy); clearErr != nil {
						slog.Warn("failed to clear reservation after notification failure",
							"suggestion_id", suggestion.ID, "txn_id", txn.ID, "error", clearErr)
					}
					return fmt.Errorf("failed to create fuzzy-match notification: %w", err)
				}

				// Fire analytics event
				if s.analytics != nil {
					s.analytics.Capture(userID, phclient.EventTransferSuggestionMatched, map[string]interface{}{
						"suggestion_id": suggestion.ID,
						"match_kind":    "fuzzy",
						"actual_amount": actualAmount,
					})
				}
			}

			break // one match per suggestion per run
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

// createBoostMatchContribution creates or promotes a confirmed "boost_match" contribution for the
// savings goal linked to the suggestion's destination account (D6 source-c / Issue #1300 M19 RG1).
// Non-fatal: errors are logged and ignored — a missing contribution row is recoverable at next sync.
// The UpsertFromTransaction ON CONFLICT promotes a pre-existing proposed row to confirmed so that
// the M18-created proposed row is not left dangling after the boost match completes.
func (s *transferCorrelationService) createBoostMatchContribution(
	ctx context.Context,
	userID string,
	suggestion *domain.TransferSuggestion,
	txnID string,
	amount float64,
) {
	if s.correlationGoalResolver == nil || s.correlationContribUpserter == nil {
		return
	}

	// Resolve the savings goal whose AccountID matches the suggestion's destination account.
	// suggestion.ToAccountID is an internal account UUID (not a Plaid account ID).
	goals, err := s.correlationGoalResolver.GetActiveByUserID(ctx, userID)
	if err != nil {
		slog.Warn("createBoostMatchContribution: failed to load goals (skipping)",
			"user_id", userID, "suggestion_id", suggestion.ID, "error", err)
		return
	}

	if suggestion.ToAccountID == nil {
		return
	}
	toAccountID := *suggestion.ToAccountID

	for _, goal := range goals {
		if goal.ProgressBasis != domain.ProgressBasisContributions {
			continue
		}
		if goal.AccountID != toAccountID {
			continue
		}

		// Found the matching contributions-basis goal. Upsert a confirmed contribution.
		// UpsertFromTransaction uses ON CONFLICT to promote any pre-existing proposed row.
		if err := s.correlationContribUpserter.UpsertFromTransaction(
			ctx, userID, goal.ID, txnID, amount, "boost_match",
		); err != nil {
			slog.Warn("createBoostMatchContribution: upsert confirmed contribution failed (skipping)",
				"goal_id", goal.ID, "txn_id", txnID, "amount", amount, "error", err)
		}
		// Only one goal per account — stop after the first match.
		return
	}
}
