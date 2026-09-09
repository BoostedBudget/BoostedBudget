package services

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/rules"
)

// ─── Rule preview / dry run (#1625) ─────────────────────────────────────────────────────────
//
// There was no way to find out what a rule would do before saving it. domain has carried a
// TestCategorizationRuleRequest since #346 but no endpoint ever used it, so the only way to
// learn whether a rule matched anything was to save it and wait for the next sync. Combined
// with condition types that could not be discovered, that is how the reporting session ended up
// firing speculative writes at a live financial account.

// PreviewScanDefault is how many recent transactions a preview examines when no limit is given,
// and PreviewScanMax is the hard ceiling. A preview is an interactive, synchronous read on the
// hot path of a form, so it is bounded rather than scanning a user's whole history.
const (
	PreviewScanDefault = 200
	PreviewScanMax     = 500
)

// PreviewTransactionReader is the narrow slice of the transaction repository preview needs.
type PreviewTransactionReader interface {
	GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error)
}

// SetTransactionReader wires the reader used by PreviewRule (setter injection, matching the
// pattern used for analytics and history elsewhere in this package). Preview is unavailable —
// and says so — when it is not set, rather than panicking.
func (s *categorizationRuleService) SetTransactionReader(r PreviewTransactionReader) {
	s.transactionReader = r
}

// PreviewRule reports which of the user's recent transactions a CANDIDATE rule would match,
// without persisting anything.
//
// The candidate is validated first, so preview returns the SAME 422 that create would — which
// makes it usable as a validation oracle by the rule builder, instead of the user discovering
// the error only on save.
func (s *categorizationRuleService) PreviewRule(
	ctx context.Context,
	userID string,
	rule *domain.CategorizationRule,
	limit int,
) (*domain.RulePreviewResult, error) {
	if s.transactionReader == nil {
		return nil, fmt.Errorf("rule preview is not available: no transaction reader configured")
	}

	// ⚠ The endpoint promises "a rule that previews cleanly will also save cleanly", so preview
	// must refuse everything create refuses — not merely everything Validate() refuses. Ownership
	// of a custom-category target is an IDOR guard (#1540) plus a tombstone check (#1569 T9) that
	// lives OUTSIDE Validate(), so without this a rule aimed at another user's or a deleted
	// category previewed 200 and then 404'd on save. Found by review, not by me: it is the same
	// documentation-contradicts-behaviour loop this ticket exists to close, in the very feature
	// added to close it.
	if err := s.assertTargetCategoryOwnedAndLive(ctx, userID, rule.TargetCustomCategoryID); err != nil {
		return nil, err
	}

	rule.UserID = userID
	// ⚠ A candidate built from a create request has Enabled at its zero value, and
	// RuleMatches returns false for a disabled rule — so without this every preview would
	// report zero matches, correctly and uselessly. Pinned by
	// TestPreviewRule_ForcesEnabledOnCandidate.
	rule.Enabled = true
	if rule.ConditionLogic == "" {
		rule.ConditionLogic = "AND"
	}
	if rule.TagBehavior == "" {
		rule.TagBehavior = "add"
	}
	if rule.Source == "" {
		rule.Source = "manual"
	}
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	scan := limit
	if scan <= 0 {
		scan = PreviewScanDefault
	}
	if scan > PreviewScanMax {
		scan = PreviewScanMax
	}

	// This read enriches EffectiveMerchantName, so preview DOES evaluate effective_merchant
	// conditions against the same name sync uses (#1625 Task 11).
	//
	// ⚠ The two differ on FAILURE, deliberately. The repository's enrichment fails CLOSED — a
	// resolution error aborts this read and preview returns an error — while ApplyRulesToBatch
	// fails OPEN, logging and falling back to the raw provider name. That asymmetry is the right
	// one: a background sync must never drop categorisation over a merchant lookup, and a
	// synchronous preview must never quietly show matches computed from different data than the
	// sync would use. They never silently disagree; on failure preview declines to answer.
	txns, err := s.transactionReader.GetByUserIDWithFilters(ctx, userID, domain.TransactionFilters{}, scan, 0)
	if err != nil {
		return nil, err
	}

	// Only the engine is reused. ApplyRulesToTransaction MUTATES the transaction and
	// ApplyRulesToBatch writes execution-history records — neither is safe for a read-only
	// preview, which must not persist, must not bump match counts, and must not write history.
	engine := rules.NewRuleEngine()
	result := &domain.RulePreviewResult{
		ScannedCount: len(txns),
		Matches:      []domain.RulePreviewMatch{},
	}

	for i := range txns {
		tx := &txns[i]
		matched, err := engine.RuleMatches(tx, rule)
		if err != nil {
			// A malformed condition makes the preview useless rather than empty — report it,
			// unlike the sync path, which must keep going for the user's other rules.
			return nil, fmt.Errorf("%w: %s", domain.ErrInvalidCondition, err.Error())
		}
		if !matched {
			continue
		}
		result.MatchCount++
		result.Matches = append(result.Matches, domain.RulePreviewMatch{
			TransactionID:   tx.ID,
			Date:            tx.Date,
			Name:            tx.Name,
			MerchantName:    tx.MerchantName,
			Amount:          tx.Amount,
			CurrentCategory: categoryAssignment(tx),
			WouldAddTags:    rule.TargetTags,
		})
	}

	return result, nil
}
