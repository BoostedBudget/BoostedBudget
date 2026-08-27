//go:build integration

package repository

// #1569 T2 — a deleted goal's contributions must remain readable.
//
// Task 8 turned DELETE /savings-goals/{id} into a tombstone and told the user, in the endpoint's
// own description, that it "Does NOT destroy any data — every contribution logged against the goal
// is retained." That sentence was true about the rows and false about the user's experience:
// ListByGoal joins `savings_goals g ... AND g.deleted_at IS NULL`, so
// GET /savings-goals/{goalID}/contributions answered 200 with an empty array for a deleted goal —
// indistinguishable from a goal that never had a contribution. There is no restore path anywhere on
// this branch, so the itemised entries (dates, amounts, sources, and money the user typed in BY
// HAND, which no provider can supply again) could never be read again. Retention that reads as
// deletion is the exact failure this project named itself for.
//
// The fix mirrors GetDeletedByUserID: a SEPARATE, tombstone-exempt read whose inverted predicate IS
// its purpose, used only when the caller deliberately names a deleted goal. It is emphatically NOT
// a relaxation of the live reads — widening SumConfirmedByGoal or SumConfirmedByGoalInPeriod would
// resurrect a deleted goal into progress recomputation and into budget "spent" totals. This file
// asserts BOTH directions: the new read sees the tombstoned goal, and every pre-existing read
// still does not.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavingsContributions_DeletedGoal_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	linkedItemID := uuid.New().String()
	otherItemID := uuid.New().String()
	accountID := uuid.New().String()
	otherAccountID := uuid.New().String()
	liveGoalID := uuid.New().String()
	deletedGoalID := uuid.New().String()
	otherGoalID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	for _, u := range []string{userID, otherUserID} {
		mustExec("user", `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u, "test|contrib-del-"+u, "contrib-del-"+u+"@example.com")
	}
	for _, a := range []struct{ item, account, user string }{
		{linkedItemID, accountID, userID},
		{otherItemID, otherAccountID, otherUserID},
	} {
		mustExec("linked_item", `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1, $2, $3, 'enc-test-token')
		`, a.item, a.user, "contrib-del-item-"+a.item)
		mustExec("account", `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
			VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active')
		`, a.account, a.item, a.user, "plaid-contrib-del-"+a.account)
	}

	for _, g := range []struct{ id, user, account, name string }{
		{liveGoalID, userID, accountID, "Still Saving"},
		{deletedGoalID, userID, accountID, "Abandoned Trip"},
		{otherGoalID, otherUserID, otherAccountID, "Someone Else's"},
	} {
		mustExec("savings_goal", `
			INSERT INTO savings_goals (id, user_id, name, target_amount, current_amount, account_id, progress_basis)
			VALUES ($1, $2, $3, 5000, 100, $4, 'contributions')
		`, g.id, g.user, g.name, g.account)
	}

	// seedContrib writes one contribution and returns its id. occurredDaysAgo drives the ordering
	// assertion; status drives the confirmed-only sum.
	//
	// The (source, status, managed_by_reconciliation, confirmed_by_user) tuples below are the two
	// shapes production actually writes — a hand-logged row (Insert: source 'manual',
	// managed=false, confirmed_by_user=true) and a reconciler row (UpsertProposedFromTransaction:
	// source 'confirmed_transfer', managed=true, confirmed_by_user=false until ConfirmContribution
	// flips it). Inventing a source string here is not a shortcut: `valid_contribution_source`
	// rejected the first draft of this fixture outright, and a fixture the application could never
	// produce proves nothing even when it inserts.
	seedContrib := func(goalID, source, status string, managed bool, amount float64, occurredDaysAgo int) string {
		id := uuid.New().String()
		mustExec("savings_goal_contribution", `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, amount, source, status, managed_by_reconciliation,
				 confirmed_by_user, occurred_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_DATE - $8::int)
		`, id, goalID, amount, source, status, managed, status == "confirmed", occurredDaysAgo)
		return id
	}

	// The deleted goal: two confirmed (one hand-typed) and one still proposed. 400 + 500 = 900.
	deletedManualID := seedContrib(deletedGoalID, "manual", "confirmed", false, 400, 1)
	deletedTransferID := seedContrib(deletedGoalID, "confirmed_transfer", "confirmed", true, 500, 5)
	deletedProposedID := seedContrib(deletedGoalID, "confirmed_transfer", "proposed", true, 250, 3)

	// The live goal, whose reads must be completely unaffected in either direction.
	liveContribID := seedContrib(liveGoalID, "manual", "confirmed", false, 111, 2)

	// The other user's goal is ALSO deleted and ALSO carries a contribution, precisely so that a
	// read keyed on `deleted_at IS NOT NULL` alone — tenancy dropped — would return it.
	otherContribID := seedContrib(otherGoalID, "manual", "confirmed", false, 777, 1)

	goalRepo := NewSavingsGoalRepository(tx)
	require.NoError(t, goalRepo.Delete(ctx, deletedGoalID, userID, nil))
	require.NoError(t, goalRepo.Delete(ctx, otherGoalID, otherUserID, nil))

	repo := NewSavingsContributionRepository(tx)

	// --- 1. The deleted goal's entries are retrievable, itemised, newest-first. ---------------
	got, err := repo.ListByDeletedGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	require.Len(t, got, 3, "every contribution logged against the deleted goal, proposed included")

	ids := make([]string, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{deletedManualID, deletedProposedID, deletedTransferID}, ids,
		"ordered by occurred_on DESC, exactly as ListByGoal orders a live goal")

	// The hand-typed row is the one that can never be refetched: assert its detail, not just count.
	assert.Equal(t, "manual", got[0].Source)
	assert.Equal(t, 400.0, got[0].Amount)
	assert.Equal(t, "confirmed", got[0].Status)
	// NotEmpty would pass for any string the scan happened to produce; the fixture seeded
	// CURRENT_DATE - 1, so assert THAT date. Expected is read back from the same session so the
	// comparison uses the same UTC calendar day the fixture's CURRENT_DATE resolved against.
	var expectedOccurredOn string
	require.NoError(t, tx.QueryRow(ctx, `SELECT (CURRENT_DATE - 1)::text`).Scan(&expectedOccurredOn))
	assert.Equal(t, expectedOccurredOn, got[0].OccurredOn,
		"the date the user recorded must survive the tombstone unaltered")

	// --- 2. ...and correctly summed (confirmed only, proposed excluded). ----------------------
	sum, err := repo.SumConfirmedByDeletedGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	assert.InDelta(t, 900.0, sum, 0.001, "400 + 500 confirmed; the 250 proposed row does not count")

	// --- 3. The live goal's reads are UNCHANGED, and the new read does not serve it. ----------
	liveList, err := repo.ListByGoal(ctx, userID, liveGoalID)
	require.NoError(t, err)
	require.Len(t, liveList, 1)
	assert.Equal(t, liveContribID, liveList[0].ID)

	liveSum, err := repo.SumConfirmedByGoal(ctx, userID, liveGoalID)
	require.NoError(t, err)
	assert.InDelta(t, 111.0, liveSum, 0.001)

	livePeriodSum, err := repo.SumConfirmedByGoalInPeriod(ctx, userID, liveGoalID,
		time.Now().AddDate(0, 0, -30), time.Now().AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.InDelta(t, 111.0, livePeriodSum, 0.001)

	fromLiveViaDeletedRead, err := repo.ListByDeletedGoal(ctx, userID, liveGoalID)
	require.NoError(t, err)
	assert.Empty(t, fromLiveViaDeletedRead,
		"the tombstone-exempt read is the INVERSE, not a superset — a live goal is not its business")

	liveSumViaDeletedRead, err := repo.SumConfirmedByDeletedGoal(ctx, userID, liveGoalID)
	require.NoError(t, err)
	assert.Zero(t, liveSumViaDeletedRead)

	// --- 4. The deleted goal stays OUT of every pre-existing read (no widening). --------------
	// This is the direction that matters most: if someone "fixes" the defect by relaxing the live
	// predicates instead of adding a separate read, a deleted goal silently re-enters progress
	// recomputation (SumConfirmedByGoal) and budget "spent" totals (…InPeriod).
	deletedViaLiveList, err := repo.ListByGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	assert.Empty(t, deletedViaLiveList, "the ordinary listing still hides a tombstoned goal")

	deletedViaLiveSum, err := repo.SumConfirmedByGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	assert.Zero(t, deletedViaLiveSum, "a deleted goal must not resurrect into progress recomputation")

	deletedViaPeriodSum, err := repo.SumConfirmedByGoalInPeriod(ctx, userID, deletedGoalID,
		time.Now().AddDate(0, 0, -30), time.Now().AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Zero(t, deletedViaPeriodSum, "a deleted goal must not resurrect into budget spent totals")

	// --- 5. Tenancy holds — the tombstone predicate is inverted, the ownership one is not. ----
	crossUser, err := repo.ListByDeletedGoal(ctx, userID, otherGoalID)
	require.NoError(t, err)
	assert.Empty(t, crossUser, "one user cannot read another user's deleted goal's contributions")

	crossUserSum, err := repo.SumConfirmedByDeletedGoal(ctx, userID, otherGoalID)
	require.NoError(t, err)
	assert.Zero(t, crossUserSum)

	// The other user's own row is genuinely there — proving the empty result above is a tenancy
	// refusal and not a fixture that failed to insert (a vacuous-test guard).
	ownersView, err := repo.ListByDeletedGoal(ctx, otherUserID, otherGoalID)
	require.NoError(t, err)
	require.Len(t, ownersView, 1)
	assert.Equal(t, otherContribID, ownersView[0].ID)
}
