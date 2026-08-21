//go:build integration

package repository

// DB-backed behavior for GetSnapshotsAsOfForUser (#1537).
//
// This exists because pgxmock cannot tell whether the query is CORRECT — it matches query strings
// and returns canned rows, so a DISTINCT ON whose expression disagrees with its ORDER BY prefix, or
// a cutoff comparison against the wrong column, looks identical to a working one. The bug this
// method replaces (GetByUserIDAndDateRange with both bounds at midnight, which matches no
// wall-clock snapshot) survived years of unit tests for exactly that reason, so the replacement is
// asserted against a real planner and real rows.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/ -run TestGetSnapshotsAsOfForUser -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSnapshotsAsOfForUser_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "asof-test-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "asof-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	// One logical account (user_accounts row) behind a re-link chain of two accounts rows, plus a
	// second standalone account whose snapshots carry a NULL user_account_id — the legacy shape.
	userAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, primary_type)
		VALUES ($1, $2, 'ins_asof_test', '1111', 'depository')
	`, userAccountID, userID)
	require.NoError(t, err, "seed user_account")

	oldAcctID, newAcctID, legacyAcctID := uuid.New(), uuid.New(), uuid.New()
	for _, a := range []struct {
		id            uuid.UUID
		plaidID       string
		userAccountID any
		status        string
		replacedAt    any
	}{
		{oldAcctID, "plaid-asof-old", userAccountID, "replaced", time.Now().UTC()},
		{newAcctID, "plaid-asof-new", userAccountID, "active", nil},
		{legacyAcctID, "plaid-asof-legacy", nil, "active", nil},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, user_account_id, status, replaced_at)
			VALUES ($1, $2, $3, $4, 'Test Account', 'depository', $5, $6, $7)
		`, a.id, linkedItemID, userID, a.plaidID, a.userAccountID, a.status, a.replacedAt)
		require.NoError(t, err, "seed account "+a.plaidID)
	}

	// Wall-clock instants, never midnight — the shape production actually writes.
	day := func(daysAgo int, h, m, s int) time.Time {
		return time.Now().UTC().AddDate(0, 0, -daysAgo).Truncate(24 * time.Hour).
			Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	chainOld := day(31, 14, 32, 7) // pre-relink row, oldest
	chainMid := day(30, 23, 30, 0) // late on the baseline day
	chainNew := day(1, 8, 15, 0)   // after the baseline cutoff
	legacyAt := day(30, 14, 32, 7) // NULL user_account_id, on the baseline day
	legacyLater := day(2, 9, 0, 0) // after the baseline cutoff

	for _, s := range []struct {
		acct          uuid.UUID
		userAccountID any
		bal           float64
		at            time.Time
	}{
		{oldAcctID, userAccountID, 100, chainOld},
		{newAcctID, userAccountID, 400, chainMid},
		{newAcctID, userAccountID, 900, chainNew},
		{legacyAcctID, nil, 50, legacyAt},
		{legacyAcctID, nil, 75, legacyLater},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO balance_snapshots (account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
			VALUES ($1, $2, $3, $4, 'USD', $5, 'manual')
		`, s.acct, s.userAccountID, userID, s.bal, s.at)
		require.NoError(t, err, "seed balance_snapshot")
	}

	repo := NewBalanceSnapshotRepository(tx)

	// Cutoff = end of the day 30 days ago: exactly what the dashboard baseline asks for.
	baselineCutoff := day(30, 0, 0, 0).Add(24*time.Hour - time.Nanosecond)
	baseline, err := repo.GetSnapshotsAsOfForUser(ctx, userID.String(), baselineCutoff)
	require.NoError(t, err, "GetSnapshotsAsOfForUser must execute valid SQL")

	byKey := map[string]float64{}
	for _, s := range baseline {
		key := s.AccountID
		if s.UserAccountID != nil {
			key = *s.UserAccountID
		}
		_, dup := byKey[key]
		assert.False(t, dup, "one row per chain key: %s appeared twice", key)
		require.NotNil(t, s.CurrentBalance)
		byKey[key] = *s.CurrentBalance
	}

	require.Len(t, byKey, 2, "one row for the re-link chain, one for the legacy NULL account")
	assert.InDelta(t, 400.0, byKey[userAccountID.String()], 0.01,
		"the chain's latest row AT OR BEFORE the cutoff is the 23:30 one on the baseline day (400). "+
			"100 means the pre-relink row won; 900 means the cutoff was not applied at all.")
	assert.InDelta(t, 50.0, byKey[legacyAcctID.String()], 0.01,
		"a NULL user_account_id row keys on its raw account_id and is still bounded by the cutoff")

	// A cutoff before every snapshot returns nothing rather than erroring — the degenerate case the
	// old zero-width window hit for every real user.
	empty, err := repo.GetSnapshotsAsOfForUser(ctx, userID.String(), day(60, 0, 0, 0))
	require.NoError(t, err)
	assert.Empty(t, empty, "no snapshot at or before the cutoff means no rows")

	// And "now" agrees with the unbounded GetLatestByUserID, which is what makes omitting
	// as_of_date backward-compatible.
	current, err := repo.GetSnapshotsAsOfForUser(ctx, userID.String(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	latest, err := repo.GetLatestByUserID(ctx, userID.String())
	require.NoError(t, err)
	require.Len(t, current, len(latest))
	currentByID := map[string]float64{}
	for _, s := range current {
		currentByID[s.ID] = *s.CurrentBalance
	}
	for _, s := range latest {
		got, ok := currentByID[s.ID]
		assert.True(t, ok, "GetSnapshotsAsOfForUser(now) must return the same rows as GetLatestByUserID")
		assert.InDelta(t, *s.CurrentBalance, got, 0.01)
	}
}
