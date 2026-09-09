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

	// Two independent reads of the SAME pre-race row, each carrying its own copy of the
	// updated_at this write believes is current — exactly what UpdateRule and ToggleRule each do
	// today (GetByID, mutate the in-memory copy, Update). Fetching both BEFORE either writer
	// starts is what makes the race genuine: neither writer has seen the other's change.
	repairReader, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	toggleReader, err := repo.GetByID(ctx, ruleID)
	require.NoError(t, err)
	require.Equal(t, repairReader.UpdatedAt, toggleReader.UpdatedAt, "both readers must observe the same pre-race version")

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
