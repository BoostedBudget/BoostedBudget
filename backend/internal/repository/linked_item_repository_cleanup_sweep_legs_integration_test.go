//go:build integration

package repository

// Per-leg integration coverage for the SWEEP side of cleanupCandidateReasonPredicate — the shared
// four-legged OR that GetItemsForCleanupPage and ClaimItemForCleanup both splice (Issue #1486).
//
// Why this file exists. The predicate already had per-leg coverage from exactly one direction:
// TestClaimItemForCleanup_Integration_PerLeg, which drives the RECHECK. A reviewer deleted the
// `revoked` leg from the shared constant and only that recheck test reddened; nothing on the sweep
// side moved. Reproduced here before writing this file, so it is a measurement and not a
// supposition — with `OR li.status = 'revoked'` replaced by `OR FALSE`:
//
//	go test -tags integration -run 'ItemsForCleanupPage|ClaimItemForCleanup|NullInstitution' ./internal/repository/
//	--- FAIL: TestClaimItemForCleanup_Integration_PerLeg
//	    --- FAIL: TestClaimItemForCleanup_Integration_PerLeg/revoked_leg_needs_no_duration
//	FAIL
//
// One failure, on the recheck. The reason the sweep suite could not see it is in
// seedCleanupCandidates: it seeds 'revoked' items with NO accounts, so the
// zero-active-accounts leg matches those rows too and keeps selecting them after `revoked` is
// gone. Every sweep fixture in this package was ambiguous across at least two legs.
//
// So each case below starts from a row that matches NO leg (healthy status, fresh sync, one ACTIVE
// account, cleanup_scheduled_at NULL) and re-breaks exactly ONE leg. A row selected here can only
// have been selected by that leg, which is what makes the removal of any single leg detectable from
// the sweep side.
//
// Run with (never localhost:5432 — a Homebrew PG 15 shadows the project's PG 17 there):
//
//	DATABASE_URL=postgres://... go test ./internal/repository/ -run TestGetItemsForCleanupPage_Integration_PerLeg -tags integration -v -p 1 -count=1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedUnscheduledIneligibleItem creates a user and one linked item that the SWEEP does not select:
// status 'active', no error timestamp, synced just now, one active account, and — unlike
// seedClaimCleanupItem, which seeds the post-sweep state — cleanup_scheduled_at NULL, because
// `cleanup_scheduled_at IS NULL` is a candidacy condition of cleanupCandidateSelectQuery that
// ClaimItemForCleanup deliberately does not share.
func seedUnscheduledIneligibleItem(t *testing.T, ctx context.Context, tx pgx.Tx, repo *linkedItemRepository, profileRepo ProfileRepository) string {
	t.Helper()

	suffix := fmt.Sprintf("sweepleg_%d_%s", time.Now().UnixNano(), uuid.NewString()[:8])
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	item := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          "plaid-item-" + suffix,
		AccessToken:     "access-token-" + suffix,
		InstitutionID:   "ins_" + suffix,
		InstitutionName: "Test Bank",
		Provider:        "plaid",
	}
	require.NoError(t, repo.Create(ctx, item))

	seedActiveAccountFor(t, ctx, tx, item.ID)
	_, err := tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'active', last_error_at = NULL, error_code = NULL, error_message = NULL,
		    last_synced_at = NOW(), cleanup_scheduled_at = NULL
		WHERE id = $1`, item.ID)
	require.NoError(t, err)

	return item.ID
}

// TestGetItemsForCleanupPage_Integration_PerLeg is the sweep-side twin of
// TestClaimItemForCleanup_Integration_PerLeg. Both sides now walk every leg of the one shared
// constant in both directions, so tightening or dropping a leg for one caller fails for that caller
// specifically — which is the drift the #1486 split was introduced to make visible.
func TestGetItemsForCleanupPage_Integration_PerLeg(t *testing.T) {
	cases := []struct {
		name   string
		break_ string // SQL applied to the ineligible row; empty means leave it ineligible
		want   bool
	}{
		{name: "healthy_item_no_leg_matches", break_: "", want: false},
		{name: "error_leg_beyond_threshold", break_: "status = 'error', last_error_at = NOW() - INTERVAL '31 days'", want: true},
		{name: "error_leg_within_threshold", break_: "status = 'error', last_error_at = NOW() - INTERVAL '29 days'", want: false},
		{name: "login_required_leg_beyond_threshold", break_: "status = 'login_required', last_error_at = NOW() - INTERVAL '31 days'", want: true},
		// The leg this file was written for: 'revoked' is unconditional — no duration threshold
		// applies — and the row still has a fresh last_synced_at and an active account, so no other
		// leg can account for its selection.
		{name: "revoked_leg_needs_no_duration", break_: "status = 'revoked'", want: true},
		// cleanupPageStaleDays is 60, so 61/59 straddle it. Status stays 'active' here, keeping the
		// error leg out of the picture entirely.
		{name: "stale_leg_beyond_threshold", break_: "last_synced_at = NOW() - INTERVAL '61 days'", want: true},
		{name: "stale_leg_within_threshold", break_: "last_synced_at = NOW() - INTERVAL '59 days'", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)
			itemID := seedUnscheduledIneligibleItem(t, ctx, tx, repo, profileRepo)

			if tc.break_ != "" {
				_, err := tx.Exec(ctx,
					fmt.Sprintf(`UPDATE linked_items SET %s WHERE id = $1`, tc.break_), itemID)
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, sweepSelects(t, ctx, repo, itemID),
				"case %q: GetItemsForCleanupPage selection must be decided by this leg alone "+
					"(the row has one ACTIVE account and, unless this case changes it, a fresh "+
					"last_synced_at, so no other leg of cleanupCandidateReasonPredicate applies)",
				tc.name)
		})
	}
}

// TestGetItemsForCleanupPage_Integration_ZeroActiveAccountsLeg covers the fourth leg. It is split
// out of the table above because it needs a two-phase fixture — assert not-a-candidate first, then
// change the ACCOUNTS row rather than the linked_items row — and because it is the leg that made
// every other sweep fixture in this package ambiguous.
func TestGetItemsForCleanupPage_Integration_ZeroActiveAccountsLeg(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)
	itemID := seedUnscheduledIneligibleItem(t, ctx, tx, repo, profileRepo)

	require.False(t, sweepSelects(t, ctx, repo, itemID),
		"precondition: a healthy item with one active account must not be a candidate, or the "+
			"positive assertion below would pass for the wrong reason")

	// Not a DELETE: the leg is `NOT EXISTS (... AND a.status = 'active')`, so moving the only account
	// off 'active' must be enough on its own. 'expired' rather than something like 'closed' because
	// accounts_valid_status admits exactly ('active', 'replaced', 'expired') — a 'closed' here fails
	// the CHECK, which is how this assertion first surfaced.
	_, err := tx.Exec(ctx, `UPDATE accounts SET status = 'expired' WHERE linked_item_id = $1`, itemID)
	require.NoError(t, err)

	assert.True(t, sweepSelects(t, ctx, repo, itemID),
		"an item whose last active account is gone must become a cleanup candidate even though its "+
			"status is 'active' and it synced moments ago — it is still billing with nothing to sync")
}

// sweepSelects reports whether the paged sweep selects itemID, walking to exhaustion because these
// rows are NOT seeded to be the oldest candidates in a shared database and can land on any page.
func sweepSelects(t *testing.T, ctx context.Context, repo *linkedItemRepository, itemID string) bool {
	t.Helper()
	for _, it := range walkAllCleanupCandidates(t, ctx, repo) {
		if it.ID == itemID {
			return true
		}
	}
	return false
}
