//go:build integration

package repository

// Issue #1569 follow-up (#1590) — a GDPR erasure must not destroy a SURVIVING user's category
// annotation without a record.
//
// WHAT IS BEING TESTED, and what it is NOT. DeleteUserCascade clears dangling custom-category
// overrides before hard-deleting the erased user, scoped by CATEGORY OWNER rather than transaction
// owner, because transactions.custom_category_override_id -> custom_categories is ON DELETE RESTRICT
// and a surviving pointer would refuse the erasure outright. The clear is required and stays. What
// #1590 adds is that when the row it clears belongs to somebody ELSE, that person gets a
// transaction_annotation_history row saying what was there.
//
// ⚠ THE FIXTURE DELIBERATELY WRITES A ROW PRODUCTION REFUSES, and that is not the vacuous-fixture
// hazard this repository has been bitten by — it is the whole subject. The cross-owner override is
// SCHEMA-reachable and not SERVICE-reachable: every one of the five writers of that column checks
// ownership (the two category endpoints, the two settlement moves, and CreateBatch via the rule
// target). No trigger enforces it in the database, so the state is representable, and this guard is
// defence in depth against a backfill, a manual fix, or a sixth write path. A fixture built through
// a production write path could not construct the case at all, because production is exactly what
// refuses it. Everything ELSE about the fixture is faithful: category_source, category_override_date
// and the review state are all set the way a real manual override carries them, so the recorded
// prior tuple is a tuple production could actually have produced.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestErasureClear_SurvivingUsersAnnotationIsRecorded_Integration is #1590's headline claim: the
// clear is RECORDED for the surviving owner, and the record survives the erasure that caused it.
func TestErasureClear_SurvivingUsersAnnotationIsRecorded_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "erasure-1590-target-"+suffix, nil, nil, nil)
	survivor := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "erasure-1590-survivor-"+suffix, nil, nil, nil)

	var categoryID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Shared Groceries') RETURNING id`,
		target.ID).Scan(&categoryID))

	// The survivor's own transaction, annotated the way a manual override really looks: source
	// 'manual', a dated override, reviewed. A row whose prior tuple is all NULLs would still trip
	// the capture's IS DISTINCT FROM, so an unfaithful fixture would produce a GREEN test that
	// pinned nothing about what is recorded.
	reviewedAt := time.Now().Add(-72 * time.Hour)
	overrideDate := time.Now().Add(-96 * time.Hour)
	var survivorTxnID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO transactions (
			user_id, source, amount, date, name, pfc_primary,
			category_override_kind, custom_category_override_id, effective_category_name,
			category_source, category_override_date, reviewed, reviewed_at)
		VALUES ($1, 'manual', 20.00, CURRENT_DATE, 'Survivor txn', 'FOOD_AND_DRINK',
			'custom', $2, 'Shared Groceries', 'manual', $3, TRUE, $4)
		RETURNING id`,
		survivor.ID, categoryID, overrideDate, reviewedAt).Scan(&survivorTxnID))

	// The erased user's OWN transaction on the same category. It is cleared by the same statement
	// and must NOT produce a history row: it is hard-deleted seconds later in the same transaction.
	var targetTxnID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO transactions (
			user_id, source, amount, date, name, pfc_primary,
			category_override_kind, custom_category_override_id, effective_category_name,
			category_source, category_override_date, reviewed, reviewed_at)
		VALUES ($1, 'manual', 11.00, CURRENT_DATE, 'Target own txn', 'GENERAL_MERCHANDISE',
			'custom', $2, 'Shared Groceries', 'manual', $3, TRUE, $4)
		RETURNING id`,
		target.ID, categoryID, overrideDate, reviewedAt).Scan(&targetTxnID))

	deleted, affectedOwnerIDs, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)
	assert.Equal(t, []string{survivor.ID}, affectedOwnerIDs,
		"the surviving owner is still reported for the cache bump; adding the capture must not change that")

	assertUserRowGone(t, ctx, pool, target.ID)
	assertUserRowExists(t, ctx, pool, survivor.ID)

	// THE RECORD SURVIVED THE ERASURE. transaction_annotation_history.user_id CASCADEs from users,
	// and the recorded user is the SURVIVOR, so A's deletion cannot reach it.
	var (
		historyUser      string
		source           string
		priorKind        *string
		priorOverrideID  *string
		priorName        *string
		priorSource      *string
		priorReviewed    bool
		newKind          *string
		newOverrideID    *string
		newName          *string
	)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT user_id, change_source,
		       prior_category_override_kind, prior_custom_category_override_id,
		       prior_effective_category_name, prior_category_source, prior_reviewed,
		       new_category_override_kind, new_custom_category_override_id, new_effective_category_name
		FROM transaction_annotation_history WHERE transaction_id = $1`, survivorTxnID).
		Scan(&historyUser, &source, &priorKind, &priorOverrideID, &priorName, &priorSource,
			&priorReviewed, &newKind, &newOverrideID, &newName))

	assert.Equal(t, survivor.ID, historyUser,
		"the record belongs to the SURVIVOR, which is also why it is not cascaded away with the erased user")
	assert.Equal(t, domain.TransactionAnnotationChangeErasure, source,
		"the user did not ask for this; filing it as 'category' would claim they recategorised their own row")

	require.NotNil(t, priorKind)
	assert.Equal(t, "custom", *priorKind)
	require.NotNil(t, priorOverrideID)
	assert.Equal(t, categoryID, *priorOverrideID, "the record names the category that was lost")
	require.NotNil(t, priorName)
	assert.Equal(t, "Shared Groceries", *priorName, "and what it was called")
	require.NotNil(t, priorSource)
	assert.Equal(t, string(domain.CategorySourceManual), *priorSource)
	assert.True(t, priorReviewed)

	assert.Nil(t, newKind, "and what replaced it: nothing")
	assert.Nil(t, newOverrideID)
	require.NotNil(t, newName)
	assert.Equal(t, "FOOD_AND_DRINK", *newName,
		"the row reverted to its own underlying PFC category, which is the value the generated column computes")

	// NO ROW FOR THE ERASED USER.
	//
	// ⚠ THIS ASSERTION IS A POST-CONDITION, NOT A DISCRIMINATING TEST, and saying so is the point.
	// It holds whether or not the statement's `surviving` CTE filters the erased user out: a history
	// row written for them carries their user_id, and transaction_annotation_history.user_id CASCADEs
	// from users, so the DELETE FROM users a few statements later destroys it either way. Removing
	// the filter is therefore a mutation NOTHING here can see, and no assertion in this file should
	// be read as proving the filter works.
	//
	// The filter is still right, for a reason that is a cost rather than a behaviour: the erased user
	// can own thousands of transactions carrying their own custom categories, and every one of them
	// would be an INSERT executed and then immediately cascaded away, inside the erasure transaction
	// that a user is waiting on. It buys nothing and lengthens the one transaction that must not
	// fail. That is the whole justification; there is no observable correctness claim behind it.
	var targetHistory int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM transaction_annotation_history WHERE transaction_id = $1`, targetTxnID).
		Scan(&targetHistory))
	assert.Zero(t, targetHistory,
		"whatever the statement wrote for the erased user is gone with them — the post-condition GDPR requires")
}

// TestErasureClear_SurvivorsOwnErasureStillDestroysTheRecord_Integration is the REVERSE direction,
// and it is the one GDPR requires. Making B's record survive A's erasure must not make it survive
// B's. The structural guard for this is
// TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned; this is the same claim aimed at
// the one row #1590 introduces, so a failure names the cause directly.
func TestErasureClear_SurvivorsOwnErasureStillDestroysTheRecord_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "erasure-1590-b-target-"+suffix, nil, nil, nil)
	survivor := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "erasure-1590-b-survivor-"+suffix, nil, nil, nil)

	var categoryID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Doomed Cat') RETURNING id`,
		target.ID).Scan(&categoryID))

	var survivorTxnID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO transactions (
			user_id, source, amount, date, name, pfc_primary,
			category_override_kind, custom_category_override_id, effective_category_name,
			category_source, category_override_date, reviewed, reviewed_at)
		VALUES ($1, 'manual', 20.00, CURRENT_DATE, 'Survivor txn', 'FOOD_AND_DRINK',
			'custom', $2, 'Doomed Cat', 'manual', NOW(), TRUE, NOW())
		RETURNING id`, survivor.ID, categoryID).Scan(&survivorTxnID))

	deleted, _, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM transaction_annotation_history WHERE user_id = $1`, survivor.ID).Scan(&count))
	require.Equal(t, 1, count, "the record exists before B's own erasure, or the assertion below is vacuous")

	deleted, _, err = repo.DeleteUserCascade(ctx, survivor.ID, survivor.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM transaction_annotation_history WHERE user_id = $1`, survivor.ID).Scan(&count))
	assert.Zero(t, count,
		"B's own erasure must still destroy B's record; no retention decision may leave a row unreachable from users")
}
