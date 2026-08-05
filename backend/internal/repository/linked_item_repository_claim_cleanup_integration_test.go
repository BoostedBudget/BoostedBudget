//go:build integration

package repository

// Integration tests for LinkedItemRepository.ClaimItemForCleanup (Issue #1486).
//
// ItemCleanupWorker used to call PlaidItemCleanupService.RemoveItem — Plaid /item/remove, which the
// user can only undo by re-linking the institution through Plaid Link — with NO check of any kind
// that the cleanup still applied, however long the job had been sitting in the queue.
// ClaimItemForCleanup is the recheck it now performs first, and this file is where the recheck's
// actual SQL semantics are pinned.
//
// pgxmock cannot validate any of this: it matches query text and argument shape, never whether the
// four-legged OR, the correlated NOT EXISTS over accounts, and the `$n::interval` arithmetic
// actually classify a repaired item as ineligible and an unrepaired one as eligible.
//
// Run with (never localhost:5432 — a Homebrew PG 15 shadows the project's PG 17 there):
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestClaimItemForCleanup_Integration -tags integration -v -p 1 -count=1

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

// seedClaimCleanupItem creates a user and one linked item, then applies overrides via a raw UPDATE
// so each test can put the row in exactly the state it wants to claim against. The item starts in
// the state the sweep would have left it: scheduled for cleanup (cleanup_scheduled_at set) and
// 'revoked', which is an unconditional leg of cleanupCandidateReasonPredicate.
func seedClaimCleanupItem(t *testing.T, ctx context.Context, tx pgx.Tx, repo *linkedItemRepository, profileRepo ProfileRepository) string {
	t.Helper()

	suffix := fmt.Sprintf("claimcleanup_%d_%s", time.Now().UnixNano(), uuid.NewString()[:8])
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

	_, err := tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'revoked', cleanup_scheduled_at = NOW() - INTERVAL '1 hour'
		WHERE id = $1`, item.ID)
	require.NoError(t, err)

	return item.ID
}

// seedActiveAccountFor attaches one active account to the item, which is what makes the
// "zero active accounts" leg stop matching.
func seedActiveAccountFor(t *testing.T, ctx context.Context, tx pgx.Tx, itemID string) {
	t.Helper()
	var userID string
	require.NoError(t, tx.QueryRow(ctx, `SELECT user_id FROM linked_items WHERE id = $1`, itemID).Scan(&userID))
	accountID := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository', 'active')`,
		accountID, itemID, userID, "plaid-acc-"+accountID.String())
	require.NoError(t, err)
}

// repairItem puts the row in the state a successful Plaid Link update-mode reconnect leaves behind:
// healthy status, a fresh sync, and at least one active account — i.e. no leg of
// cleanupCandidateReasonPredicate matches any more. cleanup_scheduled_at is deliberately LEFT SET,
// because nothing in production clears it on reconnect; that is the whole reason the recheck has to
// re-evaluate eligibility rather than merely re-read the schedule.
func repairItem(t *testing.T, ctx context.Context, tx pgx.Tx, itemID string) {
	t.Helper()
	seedActiveAccountFor(t, ctx, tx, itemID)
	_, err := tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'active', last_error_at = NULL, error_code = NULL, error_message = NULL,
		    last_synced_at = NOW()
		WHERE id = $1`, itemID)
	require.NoError(t, err)
}

// TestClaimItemForCleanup_Integration_RepairedItemIsNotClaimable is the core Issue #1486 assertion:
// an item that WAS a cleanup candidate, was scheduled, and has since been reconnected must NOT be
// claimable — even though its cleanup_scheduled_at is still set, because nothing clears it.
func TestClaimItemForCleanup_Integration_RepairedItemIsNotClaimable(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	require.True(t, claimed, "precondition: a scheduled, still-revoked item must be claimable, or the "+
		"negative assertion below would pass for the wrong reason")

	repairItem(t, ctx, tx, itemID)

	// The schedule is still set — proving the negative result below comes from the eligibility legs
	// and not from a schedule check.
	var stillScheduled bool
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT cleanup_scheduled_at IS NOT NULL FROM linked_items WHERE id = $1`, itemID).Scan(&stillScheduled))
	require.True(t, stillScheduled, "reconnect must NOT clear cleanup_scheduled_at — if this ever "+
		"becomes false, the premise of this whole recheck has changed and it must be re-derived")

	claimed, err = repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.False(t, claimed, "a reconnected item must not be claimable for irreversible Plaid removal")
}

// TestClaimItemForCleanup_Integration_PerLeg walks every leg of cleanupCandidateReasonPredicate in
// both directions. Each case starts from a repaired (ineligible) row and re-breaks exactly one leg,
// so a claim granted here can only have come from THAT leg.
func TestClaimItemForCleanup_Integration_PerLeg(t *testing.T) {
	cases := []struct {
		name   string
		break_ string // SQL applied to the repaired row; empty means leave it repaired
		want   bool
	}{
		{name: "repaired_item_no_leg_matches", break_: "", want: false},
		{name: "error_leg_beyond_threshold", break_: "status = 'error', last_error_at = NOW() - INTERVAL '31 days'", want: true},
		{name: "error_leg_within_threshold", break_: "status = 'error', last_error_at = NOW() - INTERVAL '29 days'", want: false},
		{name: "login_required_leg_beyond_threshold", break_: "status = 'login_required', last_error_at = NOW() - INTERVAL '31 days'", want: true},
		{name: "revoked_leg_needs_no_duration", break_: "status = 'revoked'", want: true},
		{name: "stale_leg_beyond_threshold", break_: "last_synced_at = NOW() - INTERVAL '61 days'", want: true},
		{name: "stale_leg_within_threshold", break_: "last_synced_at = NOW() - INTERVAL '59 days'", want: false},
		{name: "never_synced_is_not_stale_on_its_own", break_: "last_synced_at = NULL", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

			itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
			repairItem(t, ctx, tx, itemID)

			if tc.break_ != "" {
				_, err := tx.Exec(ctx, `UPDATE linked_items SET `+tc.break_+` WHERE id = $1`, itemID)
				require.NoError(t, err)
			}

			claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
			require.NoError(t, err)
			assert.Equal(t, tc.want, claimed)
		})
	}
}

// TestClaimItemForCleanup_Integration_ZeroActiveAccountsLeg covers the one leg that lives in a
// different table: the correlated NOT EXISTS over accounts. A non-active account must not count as
// an active one.
func TestClaimItemForCleanup_Integration_ZeroActiveAccountsLeg(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	repairItem(t, ctx, tx, itemID)

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	require.False(t, claimed, "precondition: a repaired item with an active account is not claimable")

	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE linked_item_id = $1`, itemID)
	require.NoError(t, err)

	claimed, err = repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.True(t, claimed, "an item whose only account is no longer active matches the zero-active-accounts leg")
}

// TestClaimItemForCleanup_Integration_RequiresSchedule pins the one condition ClaimItemForCleanup
// adds on its own: the item must still be SCHEDULED. This is the inverse of the sweep's
// `cleanup_scheduled_at IS NULL` candidacy leg, and it is what makes ClearCleanupSchedule work as
// the documented cancellation hook it claims to be — even though no production path calls it before
// removal today (grep `\.ClearCleanupSchedule(`).
func TestClaimItemForCleanup_Integration_RequiresSchedule(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)

	_, err := tx.Exec(ctx, `UPDATE linked_items SET cleanup_scheduled_at = NULL WHERE id = $1`, itemID)
	require.NoError(t, err)

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.False(t, claimed, "an unscheduled item must not be claimable, however eligible it otherwise looks")
}

// TestClaimItemForCleanup_Integration_MissingRow proves a vanished row is (false, nil) — a benign
// skip — and not an error, so the worker treats an already-deleted item as nothing to do.
func TestClaimItemForCleanup_Integration_MissingRow(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)
	_ = seedClaimCleanupItem(t, ctx, tx, repo, profileRepo) // keeps the tx non-trivial

	claimed, err := repo.ClaimItemForCleanup(ctx, uuid.NewString(), cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.False(t, claimed)
}

// TestClaimItemForCleanup_Integration_ReplacedItemStaysClaimable pins the deliberate asymmetry
// documented on cleanupCandidateReasonPredicate: 'replaced' is excluded from the SWEEP's candidacy
// but must NOT veto a claim. The re-link path (services/account_service.go) marks the old item
// 'replaced' WITHOUT calling Plaid /item/remove, so its Plaid item is still live and still billing —
// and since the sweep also excludes 'replaced', declining here would strand it forever.
func TestClaimItemForCleanup_Integration_ReplacedItemStaysClaimable(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	// Exactly what a re-link leaves behind: item 'replaced', its accounts 'replaced'.
	seedActiveAccountFor(t, ctx, tx, itemID)
	_, err := tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE linked_item_id = $1`, itemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'replaced', replaced_at = NOW(), last_error_at = NULL, last_synced_at = NOW()
		WHERE id = $1`, itemID)
	require.NoError(t, err)

	// Confirm the sweep does NOT see it, so the two behaviours are pinned against each other.
	var sweepSees bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM linked_items li
			WHERE li.id = $3 AND li.cleanup_scheduled_at IS NULL
			  AND li.status NOT IN ('replaced', 'disconnected')
			  AND `+cleanupCandidateReasonPredicate+`)`,
		fmt.Sprintf("%d days", cleanupPageErrorDays), fmt.Sprintf("%d days", cleanupPageStaleDays), itemID,
	).Scan(&sweepSees))
	require.False(t, sweepSees, "a 'replaced' item is never a SWEEP candidate")

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.True(t, claimed, "a 'replaced' item whose Plaid item was never removed must still be claimable")
}

// TestClaimItemForCleanup_Integration_DeleteAccountPathIsClaimable walks the OTHER production
// producer's sequence at the SQL level: accountService.DeleteAccount (services/account_service.go)
// enqueues an item_cleanup job when the last account at an institution is deleted, and as of Issue
// #1486 stamps the schedule with the REAL MarkForCleanup first. Before that stamp existed, the
// worker's claim was denied and the job reported success while Plaid kept billing.
//
// The fixture is the case the daily sweep can NEVER rescue, which is why this is the important row:
// accountRepository.GetByIDAndUser applies no status filter, so an account on a 'replaced' linked
// item is deletable — and the sweep's candidate predicate excludes 'replaced'. Without the stamp
// this item is stranded permanently, still billing.
func TestClaimItemForCleanup_Integration_DeleteAccountPathIsClaimable(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	// Undo the seed's schedule: DeleteAccount's path starts from an UNSCHEDULED item, which is the
	// whole point. Also make it 'replaced' with a recent sync, so the ONLY leg that can match is
	// zero-active-accounts — the leg DeleteAccount itself creates.
	_, err := tx.Exec(ctx, `
		UPDATE linked_items
		SET cleanup_scheduled_at = NULL, cleanup_notified_at = NULL,
		    status = 'replaced', replaced_at = NOW(), last_error_at = NULL, last_synced_at = NOW()
		WHERE id = $1`, itemID)
	require.NoError(t, err)

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	require.False(t, claimed,
		"precondition — this is the bug: an unstamped item is NOT claimable, so DeleteAccount's job "+
			"would have been declined and silently reported success")

	// Exactly what DeleteAccount now does before enqueueing.
	require.NoError(t, repo.MarkForCleanup(ctx, itemID, time.Now()))

	claimed, err = repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.True(t, claimed,
		"after MarkForCleanup the DeleteAccount-enqueued job must be claimable — otherwise a "+
			"'replaced' zero-account item is stranded forever, because the sweep excludes 'replaced'")
}

// TestClaimItemForCleanup_Integration_MatchesSweepPredicate is the no-drift oracle: for every item
// the SWEEP itself selects (via walkAllCleanupCandidates, which pages GetItemsForCleanupPage to
// exhaustion — #1488 deleted the unbounded GetItemsForCleanup this used to call), stamping the
// schedule the way MarkForCleanup does must leave the item claimable. If someone ever tightens
// cleanupCandidateReasonPredicate for one caller and not the other, this fails.
func TestClaimItemForCleanup_Integration_MatchesSweepPredicate(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	const seeded = 5
	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, seeded)

	selected := walkAllCleanupCandidates(t, ctx, repo)
	selectedIDs := map[string]bool{}
	for _, it := range selected {
		selectedIDs[it.ID] = true
	}

	for _, id := range ids {
		require.True(t, selectedIDs[id], "sanity: the sweep must select seeded item %s", id)

		// What MarkForCleanup does, and nothing else.
		_, execErr := tx.Exec(ctx, `
			UPDATE linked_items SET cleanup_notified_at = NOW(), cleanup_scheduled_at = NOW()
			WHERE id = $1 AND cleanup_scheduled_at IS NULL`, id)
		require.NoError(t, execErr)

		claimed, claimErr := repo.ClaimItemForCleanup(ctx, id, cleanupPageErrorDays, cleanupPageStaleDays)
		require.NoError(t, claimErr)
		assert.True(t, claimed,
			"item %s was selected by the sweep and changed only by MarkForCleanup, so the recheck must "+
				"still claim it — otherwise the sweep and the recheck have drifted and every scheduled "+
				"cleanup would be silently declined", id)
	}
}
