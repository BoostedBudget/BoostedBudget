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

// repairItem is a SYNTHETIC fixture, not a simulation of RepairStatus: it directly writes a row
// where no leg of cleanupCandidateReasonPredicate matches (healthy status, a fresh sync, at least
// one active account) while deliberately LEAVING cleanup_scheduled_at SET. Since Issue #1609 P3-T8,
// the real RepairStatus clears cleanup_scheduled_at itself, which would make every test below moot —
// this helper exists precisely to hold the schedule constant so each test can isolate ONE
// predicate leg at a time (PerLeg, ZeroActiveAccountsLeg) independent of the schedule-clearing fix.
// Also unlike RepairStatus, this refreshes last_synced_at — real production code never does that;
// see TestClaimItemForCleanup_Integration_RealisticReconnectSequence_SurvivesStaleSyncCleanup
// (linked_item_repository_claim_cleanup_integration_test.go) for the test that drives the ACTUAL
// RepairStatus call and deliberately does NOT refresh last_synced_at.
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
// claimable, even in the (synthetic, see repairItem above) case where its cleanup_scheduled_at is
// still set — i.e. even without relying on the Issue #1609 P3-T8 schedule-clearing fix at all, the
// eligibility legs alone must already deny the claim.
func TestClaimItemForCleanup_Integration_RepairedItemIsNotClaimable(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)

	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	require.True(t, claimed, "precondition: a scheduled, still-revoked item must be claimable, or the "+
		"negative assertion below would pass for the wrong reason")

	repairItem(t, ctx, tx, itemID)

	// The schedule is still set — repairItem is the synthetic fixture (see its doc comment), not a
	// call to the real RepairStatus, so this proves the negative result below comes from the
	// eligibility legs alone and not from ClaimItemForCleanup's own schedule check.
	var stillScheduled bool
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT cleanup_scheduled_at IS NOT NULL FROM linked_items WHERE id = $1`, itemID).Scan(&stillScheduled))
	require.True(t, stillScheduled, "precondition: this test's fixture must leave the schedule set, or "+
		"the negative claim result below would prove nothing about the eligibility legs specifically")

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

// TestGetItemsPendingCleanup_Integration_ReplacedItemIsRecoverable proves the stranded-cleanup
// backstop can still reach the rows most in need of it, now that its selection predicate reads
// plaid_removed_at instead of status (Issue #1495).
//
// A 'replaced' item's Plaid Item is STILL LIVE AND STILL BILLING — the re-link path
// (services/account_service.go, "re-link detected, soft-deleting old linked item") marks the OLD item
// 'replaced' WITHOUT ever calling /item/remove — and this query is its ONLY recovery path: the
// candidate sweep requires cleanup_scheduled_at IS NULL and excludes 'replaced' on top of that. So a
// stamped, overdue 'replaced' row with no recorded Plaid removal must be selected. That half is
// unchanged from Issue #1486 follow-up fix round 1, which introduced this test.
//
// WHAT ISSUE #1495 ADDED, and why the fourth row below matters. The predicate used to exclude
// 'disconnected' by status, which was a proxy for "Plaid is already done" — a proxy that could not
// express the fact for a 'replaced' row at all (MarkItemsDisconnected refuses to overwrite
// 'replaced'), so such a row was re-selected on every single run even when its Plaid side WAS done,
// costing one /item/remove per run that answers ITEM_NOT_FOUND. The predicate now reads the fact
// directly, so the fourth row — 'replaced' AND stamped — must be EXCLUDED. Without that row this test
// would still pass against the old status-based predicate and would prove nothing about the change.
func TestGetItemsPendingCleanup_Integration_ReplacedItemIsRecoverable(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	// Four rows, all stamped for cleanup and overdue — so the assertions below distinguish the
	// plaid_removed_at predicate from the stamp/overdue predicate rather than merely from row existence.
	replacedID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	disconnectedID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	liveID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	replacedAndRemovedID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)

	// Exactly what a re-link leaves behind on the OLD item: 'replaced', Plaid never called, so
	// plaid_removed_at stays NULL.
	_, err := tx.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, replacedID)
	require.NoError(t, err)
	// A completed cleanup, written the way production writes it (MarkPlaidRemoved then
	// MarkItemsDisconnected): both the stamp and the status.
	_, err = tx.Exec(ctx, `
		UPDATE linked_items SET status = 'disconnected', plaid_removed_at = NOW() WHERE id = $1`, disconnectedID)
	require.NoError(t, err)
	// Issue #1495's new case, and the one the old status-based predicate could not express: a re-link
	// leftover whose Plaid removal HAS since been recorded. 'replaced' is retained (the re-link chain
	// must survive) and the stamp is present.
	_, err = tx.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), plaid_removed_at = NOW() WHERE id = $1`,
		replacedAndRemovedID)
	require.NoError(t, err)

	items, err := repo.GetItemsPendingCleanup(ctx)
	require.NoError(t, err)

	ids := make(map[string]domain.LinkedItemStatus, len(items))
	for _, it := range items {
		ids[it.ID] = it.Status
	}

	assert.Contains(t, ids, replacedID,
		"a stamped, overdue 'replaced' item is STILL LIVE at Plaid (the re-link path never calls "+
			"/item/remove) and the candidate sweep can never re-select it, so the backstop is its only "+
			"recovery path — excluding it here left a billable Item with no live job indefinitely")
	assert.Contains(t, ids, liveID,
		"precondition: an ordinary stamped, overdue live item must still be selected, or the "+
			"assertions above and below pass for the wrong reason")
	assert.NotContains(t, ids, disconnectedID,
		"a row whose Plaid removal is recorded must not be re-enqueued — it would spin forever on work "+
			"that is already done. This exclusion is load-bearing and must stay; only its BASIS changed, "+
			"from the 'disconnected' status to the plaid_removed_at stamp that status stood in for")
	assert.NotContains(t, ids, replacedAndRemovedID,
		"Issue #1495: a 'replaced' row whose Plaid removal IS recorded must now be excluded too. Under the "+
			"old status-based predicate this row was re-selected on every run forever, because status had no "+
			"slot left to record the removal — that is the loop this column closed, and this assertion is "+
			"what distinguishes the new predicate from the old one")
}

// TestGetItemsPendingCleanup_Integration_NullStatusItemIsSelected pins that GetItemsPendingCleanup is
// STATUS-AGNOSTIC: a stamped, overdue row is selected while its Plaid removal is unrecorded, whatever
// its status — including NULL.
//
// ⚠ ISSUE #1495 CHANGED WHAT THIS TEST PINS, and the honest statement of that matters more than
// keeping the old wording. It was written (fix round 2, item 2) to pin the `status IS NULL OR` arm of
// the predicate `(status IS NULL OR status NOT IN ('disconnected'))`: without that arm, `NULL NOT IN
// ('disconnected')` is UNKNOWN rather than TRUE under SQL three-valued logic, so a NULL-status row
// silently vanished from the stranded-cleanup backstop — still stamped, still billing at Plaid, and
// invisible to its only recovery path. THAT ARM NO LONGER EXISTS: the predicate is now
// `plaid_removed_at IS NULL`, which reads no status at all and is a two-valued IS NULL test, so the
// three-valued-logic hole cannot arise here in any spelling.
//
// The test is KEPT, not deleted, because the property it now pins is still worth pinning and is
// cheap: nothing about a row's status may re-enter this predicate. A future "consistency" edit that
// re-added a status filter here would exclude NULL-status rows again (if written bare) or reintroduce
// the 'disconnected'-as-proxy-for-done confusion (if written NULL-safely), and this test fails on the
// first of those. What it can no longer claim is to be the guard for an arm that has been removed.
//
// ⚠ READ THIS BEFORE CONCLUDING ANYTHING ABOUT PRODUCTION: since migration
// 000126_set_linked_items_status_not_null (Issue #1444), linked_items.status is NOT NULL, so a
// genuinely NULL-status row CANNOT be constructed against the real schema and this state is
// UNREACHABLE today. Do not read a failure here as an outage.
//
// The DROP NOT NULL / rollback technique is borrowed verbatim from
// TestJobHealthRepository_Integration_MarkedUserWithNullStatusLiveItem_Counted
// (job_health_repository_plaid_cleanup_integration_test.go). The constraint is dropped INSIDE this
// test's own transaction and never committed, so the real schema is never permanently altered — and
// the user, the item and the query under test must all live in that SAME transaction, or a query on
// another connection could not see the uncommitted NULL-status row. newCleanupPageTestTx already
// builds its repositories on the tx and registers the rollback, so that requirement is satisfied by
// construction here.
//
// Falsifiability: adding `AND (status IS NULL OR status NOT IN ('disconnected'))` back to the query is
// NOT enough to fail this test (the row's status is NULL, which that arm admits); adding the bare
// `AND status NOT IN ('disconnected')` form IS, and so is any `AND status = ...` filter. Verified with
// the bare form.
func TestGetItemsPendingCleanup_Integration_NullStatusItemIsSelected(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	// Rolled back with the transaction — see the doc comment. Must come before the seed, because the
	// item is inserted with a non-NULL status and only then relaxed to NULL.
	_, err := tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	// A control row in the same transaction, left at its seeded 'revoked' status, so a failure below
	// distinguishes "the NULL arm is missing" from "this query returned nothing at all".
	controlID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)

	_, err = tx.Exec(ctx, `UPDATE linked_items SET status = NULL WHERE id = $1`, itemID)
	require.NoError(t, err)

	// Confirm the fixture really is NULL at the column level. domain.LinkedItem cannot express it —
	// normalizeLinkedItemStatus maps NULL to 'active' during the scan — so asserting on the returned
	// struct could never tell a NULL row from an 'active' one, and this test would pass for the wrong
	// reason without this check.
	var statusIsNull bool
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT status IS NULL FROM linked_items WHERE id = $1`, itemID).Scan(&statusIsNull))
	require.True(t, statusIsNull, "fixture precondition: the row must genuinely have a NULL status, or "+
		"this test cannot exercise the arm it exists to pin")

	items, err := repo.GetItemsPendingCleanup(ctx)
	require.NoError(t, err)

	ids := make(map[string]struct{}, len(items))
	for _, it := range items {
		ids[it.ID] = struct{}{}
	}

	assert.Contains(t, ids, itemID,
		"a stamped, overdue NULL-status item must be SELECTED: NULL is not 'disconnected', the row is "+
			"still live and still billing at Plaid, and the backstop is its only recovery path. Without "+
			"the `status IS NULL OR` arm, `NULL NOT IN ('disconnected')` is UNKNOWN rather than TRUE and "+
			"the row silently vanishes from that path")
	assert.Contains(t, ids, controlID,
		"control: an ordinary stamped, overdue row must still be selected, so the assertion above "+
			"cannot pass or fail for reasons unrelated to the NULL arm")
}

// TestClaimItemForCleanup_Integration_RealisticReconnectSequence_SurvivesStaleSyncCleanup is the
// P3-T8 (Issue #1609) regression for a HIGH finding: `repairItem` above simulates a repair with raw
// SQL that ALSO refreshes `last_synced_at`, which no production code path does — RepairStatus
// (linked_item_repository.go) never touched last_synced_at — so every other test in this file was
// blind to the stale-sync leg of cleanupCandidateReasonPredicate surviving a real repair.
//
// This test drives the ACTUAL production sequence instead of a hand-rolled row state:
//  1. the connection breaks (status = login_required) and sits broken long enough to go stale on
//     BOTH the error leg and the unconditional last_synced_at leg;
//  2. the sweep schedules it (MarkForCleanup, mirroring ItemCleanupCheckWorker.Work);
//  3. the user repairs it through the real reconnect flow — repo.RepairStatus, the exact call
//     RepairItem/HandleLoginRepaired make — WITHOUT touching last_synced_at;
//  4. ClaimItemForCleanup, the recheck ItemCleanupWorker.Work performs immediately before the
//     irreversible Plaid /item/remove, must decline.
//
// Before Issue #1609 P3-T8, RepairStatus cleared only status/error_code/error_message/last_error_at
// and left cleanup_scheduled_at set — so this test failed (claimed == true) even though the item was
// freshly repaired, because the stale-sync leg never stopped matching. The fix clears
// cleanup_notified_at/cleanup_scheduled_at in the same atomic UPDATE, which denies the claim via
// ClaimItemForCleanup's own `cleanup_scheduled_at IS NOT NULL` precondition regardless of which leg
// of cleanupCandidateReasonPredicate would otherwise still match.
func TestClaimItemForCleanup_Integration_RealisticReconnectSequence_SurvivesStaleSyncCleanup(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	itemID := seedClaimCleanupItem(t, ctx, tx, repo, profileRepo)
	var userID string
	require.NoError(t, tx.QueryRow(ctx, `SELECT user_id FROM linked_items WHERE id = $1`, itemID).Scan(&userID))

	// Step 1: the connection breaks and goes stale past BOTH thresholds. Deliberately past the
	// error-leg threshold too, so a fix that only patched the status-scoped legs (rather than the
	// unconditional stale-sync leg) would still show this test red.
	_, err := tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'login_required', error_code = 'ITEM_LOGIN_REQUIRED', error_message = 'boom',
		    last_error_at = NOW() - INTERVAL '31 days',
		    last_synced_at = NOW() - INTERVAL '61 days',
		    cleanup_scheduled_at = NULL, cleanup_notified_at = NULL
		WHERE id = $1`, itemID)
	require.NoError(t, err)

	// Step 2: the sweep schedules it — exactly what MarkForCleanup does, and nothing else.
	require.NoError(t, repo.MarkForCleanup(ctx, itemID, time.Now()))

	// Step 3: the user repairs it through the real reconnect flow.
	repaired, err := repo.RepairStatus(ctx, itemID, userID, []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired}, nil)
	require.NoError(t, err)
	require.True(t, repaired, "precondition: the repair itself must succeed, or the decline below would "+
		"prove nothing about cleanup eligibility")

	var status string
	var lastSyncedAt *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT status, last_synced_at FROM linked_items WHERE id = $1`, itemID).Scan(&status, &lastSyncedAt))
	assert.Equal(t, "active", status)
	require.NotNil(t, lastSyncedAt)
	assert.True(t, lastSyncedAt.Before(time.Now().Add(-59*24*time.Hour)),
		"RepairStatus must NOT refresh last_synced_at — asserting a sync that never happened would "+
			"corrupt the column's meaning for every other reader. This test's whole point is that the "+
			"item survives cleanup DESPITE last_synced_at staying stale.")

	// Step 4: the realistic sequence's payoff — the recheck immediately before the irreversible
	// Plaid /item/remove must decline, even though last_synced_at is still 61 days stale.
	claimed, err := repo.ClaimItemForCleanup(ctx, itemID, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	assert.False(t, claimed, "a just-repaired item must survive the cleanup sweep even though its "+
		"last_synced_at is still stale — RepairStatus never refreshes that column, so ClaimItemForCleanup "+
		"must not be relying on it having moved")
}
