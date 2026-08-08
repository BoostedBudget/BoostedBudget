//go:build integration

package jobs

// plaid_orphan_retry_sweep_cursor_integration_test.go — Issue #1477, real-Postgres proof for the
// FOURTH capped sweep of the same shape: the Plaid orphan retry.
//
// ⚠ WHAT MAKES A BACKLOG STARVE THIS SWEEP, precisely — and it is NOT the same condition as
// item_cleanup's. There, a successfully stamped row leaves the candidate set at once, so only an
// UNMARKABLE head starves. Here, GetRetryablePage's predicate is
// `resolved_at IS NULL AND access_token_encrypted IS NOT NULL AND token_expires_at > asOf` and there
// is deliberately NO attempt-count ceiling and NO backoff on last_retry_at, so a row whose
// /item/remove keeps FAILING is written to (retry_attempt_count, last_retry_error_code) and stays in
// the candidate set, at the same orphaned_at, on every subsequent run. A failing head is therefore
// enough on its own: no database failure is required. These tests inject exactly that — a remover
// that fails permanently for a nominated set of tokens — while the paging query stays entirely real.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... \
//	  -run TestPlaidOrphanRetrySweepCursor -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// recordingOrphanRemover stands in for the Plaid client. It records the credential it was handed —
// which is the only handle the sweep gives it — and fails permanently for a nominated set of them.
// A permanent failure here is a genuine Plaid failure, not a database one: that is precisely what
// keeps the row in this sweep's candidate set (see this file's header).
type recordingOrphanRemover struct {
	failFor map[string]bool
	tokens  []string
}

func (r *recordingOrphanRemover) RemoveItem(_ context.Context, accessToken string) error {
	r.tokens = append(r.tokens, accessToken)
	if r.failFor[accessToken] {
		return fmt.Errorf("simulated permanent Plaid failure removing %s", accessToken)
	}
	return nil
}

// attemptedSet is the membership view every assertion in this file uses. Deliberately membership,
// never a delta over a measured baseline: a broadened predicate moves a baseline and a result
// identically and the arithmetic stays green.
func (r *recordingOrphanRemover) attemptedSet() map[string]bool {
	set := make(map[string]bool, len(r.tokens))
	for _, tok := range r.tokens {
		set[tok] = true
	}
	return set
}

// orphanRetryToken is the credential seeded for a given record id. Deriving it from the id is what
// lets an assertion about "which ROWS did this run reach" be made from the tokens the remover saw.
func orphanRetryToken(id string) string { return "orphan-retry-tok-" + id }

// seedOrphanRetryCandidate inserts one unresolved orphan record holding a live credential, at an
// explicit orphaned_at so the test controls the sort key exactly.
func seedOrphanRetryCandidate(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, orphanedAt time.Time) string {
	t.Helper()
	id := uuid.NewString()

	encItem, err := enc.EncryptString("orphan-retry-item-" + id)
	require.NoError(t, err, "encrypt plaid item id")
	encToken, err := enc.EncryptString(orphanRetryToken(id))
	require.NoError(t, err, "encrypt retained credential")

	_, err = pool.Exec(context.Background(), `
		INSERT INTO plaid_orphaned_items (
			id, linked_item_id, plaid_item_id_encrypted, linked_item_status,
			orphaned_at, access_token_encrypted, token_expires_at
		) VALUES ($1, $2, $3, 'active', $4, $5, $6)
	`, id, uuid.NewString(), encItem, orphanedAt, encToken, time.Now().UTC().Add(24*time.Hour))
	require.NoError(t, err, "seed orphan retry candidate")

	// ⚠ context.Background(), never t.Context(): t.Context() is cancelled BEFORE t.Cleanup runs, so
	// the DELETE would fail silently. plaid_orphaned_items ACCUMULATES across runs and every
	// assertion below reasons about which rows rank first, so a leaked fixture row breaks a
	// DIFFERENT test on this shared database.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM plaid_orphaned_items WHERE id = $1`, id)
	})
	return id
}

func newTestPlaidOrphanRetryJob() *river.Job[PlaidOrphanRetryArgs] {
	return &river.Job[PlaidOrphanRetryArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, Kind: PlaidOrphanRetryArgs{}.Kind(), Queue: "low"},
		Args:   PlaidOrphanRetryArgs{TriggerSource: "sweep-cursor-integration"},
	}
}

// runOrphanRetrySweep performs ONE Work() invocation over the real repository, the real service and
// the real sweep-cursor repository. A fresh remover per run is what makes "which records did THIS
// run reach" answerable.
func runOrphanRetrySweep(t *testing.T, pool *pgxpool.Pool, pageSize, maxAttempts int, failFor map[string]bool) *recordingOrphanRemover {
	t.Helper()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	remover := &recordingOrphanRemover{failFor: failFor}
	svc := services.NewPlaidOrphanRetryService(
		repository.NewPlaidOrphanedItemRepository(pool, newJobsTestFieldEncryptor(t)), remover)
	svc.SetLogger(discard)

	worker := NewPlaidOrphanRetryWorker(PlaidOrphanRetryWorkerDeps{
		Service:      svc,
		Logger:       discard,
		PageSize:     pageSize,
		MaxPerSweep:  maxAttempts,
		SweepCursors: repository.NewSweepCursorRepository(pool),
	})
	require.NoError(t, worker.Work(context.Background(), newTestPlaidOrphanRetryJob()))
	return remover
}

// assertOrphanSweepSeesExactlySeeded is the shared-database setup guard: these tests reason about
// which of THEIR rows a capped run reaches, which is only meaningful if theirs are the ones that
// rank first.
func assertOrphanSweepSeesExactlySeeded(t *testing.T, pool *pgxpool.Pool, seeded []string) {
	t.Helper()
	repo := repository.NewPlaidOrphanedItemRepository(pool, newJobsTestFieldEncryptor(t))
	page, err := repo.GetRetryablePage(context.Background(), time.Now().UTC(), nil, len(seeded))
	require.NoError(t, err)
	got := make([]string, 0, len(page))
	for _, row := range page {
		got = append(got, row.ID)
	}
	require.ElementsMatch(t, seeded, got,
		"setup guard: the first %d retryable orphan records on this database must be exactly the rows this "+
			"test seeded; a foreign fixture row ranking ahead of them would invalidate every cap-based "+
			"assertion below", len(seeded))
}

// tokensOf maps seeded record ids to the credentials the sweep would present for them.
func tokensOf(ids ...string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[orphanRetryToken(id)] = true
	}
	return set
}

// ---------------------------------------------------------------------------------------------
// 1. THE STARVATION CASE. The head fails at PLAID — no database failure, no unmarkable row: this
//    sweep has no attempt ceiling and no backoff, so a permanently-failing record keeps its
//    orphaned_at and stays at the front of every page forever.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_FailingHead_LaterRunReachesTheRowBehindIt(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	base := time.Date(1992, 4, 1, 0, 0, 0, 0, time.UTC)
	headA := seedOrphanRetryCandidate(t, pool, enc, base)
	headB := seedOrphanRetryCandidate(t, pool, enc, base.Add(1*time.Second))
	behind := seedOrphanRetryCandidate(t, pool, enc, base.Add(2*time.Second))
	assertOrphanSweepSeesExactlySeeded(t, pool, []string{headA, headB, behind})

	// Plaid never removes these two, so they are never resolved and never leave the candidate set.
	failing := tokensOf(headA, headB)

	run1 := runOrphanRetrySweep(t, pool, 2, 2, failing)
	require.True(t, run1.attemptedSet()[orphanRetryToken(headA)], "run 1 must reach the first candidate")
	require.True(t, run1.attemptedSet()[orphanRetryToken(headB)], "run 1 must reach the second candidate")
	require.False(t, run1.attemptedSet()[orphanRetryToken(behind)], "run 1 is capped at 2 and must not reach the third")

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorOrphanRetry)
	require.True(t, present, "a run that stopped on its cap must persist where it got to")

	run2 := runOrphanRetrySweep(t, pool, 2, 2, failing)
	require.True(t, run2.attemptedSet()[orphanRetryToken(behind)],
		"THE DEFECT: the second run must reach the record ranked behind the permanently-failing head. "+
			"Without a persisted cursor it restarts from the oldest candidate and this one is never "+
			"retried on any run — until its credential expires and no automated path can ever remove it")
}

// ---------------------------------------------------------------------------------------------
// 2. THE MANDATORY SIBLING: an under-cap run still reaches everything in ONE run, and clears its
//    position rather than storing a past-the-end one.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_UnderCapRun_ReachesEveryRecordAndSkipsNone(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	base := time.Date(1992, 5, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedOrphanRetryCandidate(t, pool, enc, base),
		seedOrphanRetryCandidate(t, pool, enc, base.Add(1*time.Second)),
		seedOrphanRetryCandidate(t, pool, enc, base.Add(2*time.Second)),
		seedOrphanRetryCandidate(t, pool, enc, base.Add(3*time.Second)),
		seedOrphanRetryCandidate(t, pool, enc, base.Add(4*time.Second)),
	}
	assertOrphanSweepSeesExactlySeeded(t, pool, seeded)

	// Every record fails, so nothing leaves the candidate set mid-run and reaching all five is a
	// property of the paging, not of the set shrinking underneath it.
	run := runOrphanRetrySweep(t, pool, 2, 100, tokensOf(seeded...))

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[orphanRetryToken(id)], "under-cap run must reach every seeded record; missed %s", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorOrphanRetry)
	require.False(t, present,
		"a run that reached the END of the candidate set must CLEAR its position, not store a past-the-end one")
}

// ---------------------------------------------------------------------------------------------
// 3. WRAP-AROUND. A watermark past the end must not read as "nothing to do": the run clears it and
//    re-queries from the beginning WITHIN THE SAME RUN.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_PositionPastTheEnd_WrapsAndFindsCandidates(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	base := time.Date(1992, 6, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedOrphanRetryCandidate(t, pool, enc, base),
		seedOrphanRetryCandidate(t, pool, enc, base.Add(1*time.Second)),
	}
	assertOrphanSweepSeesExactlySeeded(t, pool, seeded)

	// orphaned_at is NOT NULL for this sweep, so "past the end" is a timestamp beyond every row's.
	pastTheEnd := time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)
	cursors := repository.NewSweepCursorRepository(pool)
	require.NoError(t, cursors.Save(context.Background(), repository.SweepCursorOrphanRetry,
		repository.SweepPosition{TS1: &pastTheEnd, Exclusive: true}))

	run := runOrphanRetrySweep(t, pool, 10, 100, tokensOf(seeded...))

	got := run.attemptedSet()
	for _, id := range seeded {
		require.True(t, got[orphanRetryToken(id)],
			"a cursor parked past the end must WRAP to the beginning within the same run and reach %s; "+
				"reporting an empty sweep here is the silent stall", id)
	}

	_, _, present := loadSweepCursorRow(t, pool, repository.SweepCursorOrphanRetry)
	require.False(t, present, "after wrapping and exhausting the set, the position must be cleared")
}

// ---------------------------------------------------------------------------------------------
// 4. THE EXCLUSIVE ADVANCE, which this sweep needs more literally than the two user sweeps do:
//    DeleteUserCascade inserts every one of an erased user's outstanding Items in ONE statement, so
//    they all take the same orphaned_at DEFAULT NOW(). A tie group that fills a whole capped run
//    would, with an inclusive resume, hand the next run the identical page forever — reintroducing
//    the starvation. repository.NextPosition steps past such a group after two runs; this proves it
//    for this sweep's own cursor type.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_TieGroupFillsTheCap_AdvancesPastItAndReachesWhatFollows(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	tied := time.Date(1992, 8, 1, 0, 0, 0, 0, time.UTC)
	tiedA := seedOrphanRetryCandidate(t, pool, enc, tied)
	tiedB := seedOrphanRetryCandidate(t, pool, enc, tied)
	tiedC := seedOrphanRetryCandidate(t, pool, enc, tied)
	behind := seedOrphanRetryCandidate(t, pool, enc, tied.Add(1*time.Second))
	assertOrphanSweepSeesExactlySeeded(t, pool, []string{tiedA, tiedB, tiedC, behind})

	// Nothing ever drains: the tie group is three rows against a cap of two, and every one of them
	// fails at Plaid permanently.
	failing := tokensOf(tiedA, tiedB, tiedC, behind)

	// Run 1 starts from the beginning and spends its whole cap inside the tie group.
	runOrphanRetrySweep(t, pool, 2, 2, failing)
	_, exclusive, present := loadSweepCursorRow(t, pool, repository.SweepCursorOrphanRetry)
	require.True(t, present)
	require.False(t, exclusive,
		"a run that started from the beginning saves an INCLUSIVE position; going exclusive here would "+
			"skip rows tied on the boundary that were never reached")

	// Run 2 resumes AT that key, spends its whole cap on the same key again, and that is the only
	// case in which the position is allowed to go exclusive.
	runOrphanRetrySweep(t, pool, 2, 2, failing)
	_, exclusive, present = loadSweepCursorRow(t, pool, repository.SweepCursorOrphanRetry)
	require.True(t, present)
	require.True(t, exclusive,
		"a capped run that made NO progress along the timestamp axis must save an EXCLUSIVE position; "+
			"saving it inclusively hands run 3 the identical page forever")

	run3 := runOrphanRetrySweep(t, pool, 2, 2, failing)
	require.True(t, run3.attemptedSet()[orphanRetryToken(behind)],
		"the record ranked BEHIND an over-cap tie group in which nothing ever drains must still be "+
			"reached; that is what the exclusive advance buys, and it is bought at the documented price "+
			"of the group's own third row (see repository.NextPosition)")
}

// ---------------------------------------------------------------------------------------------
// 5. THE TIE-BREAKER, on genuinely identical timestamps. Two erasures in the same microsecond is
//    all it takes, and orphaned_at alone is then not a total order: without the id tie-breaker the
//    keyset predicate either repeats a page forever or skips the rest of the tie group. Every
//    fixture in this repo has historically seeded strictly increasing timestamps, which is why this
//    shipped untested — a tie-breaker no test ever ties on is indistinguishable from an absent one.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_IdenticalOrphanedAt_TotalOrderAcrossPageBoundaries(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	tied := time.Date(1992, 7, 1, 0, 0, 0, 0, time.UTC)
	seeded := []string{
		seedOrphanRetryCandidate(t, pool, enc, tied),
		seedOrphanRetryCandidate(t, pool, enc, tied),
		seedOrphanRetryCandidate(t, pool, enc, tied),
		seedOrphanRetryCandidate(t, pool, enc, tied),
		seedOrphanRetryCandidate(t, pool, enc, tied),
	}
	assertOrphanSweepSeesExactlySeeded(t, pool, seeded)

	// All failing, so the tie group stays whole and the keyset predicate carries the entire burden
	// of neither repeating nor skipping across three page boundaries.
	run := runOrphanRetrySweep(t, pool, 2, 100, tokensOf(seeded...))

	seen := map[string]int{}
	for _, tok := range run.tokens {
		seen[tok]++
	}
	for _, id := range seeded {
		require.Equal(t, 1, seen[orphanRetryToken(id)],
			"record %s in the identical-orphaned_at tie group must be visited EXACTLY once across the page "+
				"boundaries; %d visits means the id tie-breaker is not making the order total",
			id, seen[orphanRetryToken(id)])
	}
	require.Len(t, seen, len(seeded), "the run must have visited exactly the tied records and nothing else")
}
