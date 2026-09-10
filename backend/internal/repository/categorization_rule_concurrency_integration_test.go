// categorization_rule_concurrency_integration_test.go — DB-backed proof that the
// optimistic-concurrency guard added to CategorizationRuleRepository.Update (#1625 Task 19)
// actually stops a concurrent toggle from resurrecting a rule's prior (malformed) conditions.
//
// Why this needs REAL Postgres and REAL goroutines, not pgxmock: pgxmock never serializes two
// callers against one row, so it cannot distinguish "the WHERE clause carries the right
// predicate" from "the WHERE clause carries a predicate that happens to always match in a
// single-threaded test." Only two genuinely concurrent writers, racing on the SAME row via
// SEPARATE connections from a real pool, can prove the guard actually rejects the loser instead
// of both writes serializing harmlessly one after the other (which sequential calls would do,
// proving nothing — this branch has been bitten by exactly that: see CLAUDE.md's Task 19 note).
//
// Run:
//
//	DATABASE_URL="postgres://..." CACHE_REDIS_TEST_URL="redis://..." REDIS_TEST_URL="redis://..." \
//	  go test ./internal/repository/ -run TestCategorizationRuleRepository_ConcurrentRepairAndToggle -v
package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCategorizationRuleRepository_ConcurrentRepairAndToggle_Integration reproduces the exact
// scenario from the #1625 Task 19 brief: a user repairing a malformed rule's conditions while a
// toggle (enable/disable) is in flight for the SAME rule. Before this fix, ToggleRule's
// read-modify-write of the whole row meant that whichever of the two writes landed SECOND at the
// database would silently overwrite the other's column values wholesale — including a repair's
// brand-new conditions getting clobbered back to the stale copy the toggle read before the repair
// ever happened. After this fix, the loser of the race gets domain.ErrRuleConcurrentlyModified
// instead of silently overwriting the winner.
func TestCategorizationRuleRepository_ConcurrentRepairAndToggle_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewCategorizationRuleRepository(pool)

	userID := insertTestUser(t, pool)

	malformedConditions := []domain.RuleCondition{
		{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"}, // the thing being repaired
	}
	repairedConditions := []domain.RuleCondition{
		{Type: domain.ConditionAmountGreaterThan, Value: 20.0},
	}

	original := &domain.CategorizationRule{
		UserID:         userID,
		Name:           "Concurrency Race Rule " + uuid.New().String(),
		Enabled:        true,
		Priority:       1,
		Conditions:     malformedConditions,
		ConditionLogic: "AND",
		TargetTags:     []string{"race-fixture"},
		TagBehavior:    "add",
		Source:         "manual",
	}
	require.NoError(t, repo.Create(ctx, original))
	ruleID := original.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM categorization_rules WHERE id = $1`, ruleID) //nolint:errcheck
	})

	// Two independent reads of the SAME pre-race row, each carrying its own copy of the VERSION
	// this write believes is current — exactly what UpdateRule and ToggleRule each do today
	// (GetByID, mutate the in-memory copy, Update). Fetching both BEFORE either writer starts is
	// what makes the race genuine: neither writer has seen the other's change.
	//
	// ⚠ #1625 second fix wave L7: this comment said updated_at and this assertion compared
	// UpdatedAt, both left over from Task 19. Final review B2 moved the guard to `version`, so
	// equal UpdatedAt was no longer the precondition this race needs — two reads could agree on
	// updated_at and the test would still say nothing about the token Update actually compares.
	repairReader, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	toggleReader, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	require.Equal(t, repairReader.Version, toggleReader.Version,
		"both readers must observe the same pre-race version — `version` is what Update's WHERE clause compares")
	require.NotZero(t, repairReader.Version, "a stored rule must carry a version; 0 would make the guard vacuous")

	// The "repair" write: fixes the conditions, leaves enabled untouched.
	repairReader.Conditions = repairedConditions
	// The "toggle" write: flips enabled, but — because ToggleRule reads the WHOLE row and writes
	// it all back — still carries the STALE malformed conditions it read before the repair.
	toggleReader.Enabled = false

	var (
		wg        sync.WaitGroup
		ready     sync.WaitGroup
		repairErr error
		toggleErr error
	)
	start := make(chan struct{})
	ready.Add(2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ready.Done()
		<-start // block until both goroutines have reached this point, forcing genuine overlap
		repairErr = repo.Update(ctx, repairReader)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ready.Done()
		<-start
		toggleErr = repo.Update(ctx, toggleReader)
	}()

	ready.Wait() // both goroutines are now parked on <-start, each on its own connection
	close(start) // release them at the same instant
	wg.Wait()

	// Exactly one writer must win outright; the other must be refused with the concurrency
	// conflict, never both succeeding (last-writer-wins) and never both failing.
	repairWon := repairErr == nil
	toggleWon := toggleErr == nil
	require.NotEqual(t, repairWon, toggleWon, "exactly one of the two concurrent writers must win: repairErr=%v toggleErr=%v", repairErr, toggleErr)

	if repairWon {
		assert.True(t, errors.Is(toggleErr, domain.ErrRuleConcurrentlyModified), "the losing toggle must report the concurrency conflict, got: %v", toggleErr)
	} else {
		assert.True(t, errors.Is(repairErr, domain.ErrRuleConcurrentlyModified), "the losing repair must report the concurrency conflict, got: %v", repairErr)
	}

	// The row in the database must match EXACTLY the winner's write — never a hybrid of both
	// (which is what "last-writer-wins across the whole row" would otherwise produce), and
	// critically: if the repair lost, its fix must NOT have been silently discarded by an
	// overwrite — it must be reportable to the caller as "try again", which the error assertion
	// above already confirms. This assertion additionally proves the persisted row is internally
	// consistent with whichever write actually landed.
	final, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)

	if repairWon {
		require.Len(t, final.Conditions, 1)
		assert.InDelta(t, 20.0, toFloat(t, final.Conditions[0].Value), 0.0001, "the repair's fixed condition value must be the one persisted")
		assert.True(t, final.Enabled, "the repair never touched enabled — it must remain at its original value")
	} else {
		require.Len(t, final.Conditions, 1)
		assert.Equal(t, "not-a-number", final.Conditions[0].Value, "the toggle won, so its (stale) conditions are what persists — the repair must be rejected outright, never merged")
		assert.False(t, final.Enabled, "the toggle's write must be the one reflected")
	}
}

// toFloat is a small helper: JSON round-tripping through jsonb turns a Go float64 condition value
// into a json.Number-ish float64 on the way back out, but guards against an unexpected type
// rather than panicking the test with a bad type assertion.
func toFloat(t *testing.T, v interface{}) float64 {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("expected condition value to decode as float64, got %T (%v)", v, v)
	}
	return f
}

// TestCategorizationRuleRepository_MatchCountBumpIsNotAConflict_Integration is #1625's final-review
// B2 reproduction.
//
// Task 19's guard keyed on `updated_at`. But `updated_at` is not a user-edit version: the table
// carries a BEFORE UPDATE trigger that stamps NOW() on EVERY write, and IncrementMatchCount — the
// engine's per-matched-transaction bookkeeping, which runs during an ordinary Plaid sync with no
// user involved at all — is a write. So a sync landing inside a user's read-modify-write window
// rejected their save with "This rule was changed by another request", which is FALSE: nothing the
// user cares about changed, and the likelihood scaled with how WELL the rule worked.
//
// The fix is a `version` column bumped only by user-facing updates (migration 000192).
// IncrementMatchCount and IncrementOverrideCount deliberately do not touch it — asserted below, on
// the column, not merely on the absence of a 409, so that a future writer adding `version =
// version + 1` to the bookkeeping path fails here rather than reintroducing the false conflict.
//
// ⚠ The genuine-conflict case is NOT weakened: TestCategorizationRuleRepository_
// ConcurrentRepairAndToggle_Integration above still requires exactly one of two racing USER writes
// to be refused. Both tests must pass together, or the lock has been traded for the lost update
// Task 19 existed to close.
func TestCategorizationRuleRepository_MatchCountBumpIsNotAConflict_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewCategorizationRuleRepository(pool)

	userID := insertTestUser(t, pool)

	original := &domain.CategorizationRule{
		UserID:         userID,
		Name:           "Match Count Bump Rule " + uuid.New().String(),
		Enabled:        true,
		Priority:       1,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"bump-fixture"},
		TagBehavior:    "add",
		Source:         "manual",
	}
	require.NoError(t, repo.Create(ctx, original))
	ruleID := original.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM categorization_rules WHERE id = $1`, ruleID) //nolint:errcheck
	})

	// The user opens the editor: one read, held while they type.
	editing, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	versionAtRead := editing.Version
	require.NotZero(t, versionAtRead, "a stored rule must carry a version; 0 would make the guard vacuous")

	// A sync fires the rule twice while the editor is open. No user is involved.
	require.NoError(t, repo.IncrementMatchCount(ctx, ruleID))
	require.NoError(t, repo.IncrementMatchCount(ctx, ruleID))

	// Falsifiability: prove the bookkeeping really did move `updated_at`, which is what the old
	// guard keyed on. Without this the test could pass against a trigger that stopped firing.
	var updatedAtAfterBump time.Time
	var versionAfterBump int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at, version FROM categorization_rules WHERE id = $1`, ruleID,
	).Scan(&updatedAtAfterBump, &versionAfterBump))
	// The precise property, and it is exactly the OLD guard's predicate: `updated_at = $14` would
	// no longer match. Asserted as inequality rather than "is later" on purpose — the Docker
	// Postgres clock and the host clock are not the same clock (the trigger stamps DB NOW(), the
	// insert wrote a host time.Now()), so an ordering assertion here is a flake, not a stronger
	// check.
	require.NotEqual(t, editing.UpdatedAt, updatedAtAfterBump,
		"IncrementMatchCount did not move updated_at, so this test no longer reproduces B2")
	require.Equal(t, int64(versionAtRead), versionAfterBump,
		"background bookkeeping must NOT bump version — that is the whole distinction the column exists to draw")

	// The user hits Save.
	editing.Name = "Match Count Bump Rule (renamed) " + uuid.New().String()
	require.NoError(t, repo.Update(ctx, editing),
		"a background match-count bump is not a competing edit and must not be reported to the "+
			"user as \"This rule was changed by another request\"")

	saved, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	assert.Equal(t, editing.Name, saved.Name)
	assert.Equal(t, versionAtRead+1, saved.Version, "a user-facing update bumps the version exactly once")
	assert.EqualValues(t, 2, saved.MatchCount, "the bookkeeping the user's save raced must not be lost")
}

// TestCategorizationRuleRepository_StaleVersionIsStillRefused_Integration is the other half of B2:
// a SECOND save from a stale in-memory copy is still a 409. Without this, "no false conflicts"
// could be satisfied by having no conflicts at all — which is the lost update Task 19 closed.
func TestCategorizationRuleRepository_StaleVersionIsStillRefused_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewCategorizationRuleRepository(pool)

	userID := insertTestUser(t, pool)

	original := &domain.CategorizationRule{
		UserID:         userID,
		Name:           "Stale Version Rule " + uuid.New().String(),
		Enabled:        true,
		Priority:       1,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"stale-fixture"},
		TagBehavior:    "add",
		Source:         "manual",
	}
	require.NoError(t, repo.Create(ctx, original))
	ruleID := original.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM categorization_rules WHERE id = $1`, ruleID) //nolint:errcheck
	})

	first, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	second, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)

	first.Priority = 9
	require.NoError(t, repo.Update(ctx, first))

	second.Priority = 3
	err = repo.Update(ctx, second)
	require.Error(t, err, "the second writer read a version the first has since superseded")
	assert.True(t, errors.Is(err, domain.ErrRuleConcurrentlyModified),
		"a genuine competing user edit must still be refused, got: %v", err)

	final, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	assert.Equal(t, 9, final.Priority, "the loser's write must not have landed")
}
