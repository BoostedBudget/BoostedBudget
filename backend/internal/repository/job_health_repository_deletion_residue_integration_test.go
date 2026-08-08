//go:build integration

package repository

// Integration tests for JobHealthRepository.GetDeletionResidueStats (Issue #1504) — the READ side of
// RecordDeletionResidue. The write has been integration-tested since #1504's first half landed
// (profile_repository_deletion_residue_integration_test.go); nothing read it, which is what these
// tests exist to change.
//
// A REAL DATABASE IS REQUIRED, AND NOT AS A FORMALITY. Three of the facts asserted below are
// invisible to pgxmock:
//
//  1. COUNT(*) / MAX(created_at) / EXTRACT(EPOCH FROM ...) over a seeded population — the brief's
//     requirement that a count be asserted against real rows rather than merely "the field exists".
//  2. That an ORDINARY deletion's audit rows are not counted. The predicate is a LIKE prefix; a
//     mocked pool returns whatever the test hands it regardless of what the predicate says, so only a
//     real server can refute a broadened prefix.
//  3. That the predicate is served by idx_audit_logs_deletion_residue rather than by a sequential
//     scan of audit_logs. audit_logs is append-only and unbounded; idx_audit_logs_action exists but
//     CANNOT serve `action LIKE 'prefix%'` under this database's en_US.utf8 collation (measured: seq
//     scan over 200k rows, 2470 buffers, 13.4ms → Index Only Scan, 2 buffers, 0.04ms once the partial
//     index exists). See migration 000138.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestJobHealthRepository_Integration_DeletionResidue -tags integration -p 1 -v

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// deletionResidueIndexName is the partial index migration 000138 creates for
// GetDeletionResidueStats' predicate. It lives here rather than beside the query it serves because the
// only consumer is the guard below, and an unreferenced production constant is a lint finding — see
// the query's own doc comment, which names the index in prose for a reader of the production file.
const deletionResidueIndexName = "idx_audit_logs_deletion_residue"

// createResidueStatsUser makes a real users row (audit_logs.resource_id is a UUID and the table
// carries an FK on user_id, so a fabricated string will not do) and registers cleanup.
//
// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs, so
// these DELETEs would fail silently and leak rows into a different test on the shared database.
func createResidueStatsUser(t *testing.T, pool *pgxpool.Pool, label string) string {
	t.Helper()
	repo := NewProfileRepository(pool, newTestFieldEncryptor(t))
	suffix := fmt.Sprintf("%s_%d", label, time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_residue_stats_" + suffix,
		Email:              "residue-stats-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user.ID
}

// TestJobHealthRepository_Integration_DeletionResidue_SeededPopulationIsCounted is the brief's RED
// test: a residue record is visible through the surface. It asserts against a SEEDED population
// (three records across two users), not merely that a field exists — a surface that returned a
// constant would pass the latter.
//
// ⚠ IT ASSERTS MEMBERSHIP, NOT A DELTA, AND THAT REPLACES WHAT IT USED TO ASSERT. The original
// assertion was `baseline + 3`, and a delta over a measured baseline is blind to any mutation that
// shifts the baseline and the result by the SAME amount. Predicate WIDENING is exactly that class —
// broaden `user.deletion_residue.%` to `user.%` and both readings grow by the same set of extra rows,
// so `baseline + 3` still holds. That mutation passed this test once. (The delta is kept below,
// because "our three rows are among what it counted" is still worth saying; it is no longer the only
// thing said.)
//
// The membership assertion pins the surface's count against an INDEPENDENT enumeration of what a
// residue record is: exact action EQUALITY, computed here, rather than the LIKE prefix the query
// under test embeds. Widening the query's pattern makes the two disagree; narrowing it does too. And
// the decoy rows are seeded by this test rather than assumed to exist, so the comparison cannot pass
// vacuously on an otherwise-empty audit_logs — a passing control validates the pattern, not the
// input.
//
// The baseline still has to be measured rather than assumed zero: GetDeletionResidueStats scans the
// whole audit_logs table (it is a health gauge, not a per-test query), so it is not isolated from
// rows another package's fixtures left behind.
func TestJobHealthRepository_Integration_DeletionResidue_SeededPopulationIsCounted(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewJobHealthRepository(pool)
	profileRepo := NewProfileRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()

	baseline, _, err := repo.GetDeletionResidueStats(ctx)
	require.NoError(t, err)

	userA := createResidueStatsUser(t, pool, "a")
	userB := createResidueStatsUser(t, pool, "b")

	// Written through RecordDeletionResidue itself, not by hand-rolled INSERTs: if the writer's
	// action prefix and this reader's predicate ever drift apart, this is what notices. That shared
	// constant is the whole reason deletionResidueActionPrefix exists.
	require.NoError(t, profileRepo.RecordDeletionResidue(ctx, userA, DeletionResiduePaddleSubscriptionCancelled))
	require.NoError(t, profileRepo.RecordDeletionResidue(ctx, userB, DeletionResiduePaddleSubscriptionCancelled))
	require.NoError(t, profileRepo.RecordDeletionResidue(ctx, userB, DeletionResiduePaddleSubscriptionCancelled))

	// The decoys a WIDENED predicate would sweep up: the audit rows an ordinary, harmless deletion
	// leaves. They live inside `user.` and outside the residue namespace, and they are seeded HERE so
	// the membership assertion below has something to be non-vacuous about even on a fresh database.
	// Their resource_id is userA's, so createResidueStatsUser's cleanup takes them.
	for _, action := range []string{"user.deletion_requested", "user.deleted", "user.updated"} {
		_, insErr := pool.Exec(ctx,
			`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, $1, 'user', $2)`,
			action, userA)
		require.NoError(t, insErr)
	}

	count, newest, err := repo.GetDeletionResidueStats(ctx)
	require.NoError(t, err)

	// MEMBERSHIP. Enumerated by exact equality on the full action, which is what a residue record IS;
	// the query under test reaches the same rows through a LIKE prefix. Any widening or narrowing of
	// that prefix makes the two numbers disagree, whatever the baseline does.
	var exactlyResidue int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = $1`,
		deletionResidueActionPrefix+string(DeletionResiduePaddleSubscriptionCancelled),
	).Scan(&exactlyResidue))
	require.GreaterOrEqual(t, exactlyResidue, 3,
		"the three rows just written must be in the reference set, or the comparison below proves nothing")
	assert.Equal(t, exactlyResidue, count,
		"the surface must count EXACTLY the residue rows and nothing else — three ordinary `user.` deletion rows were seeded above precisely so a predicate broadened to `user.%` shows up here as a disagreement rather than as a matching shift in both readings")

	assert.Equal(t, baseline+3, count,
		"every residue record must be counted — one per occurrence, not one per user: two separate revocations against the same user are two subscriptions somebody has to restore")
	assert.Greater(t, newest, time.Duration(0),
		"the newest record's age must be a real, positive elapsed time")
	assert.Less(t, newest, 5*time.Minute,
		"a record written moments ago must read as moments old — MAX(created_at), not MIN")
}

// TestJobHealthRepository_Integration_DeletionResidue_OrdinaryDeletionIsNotCounted is the mandatory
// sibling. Without it the surface could report a constant, or count every audit row, and the test
// above would still pass.
//
// The rows seeded here are the ones an ORDINARY, fully successful deletion leaves behind — the
// deletion that harmed nobody and needs no operator. They share the `user.` namespace and two of them
// share the word `deletion`, so they refute a predicate broadened to `user.%` or to `%deletion%` as
// well as one that simply counts audit_logs.
func TestJobHealthRepository_Integration_DeletionResidue_OrdinaryDeletionIsNotCounted(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	baseline, baselineNewest, err := repo.GetDeletionResidueStats(ctx)
	require.NoError(t, err)

	userID := createResidueStatsUser(t, pool, "ordinary")
	for _, action := range []string{
		"user.deletion_requested",
		"user.deletion_grace_expired",
		"user.deleted",
		"user.updated",
	} {
		_, err := pool.Exec(ctx,
			`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, $1, 'user', $2)`,
			action, userID)
		require.NoError(t, err)
	}

	count, newest, err := repo.GetDeletionResidueStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, baseline, count,
		"an ordinary successful deletion produces NO residue, so the surface must not move — a residue record means a subscription was cancelled for somebody who kept their account")

	// The age assertion is expressed as "must not go BACKWARDS", not as equality, because for an
	// unchanged population the newest record's age grows with wall-clock time between the two reads.
	// It is still the exact falsifier: the rows seeded above were written moments ago, so if the
	// predicate counted any of them the newest age would COLLAPSE to ~0.
	if baseline == 0 {
		assert.Equal(t, time.Duration(0), newest,
			"with no residue anywhere the age must stay a clean zero — an ordinary deletion must not make it start ticking")
	} else {
		assert.GreaterOrEqual(t, newest, baselineNewest,
			"the newest residue record's age can only grow while no new residue is written; a drop means an ordinary deletion's audit row was miscounted as residue")
	}
}

// TestJobHealthRepository_Integration_DeletionResidue_IsServedByThePartialIndex is the bounded-read
// proof, and it is the lesson the /admin/health batch learned the hard way applied to a far larger
// table: audit_logs has no pruning path at all, so any predicate that cannot use an index degrades
// forever.
//
// Asserted structurally (the index's own predicate text) rather than through EXPLAIN, deliberately:
// EXPLAIN's plan choice depends on table statistics, and on a nearly-empty test database Postgres
// legitimately prefers a sequential scan over any index — an EXPLAIN assertion here would be a
// coin-flip that passes for the wrong reason. What actually has to hold is narrower and exact:
// Postgres only uses a partial index when it can PROVE the query's predicate implies the index's, and
// for a LIKE clause that proof is an expression-tree equality. So the index predicate and the query
// predicate must be the SAME literal. That is what is checked.
func TestJobHealthRepository_Integration_DeletionResidue_IsServedByThePartialIndex(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	var indexDef string
	err := pool.QueryRow(ctx,
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
		deletionResidueIndexName).Scan(&indexDef)
	require.NoError(t, err,
		"migration 000138 must have created %s — without it this COUNT is a sequential scan of an append-only, unbounded table", deletionResidueIndexName)

	// The exact predicate GetDeletionResidueStats embeds. Not a paraphrase: an equivalent-but-different
	// spelling (a different quote style is fine, a different pattern is not) silently costs the index.
	wantPredicate := fmt.Sprintf("(action)::text ~~ '%s%%'::text", deletionResidueActionPrefix)
	assert.Contains(t, indexDef, wantPredicate,
		"the index predicate must be the same LIKE clause the query uses, or Postgres cannot prove implication and will fall back to a full scan")

	// And the query must embed that pattern as a LITERAL. A bound parameter ($1) is not provably
	// implied by the index predicate at plan time, so parameterising it — the reflex — silently
	// reverts this to the full scan.
	assert.Contains(t, deletionResidueStatsQuery, "'"+deletionResidueActionPrefix+"%'",
		"the pattern must be inlined as a constant; a bind parameter defeats the partial index")
	assert.False(t, strings.Contains(deletionResidueStatsQuery, "$1"),
		"there is nothing here for a bind parameter to carry, and one would cost the index")
}
