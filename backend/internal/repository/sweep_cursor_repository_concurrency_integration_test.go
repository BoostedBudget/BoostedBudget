//go:build integration

package repository_test

// sweep_cursor_repository_concurrency_integration_test.go — Issue #1477 follow-up (AC-3): the
// PREMISE of the ByPeriod floor, proved against a real database instead of asserted in a comment.
//
// jobs.TestSweepByPeriod_ExceedsTheSweepsWorstCaseRuntime says River's ByPeriod uniqueness is the
// SOLE defence against two runs of one sweep overlapping. That claim is only load-bearing if Save
// really is unguarded. This file pins that: Save is an unconditional
// `INSERT ... ON CONFLICT (sweep_name) DO UPDATE`, so a second write wins over the first
// unconditionally — including when it moves the watermark BACKWARDS, which is what a losing
// concurrent run's stale position does.
//
// ⚠ THIS TEST IS DELIBERATELY A DESCRIPTION OF CURRENT SEMANTICS, NOT A REQUIREMENT. If a
// compare-and-set, a version column or a monotonic guard is ever added to Save, this test SHOULD go
// red — and the correct response is to rewrite it around the new semantics and revisit the ByPeriod
// floor's rationale in the same change, not to delete it. That coupling is the point: the floor and
// the unguarded upsert are one argument, and neither half may move alone.
//
// ⚠ A MONOTONIC GUARD IS NOT AN OBVIOUS IMPROVEMENT, which is why one was not simply added. A
// forward-only Save would reject the legitimate reset a run performs after wrapping past the end of
// its candidate set, and a cursor that cannot be moved back is a cursor that can stall the sweep
// permanently — the exact failure sweep_cursor_repository.go's header says the two reset rules exist
// to prevent. The unguarded upsert is a considered choice; the untested DEPENDENCY on ByPeriod was
// not, and that is what these two tests fix.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestSweepCursorSave -tags integration -p 1 -count=1 -v

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// sweepCursorConcurrencyName is a cursor name used ONLY by this file. It is deliberately not one of
// the four production names: writing those from here would hand a foreign watermark to whichever
// sweep test ran next on this shared database.
const sweepCursorConcurrencyName = repository.SweepCursorName("integration_test_concurrency_probe")

func connectSweepCursorTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// clearProbeCursor removes this file's row before and after. context.Background(), never
// t.Context(): t.Context() is cancelled BEFORE t.Cleanup runs, so the DELETE would fail silently and
// leave a row behind on a database every other sweep test reads.
func clearProbeCursor(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	del := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM sweep_cursors WHERE sweep_name = $1`, string(sweepCursorConcurrencyName))
	}
	del()
	t.Cleanup(del)
}

// TestSweepCursorSave_IsLastWriterWins_IncludingBackwards is the premise.
func TestSweepCursorSave_IsLastWriterWins_IncludingBackwards(t *testing.T) {
	pool := connectSweepCursorTestDB(t)
	clearProbeCursor(t, pool)
	ctx := context.Background()

	repo := repository.NewSweepCursorRepository(pool)

	// Run A walks a long way and saves an ADVANCED position.
	advanced := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, sweepCursorConcurrencyName,
		repository.SweepPosition{TS1: &advanced, Exclusive: true}))

	loaded, err := repo.Load(ctx, sweepCursorConcurrencyName)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.True(t, advanced.Equal(*loaded.TS1), "the advanced position must be stored before the overwrite")

	// Run B started earlier from a STALE snapshot, got less far, and finishes second. There is no
	// lock, no CAS and no version, so its position is written unconditionally.
	stale := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, sweepCursorConcurrencyName,
		repository.SweepPosition{TS1: &stale, Exclusive: false}))

	loaded, err = repo.Load(ctx, sweepCursorConcurrencyName)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.True(t, stale.Equal(*loaded.TS1),
		"Save is an unconditional last-writer-wins upsert: the second write moved the watermark "+
			"BACKWARDS from %s to %s and run A's advance is gone with no error, no conflict and no "+
			"log. This is the premise of jobs.TestSweepByPeriod_ExceedsTheSweepsWorstCaseRuntime — "+
			"River's ByPeriod is the only thing keeping two runs from reaching this state. If a "+
			"compare-and-set or version column is added to Save, update BOTH tests together",
		advanced, stale)
	require.False(t, loaded.Exclusive,
		"the exclusive flag is overwritten by the same unconditional upsert, so a losing run can "+
			"also undo the exclusive advance that stepped the watermark past an over-cap tie group")

	// Sibling positive, mandatory: the row is not merely inert. Clear removes it, so the next Load
	// reports absence — which is how a run reaching the end of its candidate set resets the cursor.
	require.NoError(t, repo.Clear(ctx, sweepCursorConcurrencyName))
	loaded, err = repo.Load(ctx, sweepCursorConcurrencyName)
	require.NoError(t, err)
	require.Nil(t, loaded, "Clear must delete the row, so Load reports absence rather than a stale position")
}

// TestSweepCursorSave_ConcurrentWritersBothSucceed shows the same fact from the direction that
// matters operationally: two writers racing on ONE sweep_name neither block nor error. There is
// nothing for the losing run to detect.
func TestSweepCursorSave_ConcurrentWritersBothSucceed(t *testing.T) {
	pool := connectSweepCursorTestDB(t)
	clearProbeCursor(t, pool)
	ctx := context.Background()

	repo := repository.NewSweepCursorRepository(pool)

	first := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	errs := make(chan error, 2)
	go func() { errs <- repo.Save(ctx, sweepCursorConcurrencyName, repository.SweepPosition{TS1: &first}) }()
	go func() { errs <- repo.Save(ctx, sweepCursorConcurrencyName, repository.SweepPosition{TS1: &second}) }()
	require.NoError(t, <-errs, "neither concurrent Save may fail; there is no conflict to report")
	require.NoError(t, <-errs, "neither concurrent Save may fail; there is no conflict to report")

	loaded, err := repo.Load(ctx, sweepCursorConcurrencyName)
	require.NoError(t, err)
	require.NotNil(t, loaded, "one of the two writes must be stored")
	require.True(t, first.Equal(*loaded.TS1) || second.Equal(*loaded.TS1),
		"exactly one of the two racing positions survives and the other is discarded with no "+
			"signal to its writer — which is why the ByPeriod floor, not this repository, is the "+
			"mechanism that has to keep the two runs apart")
}
