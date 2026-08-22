//go:build integration

package repository

// Integration tests for MerchantRepository methods that involve GROUP BY, aggregation,
// or JOINs which pgxmock cannot validate (type/aggregate errors).
//
// Also covers ResolveEffectiveMerchantNames (TransactionRepository), which exercises the
// three-way LEFT JOIN (source override, merge topology, target override) that pgxmock
// cannot validate at the type/aggregate level (G6b + G7 from issue #957).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run 'TestMerchant.*Integration|TestResolveEffectiveMerchantNames.*Integration' \
//	  -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMerchantFixture creates a user + linked_item + account + merchant + transaction.
// All writes target db (typically pgx.Tx) so they roll back with it.
func seedMerchantFixture(ctx context.Context, t *testing.T, db DB, prefix string) (userID, merchantID uuid.UUID) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	accountID := uuid.New()
	merchantID = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct-"+accountID.String())
	require.NoError(t, err, "seed account")

	normalized := "Coffee Shop"
	_, err = db.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, $2, $3, $4)
	`, merchantID, "SQ*Coffee Shop", normalized, time.Now().UTC())
	require.NoError(t, err, "seed merchant")

	txID := uuid.New()
	extID := "plaid-tx-" + txID.String()
	_, err = db.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, merchant_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			merchant_name, name, amount, date, pending,
			pfc_primary, category_source, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7, $8, $8, -5.50, CURRENT_DATE, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, txID, userID, accountID, linkedItemID, merchantID, extID, extID, "SQ*Coffee Shop")
	require.NoError(t, err, "seed transaction")

	return userID, merchantID
}

// TestListMerchants_Integration verifies that ListMerchants executes valid SQL against
// a real PostgreSQL instance and returns correct aggregated data.
func TestListMerchants_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-integ")

	// Use a repo that queries through the transaction.
	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	require.NotEmpty(t, summaries, "expect at least one merchant summary")
	found := false
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			found = true
			assert.Equal(t, int64(1), s.TransactionCount)
			assert.InDelta(t, 5.50, s.TotalSpend, 0.01)
			assert.False(t, s.HasOverride)
			assert.False(t, s.IsMerged)
			break
		}
	}
	assert.True(t, found, "seeded merchant must appear in ListMerchants result")
}

// TestListMerchants_WithOverride_Integration verifies that a rename override is reflected
// in effective_display_name.
func TestListMerchants_WithOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-override")

	customName := "My Cafe"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantID, customName)
	require.NoError(t, err, "seed override")

	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	var found bool
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			found = true
			assert.Equal(t, customName, s.EffectiveDisplayName)
			assert.True(t, s.HasOverride)
			assert.False(t, s.IsMerged)
			break
		}
	}
	assert.True(t, found, "merchant with override must appear in results")
}

// TestListMerchants_MergedMerchant_Integration verifies that when a merchant is merged
// into another, the source collapses into the target group.
func TestListMerchants_MergedMerchant_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "lm-merge-src")

	// Create a second merchant to merge into.
	targetMerchantID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Target Coffee', 'Target Coffee', NOW())
	`, targetMerchantID)
	require.NoError(t, err, "seed target merchant")

	// Merge source into target via override.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantID, targetMerchantID)
	require.NoError(t, err, "seed merge override")

	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	// The source merchant should not appear as its own group.
	// The target merchant ID should appear as the effective group.
	var foundSource, foundTarget bool
	for _, s := range summaries {
		if s.MerchantID == merchantID.String() {
			foundSource = true
		}
		if s.MerchantID == targetMerchantID.String() {
			foundTarget = true
			assert.True(t, s.IsMerged, "target group should be marked is_merged when it absorbs another")
		}
	}
	assert.False(t, foundSource, "source merchant must not appear as its own row after merge")
	assert.True(t, foundTarget, "target merchant must appear in results after absorbing source")
}

// TestGetDuplicateSuggestions_Integration verifies that the pg_trgm similarity query
// executes valid SQL and correctly groups merchants with similar normalized names.
func TestGetDuplicateSuggestions_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _ := seedMerchantFixture(ctx, t, tx, "dup-sugg")

	// Create a second very-similar merchant and link a transaction to it.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token2')
	`, linkedItemID, userID, "item2-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking2', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct2-"+accountID.String())
	require.NoError(t, err)

	// "Coffee Shoppe" is very similar to "Coffee Shop" (similarity > 0.6).
	dupMerchantID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Coffee Shoppe', 'Coffee Shoppe', NOW())
	`, dupMerchantID)
	require.NoError(t, err)

	dupTxID := uuid.New()
	dupExtID := "plaid-tx-" + dupTxID.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, merchant_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			merchant_name, name, amount, date, pending,
			pfc_primary, category_source, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7, 'Coffee Shoppe', 'Coffee Shoppe', -4.75, CURRENT_DATE, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, dupTxID, userID, accountID, linkedItemID, dupMerchantID, dupExtID, dupExtID)
	require.NoError(t, err)

	// Use tx-scoped repo.
	txRepo := NewMerchantRepository(tx)
	groups, err := txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.6)
	require.NoError(t, err)
	// With two similar merchants, we expect at least one group of 2.
	require.GreaterOrEqual(t, len(groups), 1, "expect at least one duplicate group")
	found := false
	for _, g := range groups {
		if len(g) >= 2 {
			ids := make(map[string]bool)
			for _, s := range g {
				ids[s.MerchantID] = true
				// P5-1: Both merchants lack overrides, so is_merged and has_override must be false.
				assert.False(t, s.IsMerged, "merchant with no override must not be is_merged; got true for %s", s.MerchantID)
				assert.False(t, s.HasOverride, "merchant with no override row must have has_override=false; got true for %s", s.MerchantID)
			}
			// At least one group should contain both our seeded merchants.
			// (There may be other merchants in the DB from prior tests.)
			if ids[dupMerchantID.String()] {
				found = true
			}
		}
	}
	assert.True(t, found, "duplicate merchant pair must appear in GetDuplicateSuggestions")
}

// TestGetDuplicateSuggestions_NoResults_Integration verifies that a user with only one
// merchant returns an empty slice.
func TestGetDuplicateSuggestions_NoResults_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _ := seedMerchantFixture(ctx, t, tx, "dup-none")

	txRepo := NewMerchantRepository(tx)
	// Use a very high threshold so nothing matches.
	groups, err := txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.99)
	require.NoError(t, err)
	assert.Empty(t, groups, "with threshold=0.99 and a single unique merchant, no groups expected")
}

// ── ResolveEffectiveMerchantNames integration tests (G6b + G7, issue #957) ──────────────
//
// These tests exercise the three-way LEFT JOIN SQL that pgxmock cannot validate:
//   source_override → merge topology → target_override → normalized_name → name

// TestResolveEffectiveMerchantNames_DirectOverride_Integration verifies that when a user
// has set a direct display_name override on a merchant, that name is returned.
func TestResolveEffectiveMerchantNames_DirectOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "resolve-direct")

	// Set a direct display_name override.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'Whole Foods Market', NOW(), NOW())
	`, uuid.New(), userID, merchantID)
	require.NoError(t, err, "seed direct override")

	repo := NewTransactionRepository(tx, enc)
	result, err := repo.ResolveEffectiveMerchantNames(ctx, userID.String(), []string{merchantID.String()})
	require.NoError(t, err)
	require.Contains(t, result, merchantID.String())
	assert.Equal(t, "Whole Foods Market", result[merchantID.String()], "direct override must take priority")
}

// TestResolveEffectiveMerchantNames_NormalizedName_Integration verifies that when no override
// exists, the merchant's normalized_name is returned.
func TestResolveEffectiveMerchantNames_NormalizedName_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantID := seedMerchantFixture(ctx, t, tx, "resolve-norm")
	// seedMerchantFixture seeds merchant with normalized_name="Coffee Shop"

	repo := NewTransactionRepository(tx, enc)
	result, err := repo.ResolveEffectiveMerchantNames(ctx, userID.String(), []string{merchantID.String()})
	require.NoError(t, err)
	require.Contains(t, result, merchantID.String())
	assert.Equal(t, "Coffee Shop", result[merchantID.String()], "normalized_name used when no override")
}

// TestResolveEffectiveMerchantNames_MergeWithTargetOverride_Integration is the G6b regression
// test: when merchant A is merged into merchant B, and B has a display_name override, the
// effective name for A must be B's override (not B's normalized_name). This is the bug that
// the prior Round-2 enrich query missed (it only joined the source merchant's override row).
func TestResolveEffectiveMerchantNames_MergeWithTargetOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user + two merchants: sourceID (garbled) and targetID (canonical).
	userID, sourceID := seedMerchantFixture(ctx, t, tx, "resolve-mergetgt-src")

	targetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Starbucks Corp', 'Starbucks', NOW())
	`, targetID)
	require.NoError(t, err, "seed target merchant")

	// User merges source → target (no direct display_name on source row).
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, uuid.New(), userID, sourceID, targetID)
	require.NoError(t, err, "seed source→target merge")

	// User also renamed the target merchant with a display_name override.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'Starbucks Coffee', NOW(), NOW())
	`, uuid.New(), userID, targetID)
	require.NoError(t, err, "seed target display_name override")

	repo := NewTransactionRepository(tx, enc)
	result, err := repo.ResolveEffectiveMerchantNames(ctx, userID.String(), []string{sourceID.String()})
	require.NoError(t, err)
	require.Contains(t, result, sourceID.String(), "source merchant must appear in result")
	assert.Equal(t, "Starbucks Coffee", result[sourceID.String()],
		"G6b: target's display override must be used when source is merged into target")
}

// TestResolveEffectiveMerchantNames_NoMerchantID_Integration verifies that an empty merchantIDs
// slice returns an empty map without hitting the DB.
func TestResolveEffectiveMerchantNames_EmptyInput_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	repo := NewTransactionRepository(pool, enc)
	result, err := repo.ResolveEffectiveMerchantNames(ctx, "user-nobody", []string{})
	require.NoError(t, err)
	assert.Empty(t, result, "empty merchantIDs must return empty map")
}

// ── G23: duplicate suggestions must exclude already-merged merchants ──────────────────────
//
// If a merchant has been merged into another (merged_into_merchant_id IS NOT NULL in
// merchant_name_overrides), it must not appear in GetDuplicateSuggestions — otherwise the
// user sees already-completed merges re-suggested and one-click re-merging creates depth-2 chains.

// ── G29: CASCADE delete + transitive re-point integration tests (issue #957 Round 9) ────────
//
// These tests exercise real-SQL behaviors that pgxmock cannot validate:
//  1. Migration 000102 CASCADE: DELETE-ing a merge target removes merge-only overrides.
//  2. MergeAndFlatten transitive re-point: calling MergeAndFlatten([B], C, [B]) re-points
//     any existing A→B pointer to A→C inside a single atomic savepoint.

// TestMerchantDelete_CascadesMergeOverride_Integration proves that migration 000102's
// ON DELETE CASCADE removes a merge-only override row when its target merchant is deleted,
// without triggering the CHECK constraint (display_name IS NOT NULL OR merged_into IS NOT NULL).
func TestMerchantDelete_CascadesMergeOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, sourceID := seedMerchantFixture(ctx, t, tx, "g29-cascade")

	targetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Target Merchant G29', 'Target Merchant G29', NOW())
	`, targetID)
	require.NoError(t, err, "seed target merchant C")

	// Seed a merge-only override: source→target (display_name IS NULL).
	overrideID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, overrideID, userID, sourceID, targetID)
	require.NoError(t, err, "seed merge-only override source→target")

	var count int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM merchant_name_overrides WHERE id = $1`, overrideID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "override must exist before merchant delete")

	// DELETE the target merchant — ON DELETE CASCADE must drop the override, not NULL it.
	_, err = tx.Exec(ctx, `DELETE FROM merchants WHERE id = $1`, targetID)
	require.NoError(t, err, "DELETE target merchant must succeed — CASCADE removes the override row")

	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM merchant_name_overrides WHERE id = $1`, overrideID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "G29: migration 000102 CASCADE must delete merge-only override when target merchant is deleted")
}

// TestMergeAndFlatten_TransitiveOverrideResolves_Integration is the end-to-end real-SQL
// regression test for the G24 override-merge path. Scenario: A→B override exists; then
// MergeAndFlatten([B], C, [B]) is called (simulating SetOverride(B, merged_into=C)).
// The flatten UPDATE must re-point A→B to A→C. A transaction on A must resolve to C's name.
func TestMergeAndFlatten_TransitiveOverrideResolves_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g29-transitive")

	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Merchant B G29', 'Merchant B G29', NOW())
	`, merchantB)
	require.NoError(t, err, "seed merchant B")

	cName := "Canonical C G29"
	merchantC := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, $2, $3, NOW())
	`, merchantC, cName, cName)
	require.NoError(t, err, "seed merchant C")

	// Seed existing A→B override (from a prior merge).
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed A→B override")

	// Call MergeAndFlatten([B], C, [B]) — pgx.Tx.Begin() creates a savepoint internally.
	overrideRepo := NewMerchantOverrideRepository(tx)
	results, err := overrideRepo.MergeAndFlatten(ctx, userID.String(),
		[]string{merchantB.String()},
		merchantC.String(),
		[]string{merchantB.String()},
		nil,
	)
	require.NoError(t, err, "MergeAndFlatten must succeed")
	require.Len(t, results, 1, "expect one result row for the B→C upsert")
	assert.Equal(t, merchantB.String(), results[0].MerchantID)

	// Assert the A→B override is now A→C.
	var newTarget string
	err = tx.QueryRow(ctx, `
		SELECT merged_into_merchant_id::text
		FROM   merchant_name_overrides
		WHERE  user_id = $1 AND merchant_id = $2
	`, userID, merchantA).Scan(&newTarget)
	require.NoError(t, err, "A override row must still exist after flatten")
	assert.Equal(t, merchantC.String(), newTarget,
		"G29: A→B must be re-pointed to A→C by the flatten UPDATE inside MergeAndFlatten")

	// End-to-end: transaction on A must resolve to C's canonical name.
	txRepo := NewTransactionRepository(tx, enc)
	effective, err := txRepo.ResolveEffectiveMerchantNames(ctx, userID.String(), []string{merchantA.String()})
	require.NoError(t, err)
	assert.Equal(t, cName, effective[merchantA.String()],
		"G29: transaction on A must resolve to C's name after transitive MergeAndFlatten re-point")
}

// TestMergeAndFlatten_DisplayNames_COALESCE_Integration exercises the riskiest G27 SQL path:
// the unnest($3::text[], $4::text[]) + COALESCE(EXCLUDED.display_name, existing.display_name)
// clause inside MergeAndFlatten. Two sub-cases verify both arms of COALESCE:
//  1. non-nil displayNames entry → new name is written as the source's display_name.
//  2. nil displayNames entry on a second call → existing display_name is preserved (COALESCE keeps it).
func TestMergeAndFlatten_DisplayNames_COALESCE_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g27-coalesce")

	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Merchant B COALESCE', 'Merchant B COALESCE', NOW())
	`, merchantB)
	require.NoError(t, err, "seed canonical merchant B")

	overrideRepo := NewMerchantOverrideRepository(tx)

	// Sub-case 1: call MergeAndFlatten with a non-nil displayNames entry.
	// The upserted row for A must carry the provided display_name.
	customName := "My Custom Shop"
	results, err := overrideRepo.MergeAndFlatten(ctx, userID.String(),
		[]string{merchantA.String()},
		merchantB.String(),
		[]string{merchantA.String()},
		[]*string{&customName},
	)
	require.NoError(t, err, "MergeAndFlatten with non-nil displayNames must succeed")
	require.Len(t, results, 1, "expect one result row for A→B upsert")
	require.NotNil(t, results[0].DisplayName, "display_name must be set on the upserted row")
	assert.Equal(t, customName, *results[0].DisplayName, "G27: non-nil displayNames entry must be written as display_name")
	assert.Equal(t, merchantA.String(), results[0].MerchantID)

	// Sub-case 2: call again with nil displayNames — COALESCE must preserve the existing display_name.
	results2, err := overrideRepo.MergeAndFlatten(ctx, userID.String(),
		[]string{merchantA.String()},
		merchantB.String(),
		[]string{merchantA.String()},
		nil,
	)
	require.NoError(t, err, "MergeAndFlatten with nil displayNames must succeed")
	require.Len(t, results2, 1, "expect one result row for A→B re-upsert")
	require.NotNil(t, results2[0].DisplayName, "display_name must be preserved by COALESCE when new entry is nil")
	assert.Equal(t, customName, *results2[0].DisplayName, "G27: nil displayNames entry must preserve existing display_name via COALESCE")
}

// ── G31: Upsert/BulkApply must preserve an existing merge pointer on pure-rename ─────────────
//
// Issue #957 Round 10: the ON CONFLICT SET clause unconditionally overwrites
// merged_into_merchant_id with EXCLUDED.merged_into_merchant_id (NULL on a pure rename),
// silently clearing a previously-set merge pointer and breaking AC5.

// TestUpsert_PureRename_PreservesExistingMergePointer_Integration verifies that calling
// Upsert with a display_name but no merged_into_merchant_id preserves an existing merge pointer.
func TestUpsert_PureRename_PreservesExistingMergePointer_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g31-upsert")

	// Seed target merchant B.
	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Merchant B G31', 'Merchant B G31', NOW())
	`, merchantB)
	require.NoError(t, err, "seed target merchant B")

	// Seed an existing merge-only override: A→B (display_name NULL).
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed A→B merge-only override")

	// Now call Upsert with a display_name but no merged_into_merchant_id (pure rename).
	overrideRepo := NewMerchantOverrideRepository(tx)
	displayName := "My Merchant A"
	result, err := overrideRepo.Upsert(ctx, domain.MerchantNameOverride{
		UserID:               userID.String(),
		MerchantID:           merchantA.String(),
		DisplayName:          &displayName,
		MergedIntoMerchantID: nil, // pure rename — no merge pointer
	})
	require.NoError(t, err, "Upsert must succeed")

	// G31 assertion: the merge pointer must still be B, not wiped to NULL.
	require.NotNil(t, result.DisplayName, "display_name must be set")
	assert.Equal(t, displayName, *result.DisplayName, "display_name must be the new override")
	require.NotNil(t, result.MergedIntoMerchantID, "merged_into_merchant_id must be preserved — was wiped to NULL before G31 fix")
	assert.Equal(t, merchantB.String(), *result.MergedIntoMerchantID, "G31: pure-rename Upsert must COALESCE-preserve the existing merge pointer")
}

// TestBulkApply_PureRename_PreservesExistingMergePointer_Integration mirrors the above
// for the bulk write path (BulkApply's display-only upsert), which shares the same
// ON CONFLICT clause via bulkUpsertTx.
func TestBulkApply_PureRename_PreservesExistingMergePointer_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g31-bulk")

	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Merchant B BulkG31', 'Merchant B BulkG31', NOW())
	`, merchantB)
	require.NoError(t, err, "seed target merchant B")

	// Seed A→B merge-only override.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed A→B merge-only override")

	// BulkApply with a display_name only (no merged_into_merchant_id, no merge groups).
	overrideRepo := NewMerchantOverrideRepository(tx)
	displayName := "My Merchant A (bulk)"
	results, err := overrideRepo.BulkApply(ctx, userID.String(), []domain.MerchantNameOverride{{
		UserID:               userID.String(),
		MerchantID:           merchantA.String(),
		DisplayName:          &displayName,
		MergedIntoMerchantID: nil,
	}}, nil)
	require.NoError(t, err, "BulkApply must succeed")
	require.Len(t, results, 1)

	require.NotNil(t, results[0].DisplayName, "display_name must be set")
	assert.Equal(t, displayName, *results[0].DisplayName)
	require.NotNil(t, results[0].MergedIntoMerchantID, "merged_into_merchant_id must be preserved — was wiped before G31 fix")
	assert.Equal(t, merchantB.String(), *results[0].MergedIntoMerchantID, "G31: the bulk display-only upsert must COALESCE-preserve the existing merge pointer")
}

// TestUpsert_PureRenameAfterMerge_StillResolvesToCanonical_Integration is the end-to-end AC5
// regression: after merging A→B, a pure rename of A must preserve the merge pointer so
// ResolveEffectiveMerchantNames still follows the chain correctly.
func TestUpsert_PureRenameAfterMerge_StillResolvesToCanonical_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g31-e2e")

	bName := "Canonical B G31 E2E"
	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, $2, $3, NOW())
	`, merchantB, bName, bName)
	require.NoError(t, err, "seed target merchant B")

	// Rename B so it has a display override.
	bDisplay := "Starbucks (G31)"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantB, bDisplay)
	require.NoError(t, err, "seed target display override")

	// Merge A→B.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed A→B merge")

	// Now pure-rename A (display_name only, mergedInto=nil).
	overrideRepo := NewMerchantOverrideRepository(tx)
	aLabel := "My A Label"
	upserted, err := overrideRepo.Upsert(ctx, domain.MerchantNameOverride{
		UserID:               userID.String(),
		MerchantID:           merchantA.String(),
		DisplayName:          &aLabel,
		MergedIntoMerchantID: nil,
	})
	require.NoError(t, err, "pure-rename A must not fail")

	// G31 AC5: merge pointer must still be intact.
	require.NotNil(t, upserted.MergedIntoMerchantID,
		"G31 AC5: merge pointer must be preserved after pure-rename Upsert")
	assert.Equal(t, merchantB.String(), *upserted.MergedIntoMerchantID,
		"G31 AC5: merge pointer must still point to B")

	// ResolveEffectiveMerchantNames: A's own display_name (aLabel) wins COALESCE first.
	txRepo := NewTransactionRepository(tx, enc)
	effective, err := txRepo.ResolveEffectiveMerchantNames(ctx, userID.String(), []string{merchantA.String()})
	require.NoError(t, err)
	assert.Equal(t, aLabel, effective[merchantA.String()],
		"G31: A's own display_name wins COALESCE (merge pointer preserved confirms AC5 is still intact)")
}

// ── G32: ListMerchants must include target's display override in effective name ──────────────
//
// Issue #957 Round 10: ListMerchants only joins the source override (o.merchant_id=t.merchant_id)
// but has no mno_t join for the target. When source A is merged into target B and B has a
// display_name override but no own transactions, ListMerchants shows B.normalized_name instead
// of B's override.

// TestListMerchants_MergedTargetOverride_Integration verifies that when a merchant is merged
// into a target with a display_name override, the Merchants tab shows the target's override.
func TestListMerchants_MergedTargetOverride_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed source merchant A with a transaction (so it appears in ListMerchants).
	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g32-list")

	// Seed target merchant B with NO transactions of its own.
	merchantB := uuid.New()
	bNorm := "B Normalized G32"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'B Raw G32', $2, NOW())
	`, merchantB, bNorm)
	require.NoError(t, err, "seed target merchant B (no transactions)")

	// Rename target B with a display override.
	bDisplay := "Starbucks Coffee (G32)"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantB, bDisplay)
	require.NoError(t, err, "seed B display override")

	// Merge source A → target B.
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed A→B merge override")

	txRepo := NewMerchantRepository(tx)
	summaries, err := txRepo.ListMerchants(ctx, userID.String())
	require.NoError(t, err)

	// The collapsed group is keyed by B's effective_merchant_id.
	var found bool
	for _, s := range summaries {
		if s.MerchantID == merchantB.String() {
			found = true
			assert.Equal(t, bDisplay, s.EffectiveDisplayName,
				"G32: target's display override must appear as effective_display_name in ListMerchants; got %q (normalized=%q)", s.EffectiveDisplayName, bNorm)
			break
		}
	}
	assert.True(t, found, "G32: target merchant B must appear as the collapsed group in ListMerchants")
}

func TestGetDuplicateSuggestions_ExcludesMergedMerchants_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed two very similar merchants and link transactions to both.
	userID, merchantA := seedMerchantFixture(ctx, t, tx, "g23-src-a")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token-g23b')
	`, linkedItemID, userID, "item-g23b-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking-G23', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct-g23-"+accountID.String())
	require.NoError(t, err)

	merchantB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name, normalized_name, created_at)
		VALUES ($1, 'Coffee Shope', 'Coffee Shope', NOW())
	`, merchantB)
	require.NoError(t, err)

	txID := uuid.New()
	extIDG23b := "plaid-tx-g23b-" + txID.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, account_id, linked_item_id, merchant_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			merchant_name, name, amount, date, pending,
			pfc_primary, category_source, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7, 'Coffee Shope', 'Coffee Shope', -3.50, CURRENT_DATE, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, txID, userID, accountID, linkedItemID, merchantB, extIDG23b, extIDG23b)
	require.NoError(t, err)

	txRepo := NewMerchantRepository(tx)

	// Phase 1: before merge — both merchants should appear in suggestions.
	groups, err := txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.5)
	require.NoError(t, err)
	preFoundA, preFoundB := false, false
	for _, g := range groups {
		for _, s := range g {
			if s.MerchantID == merchantA.String() {
				preFoundA = true
			}
			if s.MerchantID == merchantB.String() {
				preFoundB = true
			}
		}
	}
	require.True(t, preFoundA || preFoundB, "at least one of the similar merchants should appear in suggestions before merge")

	// Phase 2: merge A → B (A becomes the merged-away source).
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides
		    (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, userID, merchantA, merchantB)
	require.NoError(t, err, "seed merge override: A→B")

	// Phase 3: after merge — merchantA must NOT appear in suggestions.
	groups, err = txRepo.GetDuplicateSuggestions(ctx, userID.String(), 0.5)
	require.NoError(t, err)
	for _, g := range groups {
		for _, s := range g {
			assert.NotEqual(t, merchantA.String(), s.MerchantID,
				"G23: already-merged merchant A must not appear in duplicate suggestions")
		}
	}
}

// ============================================================================
// BulkApply atomicity integration tests (Issue #957 R13, promoted from #1348)
// ============================================================================

// seedTwoMerchants creates a user + two merchants + one transaction per merchant,
// all scoped to db (typically pgx.Tx) so they roll back with it.
func seedTwoMerchants(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, merchantA, merchantB uuid.UUID,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	accountID := uuid.New()
	merchantA = uuid.New()
	merchantB = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id, name, type, subtype)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'checking')
	`, accountID, userID, linkedItemID, "plaid-acct-"+accountID.String())
	require.NoError(t, err, "seed account")

	for i, mID := range []uuid.UUID{merchantA, merchantB} {
		name := prefix + "-merchant-" + string(rune('A'+i))
		_, err = db.Exec(ctx, `
			INSERT INTO merchants (id, name, normalized_name, created_at)
			VALUES ($1, $2, $3, NOW())
		`, mID, name, name)
		require.NoError(t, err, "seed merchant")

		txID := uuid.New()
		extID := "plaid-tx-" + txID.String()
		_, err = db.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, merchant_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				merchant_name, name, amount, date, pending,
				pfc_primary, category_source, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7, $8, $8, -10.00, CURRENT_DATE, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
		`, txID, userID, accountID, linkedItemID, mID, extID, extID, name)
		require.NoError(t, err, "seed transaction")
	}
	return userID, merchantA, merchantB
}

// TestBulkApply_HappyPath_CommitsAllRows verifies that BulkApply commits display-only
// and merge overrides inside one transaction.
func TestBulkApply_HappyPath_CommitsAllRows(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, merchantA, merchantB := seedTwoMerchants(ctx, t, tx, "ba-happy")

	displayName := "My Coffee"
	displayOnly := []domain.MerchantNameOverride{
		{UserID: userID.String(), MerchantID: merchantA.String(), DisplayName: &displayName},
	}

	// Merge merchantB → merchantA (canonical)
	groups := []BulkMergeGroup{
		{
			Sources:      []string{merchantB.String()},
			Canonical:    merchantA.String(),
			ToFlatten:    []string{merchantB.String()},
			DisplayNames: nil,
		},
	}

	overrideRepo := NewMerchantOverrideRepository(tx)
	results, err := overrideRepo.BulkApply(ctx, userID.String(), displayOnly, groups)
	require.NoError(t, err)
	require.Len(t, results, 2, "should return display-only + merge override")

	foundDisplay := false
	for _, r := range results {
		if r.MerchantID == merchantA.String() && r.DisplayName != nil && *r.DisplayName == displayName {
			foundDisplay = true
		}
	}
	assert.True(t, foundDisplay, "display-only override must appear in results")

	foundMerge := false
	for _, r := range results {
		if r.MerchantID == merchantB.String() && r.MergedIntoMerchantID != nil && *r.MergedIntoMerchantID == merchantA.String() {
			foundMerge = true
		}
	}
	assert.True(t, foundMerge, "merge override must appear in results")
}

// TestBulkApply_PartialFailure_RollsBackAll verifies that a failed merge group causes
// the entire batch to roll back, including display-only overrides processed first.
func TestBulkApply_PartialFailure_RollsBackAll(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// Outer rollback tx so the test leaves no permanent state.
	outerTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer outerTx.Rollback(ctx) //nolint:errcheck

	userID, merchantA, _ := seedTwoMerchants(ctx, t, outerTx, "ba-partial")

	nonExistentCanonical := uuid.New().String() // not in DB

	displayName := "Should Roll Back"
	displayOnly := []domain.MerchantNameOverride{
		{UserID: userID.String(), MerchantID: merchantA.String(), DisplayName: &displayName},
	}

	// Group references non-existent canonical → constraint error
	groups := []BulkMergeGroup{
		{
			Sources:      []string{merchantA.String()},
			Canonical:    nonExistentCanonical,
			ToFlatten:    []string{merchantA.String()},
			DisplayNames: nil,
		},
	}

	overrideRepo := NewMerchantOverrideRepository(outerTx)
	_, err = overrideRepo.BulkApply(ctx, userID.String(), displayOnly, groups)
	require.Error(t, err, "BulkApply must error when merge group has invalid canonical")

	// After failed BulkApply, the display-only override must NOT exist (internal tx rolled back)
	var count int
	row := outerTx.QueryRow(ctx, `
		SELECT COUNT(*) FROM merchant_name_overrides
		WHERE user_id = $1 AND merchant_id = $2
	`, userID, merchantA)
	require.NoError(t, row.Scan(&count))
	assert.Equal(t, 0, count, "display-only override must be rolled back with failed merge group")
}
