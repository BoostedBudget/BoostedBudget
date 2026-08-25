//go:build integration

package repository

// Integration tests for savings impact repository.
// These tests execute SQL against a real Postgres planner and catch type/aggregate
// errors that pgxmock cannot detect (e.g., ON CONFLICT expression-index mismatches,
// aggregate JOIN + filter correctness — Issue #972).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestWriteSnapshot -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteSnapshot_OnConflictUpsert_Integration proves the ON CONFLICT
// ((DATE(computed_at AT TIME ZONE 'UTC'))) expression exactly matches the unique
// index created by migration 000092. pgxmock cannot catch expression-index
// target mismatches because it never sends SQL to a real planner.
// This is the regression guard for the Codex HIGH bug (wrong expression target).
func TestWriteSnapshot_OnConflictUpsert_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepository(tx)

	// First write: snapshot at 09:00 UTC on the test day.
	day1 := time.Date(2024, 3, 15, 9, 0, 0, 0, time.UTC)
	snap1 := &domain.SavingsImpactSnapshot{
		ComputedAt:     day1,
		TotalAmount:    1000.00,
		UserCount:      5,
		CompletedCount: 10,
	}
	err = repo.WriteSnapshot(ctx, snap1)
	require.NoError(t, err, "first WriteSnapshot must succeed")

	// Second write: same UTC calendar day (different wall-clock time), different totals.
	// The ON CONFLICT clause must upsert, not insert a duplicate.
	day2 := time.Date(2024, 3, 15, 18, 30, 0, 0, time.UTC)
	snap2 := &domain.SavingsImpactSnapshot{
		ComputedAt:     day2,
		TotalAmount:    1100.00,
		UserCount:      6,
		CompletedCount: 11,
	}
	err = repo.WriteSnapshot(ctx, snap2)
	require.NoError(t, err, "second WriteSnapshot on same UTC date must not error (ON CONFLICT upsert)")

	// Verify exactly one row exists for this UTC date.
	var rowCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM savings_impact_snapshots
		 WHERE DATE(computed_at AT TIME ZONE 'UTC') = DATE($1 AT TIME ZONE 'UTC')`,
		day1,
	).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount,
		"ON CONFLICT upsert must produce exactly one row per UTC date (not two)")

	// Verify the upserted row holds the second write's values.
	// Query by the specific UTC date rather than GetLatestSnapshot, which could return
	// a row from a different date if the test DB already has more-recent committed snapshots.
	var gotTotal float64
	var gotUsers, gotCompleted int
	err = tx.QueryRow(ctx, `
		SELECT total_amount, user_count, completed_count
		FROM savings_impact_snapshots
		WHERE DATE(computed_at AT TIME ZONE 'UTC') = DATE($1 AT TIME ZONE 'UTC')
	`, day1).Scan(&gotTotal, &gotUsers, &gotCompleted)
	require.NoError(t, err)
	assert.InDelta(t, 1100.00, gotTotal, 0.01,
		"upserted snapshot must reflect second write's total_amount")
	assert.Equal(t, 6, gotUsers,
		"upserted snapshot must reflect second write's user_count")
	assert.Equal(t, 11, gotCompleted,
		"upserted snapshot must reflect second write's completed_count")
}

// TestGetAggregateSavingsStats_Integration proves the aggregate query correctly
// applies the include_in_aggregate_stats privacy JOIN filter and counts only
// completed suggestions with non-NULL actual_amount. Uses a delta approach
// relative to the pre-seed baseline so the test is safe against non-empty DBs.
func TestGetAggregateSavingsStats_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepository(tx)

	// Capture baseline before seeding so the test is safe against non-empty DBs.
	baseTotal, baseUsers, baseCompleted, err := repo.GetAggregateSavingsStats(ctx)
	require.NoError(t, err, "baseline aggregate query must not error")

	// Seed two users: one with consent, one without.
	userConsentedID := uuid.New()
	userNoConsentID := uuid.New()
	for _, row := range []struct {
		id      uuid.UUID
		consent bool
	}{
		{userConsentedID, true},
		{userNoConsentID, false},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, row.id, "test|"+row.id.String(), "int-agg-"+row.id.String()+"@example.com")
		require.NoError(t, err, "seed user")

		_, err = tx.Exec(ctx, `
			INSERT INTO privacy_settings (user_id, include_in_aggregate_stats)
			VALUES ($1, $2)
		`, row.id, row.consent)
		require.NoError(t, err, "seed privacy_settings")
	}

	// Seed 2 completed suggestions for the consented user (100 + 200 = 300 total).
	now := time.Now().UTC()
	for _, amount := range []float64{100.00, 200.00} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, completed_at, actual_amount, completion_source, created_at, updated_at)
			VALUES ($1, $2, $3, 'boost', 'completed', $4, $3, 'exact_match', $4, $4)
		`, uuid.New(), userConsentedID, amount, now)
		require.NoError(t, err, "seed completed suggestion for consented user")
	}

	// Seed 1 pending suggestion for the consented user (must NOT count toward the aggregate).
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 50.00, 'boost', 'pending', $3, $3)
	`, uuid.New(), userConsentedID, now)
	require.NoError(t, err, "seed pending suggestion for consented user")

	// Seed 1 completed suggestion for the non-consented user (must NOT count toward the aggregate).
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, completed_at, actual_amount, completion_source, created_at, updated_at)
		VALUES ($1, $2, 50.00, 'boost', 'completed', $3, 50.00, 'exact_match', $3, $3)
	`, uuid.New(), userNoConsentID, now)
	require.NoError(t, err, "seed completed suggestion for non-consented user")

	// Run the aggregate — delta vs. baseline proves the JOIN + filter logic.
	total, users, completed, err := repo.GetAggregateSavingsStats(ctx)
	require.NoError(t, err, "GetAggregateSavingsStats must not error after seeding")

	assert.InDelta(t, 300.00, total-baseTotal, 0.01,
		"aggregate must include only consented, completed suggestions (100+200=300)")
	assert.Equal(t, 1, users-baseUsers,
		"aggregate must count only one consented user")
	assert.Equal(t, 2, completed-baseCompleted,
		"aggregate must count exactly two completed+consented suggestions")
}

// TestGetUserImpact_Integration exercises the month/prev-month FILTER SQL against a
// real Postgres planner. pgxmock uses too-loose string matching and cannot verify
// that FILTER (WHERE ...) clauses correctly scope to calendar month boundaries.
//
// Issue #972: C5 test rigor — savings_impact_repository.GetUserImpact SQL validation.
func TestGetUserImpact_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepository(tx)

	// Seed a user. No privacy_settings needed — GetUserImpact doesn't filter by consent.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "user-impact-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Compute month boundaries relative to NOW() (same logic as the repo).
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevMonthStart := monthStart.AddDate(0, -1, 0)

	// thisMonthTime: a timestamp within the current month.
	thisMonthTime := monthStart.Add(24 * time.Hour)
	// prevMonthTime: a timestamp within the previous month.
	prevMonthTime := prevMonthStart.Add(24 * time.Hour)
	// olderTime: two months ago — should count toward TotalAmount but not month/prevMonth.
	olderTime := prevMonthStart.AddDate(0, -1, 0)

	insertCompleted := func(amount float64, completedAt time.Time) string {
		id := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, completed_at, actual_amount, completion_source, created_at, updated_at)
			VALUES ($1, $2, $3, 'boost', 'completed', $4, $3, 'exact_match', $4, $4)
		`, id, userID, amount, completedAt)
		require.NoError(t, err)
		return id
	}

	// This month: 100 + 150 = 250
	insertCompleted(100.00, thisMonthTime)
	insertCompleted(150.00, thisMonthTime)

	// Previous month: 80
	insertCompleted(80.00, prevMonthTime)

	// Two months ago: 50 (counts toward total but not month/prevMonth).
	insertCompleted(50.00, olderTime)

	// Pending suggestion: must NOT appear in any total (status != 'completed').
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 75.00, 'boost', 'pending', $3, $3)
	`, uuid.New(), userID, now)
	require.NoError(t, err)

	impact, err := repo.GetUserImpact(ctx, userID.String())
	require.NoError(t, err, "GetUserImpact must not error")
	require.NotNil(t, impact)

	// TotalAmount = 100 + 150 + 80 + 50 = 380 (all completed, no pending).
	assert.InDelta(t, 380.00, impact.TotalAmount, 0.01,
		"TotalAmount must sum ALL completed suggestions regardless of month")

	// CompletedCount = 4 (the four completed inserts; pending excluded).
	assert.Equal(t, 4, impact.CompletedCount,
		"CompletedCount must count exactly four completed suggestions")

	// MonthAmount = 100 + 150 = 250 (only this-month completions).
	assert.InDelta(t, 250.00, impact.MonthAmount, 0.01,
		"MonthAmount FILTER clause must include only current-month completions")

	// PrevMonthAmount = 80 (only prev-month completions).
	assert.InDelta(t, 80.00, impact.PrevMonthAmount, 0.01,
		"PrevMonthAmount FILTER clause must include only previous-month completions")

	// RecentItems: at most 5, ordered by completed_at DESC — all 4 items fit.
	assert.Len(t, impact.RecentItems, 4,
		"RecentItems must return all 4 completed suggestions (within the 5-item limit)")

	// None of the items should be the pending suggestion.
	for _, item := range impact.RecentItems {
		assert.NotEqual(t, 75.00, item.ActualAmount,
			"pending suggestion (75.00) must never appear in RecentItems")
	}
}

// TestGetUserImpact_DestinationAccountName_Integration validates that GetUserImpact
// resolves the human-readable destination account name for each of the four cases:
//
//	(a) to_account_id → Plaid-linked account: resolves COALESCE(nickname, name)
//	(b) to_user_account_id → manual account with plaintext nickname: resolves nickname
//	(c) to_user_account_id → manual account with ONLY encrypted account_name_hint: decrypts hint
//	(d) to_user_account_id → manual account with neither nickname nor hint: ToAccountName == nil
//
// This must be a real-PG test because pgxmock cannot validate JOIN correctness or
// in-Go decrypt logic combined with the SQL result shape.
// GAP CA (#972) — transfers can target any account type, so the name must be real.
func TestGetUserImpact_DestinationAccountName_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Use the standard test FieldEncryptor (testEncryptionService: TENC[...] format).
	enc := newTestFieldEncryptor(t)

	repo := NewSavingsImpactRepositoryWithEncryptor(tx, enc)

	// Seed a user.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-ca-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed a linked_item and accounts row for case (a) Plaid account.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token-ca')
	`, linkedItemID, userID, "test-item-ca-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	plaidAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, nickname)
		VALUES ($1, $2, $3, 'plaid-ca-savings', 'Plaid Savings', 'depository', 'My Savings')
	`, plaidAccountID, linkedItemID, userID)
	require.NoError(t, err, "seed Plaid account with nickname")

	// Seed user_accounts rows for cases (b), (c), (d).
	uaWithNicknameID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, nickname)
		VALUES ($1, $2, 'inst-ca-b', 'Manual Savings')
	`, uaWithNicknameID, userID)
	require.NoError(t, err, "seed manual account with nickname (case b)")

	// Encrypt the account_name_hint for case (c).
	encryptedHint, errEnc := enc.EncryptString("Ally Savings Account")
	require.NoError(t, errEnc, "encrypt account_name_hint for case (c)")
	uaWithHintID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, account_name_hint)
		VALUES ($1, $2, 'inst-ca-c', $3)
	`, uaWithHintID, userID, encryptedHint)
	require.NoError(t, err, "seed manual account with encrypted hint (case c)")

	uaNoNameID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'inst-ca-d')
	`, uaNoNameID, userID)
	require.NoError(t, err, "seed manual account with no name (case d)")

	now := time.Now().UTC()
	plaidAccIDStr := plaidAccountID.String()
	uaNicknameIDStr := uaWithNicknameID.String()
	uaHintIDStr := uaWithHintID.String()
	uaNoNameIDStr := uaNoNameID.String()

	insertCompleted := func(toAccountID, toUserAccountID *string, amount float64) {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, completed_at, actual_amount, completion_source,
				 to_account_id, to_user_account_id, created_at, updated_at)
			VALUES ($1, $2, $3, 'boost', 'completed', $4, $3, 'exact_match', $5, $6, $4, $4)
		`, uuid.New(), userID, amount, now, toAccountID, toUserAccountID)
		require.NoError(t, err, "seed completed suggestion")
	}

	// (a) Plaid account with nickname → should resolve "My Savings"
	insertCompleted(&plaidAccIDStr, nil, 100.00)
	// (b) manual with nickname → should resolve "Manual Savings"
	insertCompleted(nil, &uaNicknameIDStr, 200.00)
	// (c) manual with encrypted hint → should resolve "Ally Savings Account"
	insertCompleted(nil, &uaHintIDStr, 300.00)
	// (d) manual with no name → ToAccountName should be nil
	insertCompleted(nil, &uaNoNameIDStr, 400.00)

	impact, err := repo.GetUserImpact(ctx, userID.String())
	require.NoError(t, err, "GetUserImpact must not error")
	require.Len(t, impact.RecentItems, 4, "should have 4 completed suggestions")

	// Index by actual_amount for reliable lookup (amounts are unique in this test).
	byAmount := make(map[float64]*domain.CompletedSuggestionSummary)
	for _, item := range impact.RecentItems {
		byAmount[item.ActualAmount] = item
	}

	// (a) Plaid account with nickname.
	caseA := byAmount[100.00]
	require.NotNil(t, caseA, "case (a): amount 100 must appear in RecentItems")
	require.NotNil(t, caseA.ToAccountName, "case (a): ToAccountName must be non-nil for Plaid account with nickname")
	assert.Equal(t, "My Savings", *caseA.ToAccountName,
		"case (a): should resolve accounts.nickname for a Plaid account")

	// (b) Manual account with plaintext nickname.
	caseB := byAmount[200.00]
	require.NotNil(t, caseB, "case (b): amount 200 must appear in RecentItems")
	require.NotNil(t, caseB.ToAccountName, "case (b): ToAccountName must be non-nil for manual account with nickname")
	assert.Equal(t, "Manual Savings", *caseB.ToAccountName,
		"case (b): should resolve user_accounts.nickname for a manual account")

	// (c) Manual account with only encrypted account_name_hint.
	caseC := byAmount[300.00]
	require.NotNil(t, caseC, "case (c): amount 300 must appear in RecentItems")
	require.NotNil(t, caseC.ToAccountName, "case (c): ToAccountName must be non-nil after decrypting account_name_hint")
	assert.Equal(t, "Ally Savings Account", *caseC.ToAccountName,
		"case (c): should decrypt and resolve user_accounts.account_name_hint")

	// (d) Manual account with neither nickname nor hint.
	caseD := byAmount[400.00]
	require.NotNil(t, caseD, "case (d): amount 400 must appear in RecentItems")
	assert.Nil(t, caseD.ToAccountName,
		"case (d): ToAccountName must be nil when no name is available")
}

// TestGetUserImpact_CrossUserAccountName_Integration proves GAP CF: the destination
// account JOIN in GetUserImpact must be user-scoped so that a cross-user account_id
// stored in a transfer_suggestion does NOT leak another user's account name.
//
// Without the fix (AND a.user_id = ts.user_id / AND ua.user_id = ts.user_id), the
// JOIN matches any account row with the given ID regardless of ownership and would
// return the other user's account name. This is a defense-in-depth constraint on top
// of IDOR ownership enforced at create/execute time (GAP AE).
//
// Only a real-PG test can catch this — pgxmock cannot validate JOIN predicate correctness.
func TestGetUserImpact_CrossUserAccountName_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepositoryWithEncryptor(tx, newTestFieldEncryptor(t))

	// Two users: the requester and another user whose accounts must stay private.
	requesterID := uuid.New()
	otherUserID := uuid.New()
	for _, row := range []struct {
		id    uuid.UUID
		label string
	}{
		{requesterID, "cf-requester"},
		{otherUserID, "cf-other"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, row.id, "test|"+row.id.String(), row.label+"-"+row.id.String()+"@example.com")
		require.NoError(t, err, "seed user "+row.label)
	}

	// Seed a Plaid account owned by otherUserID (not the requester).
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-cf-token')
	`, linkedItemID, otherUserID, "test-item-cf-"+linkedItemID.String())
	require.NoError(t, err, "seed other user's linked_item")

	otherAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, nickname)
		VALUES ($1, $2, $3, 'plaid-cf-savings', 'OtherUser Savings', 'depository', 'Leaked Nickname')
	`, otherAccountID, linkedItemID, otherUserID)
	require.NoError(t, err, "seed other user's Plaid account")

	// Seed a manual account owned by otherUserID.
	otherUAID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, nickname)
		VALUES ($1, $2, 'inst-cf-ua', 'OtherUser Manual Savings')
	`, otherUAID, otherUserID)
	require.NoError(t, err, "seed other user's manual account")

	// Seed completed suggestions for the REQUESTER that reference the OTHER user's account IDs.
	// This simulates a cross-user foreign-key anomaly that the JOIN predicate must guard against.
	now := time.Now().UTC()
	otherAccountIDStr := otherAccountID.String()
	otherUAIDStr := otherUAID.String()

	// Case (e): completed suggestion referencing other user's Plaid account.
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, completed_at, actual_amount, completion_source,
			 to_account_id, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'completed', $3, 100.00, 'exact_match', $4, $3, $3)
	`, uuid.New(), requesterID, now, &otherAccountIDStr)
	require.NoError(t, err, "seed suggestion with cross-user to_account_id")

	// Case (f): completed suggestion referencing other user's manual account.
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, completed_at, actual_amount, completion_source,
			 to_user_account_id, created_at, updated_at)
		VALUES ($1, $2, 200.00, 'boost', 'completed', $3, 200.00, 'exact_match', $4, $3, $3)
	`, uuid.New(), requesterID, now, &otherUAIDStr)
	require.NoError(t, err, "seed suggestion with cross-user to_user_account_id")

	impact, err := repo.GetUserImpact(ctx, requesterID.String())
	require.NoError(t, err, "GetUserImpact must not error")
	require.Len(t, impact.RecentItems, 2, "both completed suggestions must appear")

	for _, item := range impact.RecentItems {
		assert.Nil(t, item.ToAccountName,
			"cross-user account reference must NOT leak the other user's account name (GAP CF)")
	}
}
