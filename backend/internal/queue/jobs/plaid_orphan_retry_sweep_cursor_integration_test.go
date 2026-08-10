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
	"sort"
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
	return seedOrphanRetryCandidateWithID(t, pool, enc, uuid.NewString(), orphanedAt)
}

// seedOrphanRetryCandidateWithID is seedOrphanRetryCandidate with the row identity supplied by the
// caller. It exists for the ONE property a random id cannot express: which member of a tie group is
// at its HEAD and which is at its TAIL. Ties break on `id ASC`, so a test that needs "these two rows
// rank ahead of that one, at the same orphaned_at" must choose the ids, not accept them.
func seedOrphanRetryCandidateWithID(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, id string, orphanedAt time.Time) string {
	t.Helper()

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

// sortedOrphanIDs returns n fresh uuids in the order PostgreSQL will rank them.
//
// Safe to compare as Go strings: PostgreSQL orders `uuid` by its 16 raw bytes, and uuid.NewString
// emits the canonical LOWERCASE hex form with dashes at fixed positions — over the alphabet
// [0-9a-f] the ASCII order of a hex digit is its numeric order, so a bytewise string comparison of
// two canonical uuids and a bytewise comparison of the values they encode agree everywhere.
func sortedOrphanIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, uuid.NewString())
	}
	sort.Strings(ids)
	return ids
}

// orphanRecordIsResolved reports whether the sweep actually removed this record's Plaid Item.
// Reading resolved_at rather than the remover's tally is deliberate: "the Item is gone at Plaid and
// the credential is purged" is a fact about the ROW, and it is the fact that decides whether an
// outstanding Item was really disposed of.
func orphanRecordIsResolved(t *testing.T, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var resolvedAt *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT resolved_at FROM plaid_orphaned_items WHERE id = $1`, id).Scan(&resolvedAt))
	return resolvedAt != nil
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
// 4b. WHAT THE EXCLUSIVE ADVANCE ACTUALLY COSTS — and it is NOT what NextPosition's comment used to
//     say it was. That comment bounded the sacrifice to a tie group "in which no row ever leaves the
//     candidate set", and test 4 above builds exactly that (all three tied rows failing). The
//     precondition is OVERSTATED: only the rows at the HEAD of the group need keep failing, because
//     only they have to fill the cap. The row at the TAIL may be FULLY PROCESSABLE and is sacrificed
//     anyway.
//
//     These two tests construct the case rather than argue it: 4b proves the tail row is never
//     reached, 4c proves — with the identical fixture — that it would have been resolved had it
//     been reached, so 4b's negative is a sacrifice and not a row that was never processable.
//
//     ⚠ SEVERITY RETRACTED IN PLACE — the retracted sentence is reproduced rather than referenced.
//     This block used to continue: "For this sweep that row is a removable Plaid Item that is never
//     retried on any run — and once its 30-day token TTL elapses (`token_expires_at`, migration
//     000135) no automated path can ever remove it." True of the fixture below, which runs at a cap
//     of 2; OVERSTATED as a production claim. The sacrifice requires an OVER-CAP tie group, and
//     maxOrphanRetriesPerSweep is 1,000, so production would need ONE erased user holding more than
//     1,000 Plaid Items in a single DeleteUserCascade — which inserts that user's outstanding Items
//     in one statement, so the group is that user's item count. Not reachable. NOTHING in these two
//     tests changes: the mechanism is real, and the caps of 2 and 3 are precisely what make it
//     observable. See repository.NextPosition and this repo's sweep_cursor_repository.go header,
//     which carry the same retraction.
// ---------------------------------------------------------------------------------------------

func TestPlaidOrphanRetrySweepCursor_OverCapTieGroup_SacrificesAProcessableTailRow(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	tied := time.Date(1992, 9, 1, 0, 0, 0, 0, time.UTC)
	ids := sortedOrphanIDs(3)
	headA := seedOrphanRetryCandidateWithID(t, pool, enc, ids[0], tied)
	headB := seedOrphanRetryCandidateWithID(t, pool, enc, ids[1], tied)
	tail := seedOrphanRetryCandidateWithID(t, pool, enc, ids[2], tied)
	behind := seedOrphanRetryCandidate(t, pool, enc, tied.Add(1*time.Second))
	assertOrphanSweepSeesExactlySeeded(t, pool, []string{headA, headB, tail, behind})

	// ⚠ ONLY THE HEAD FAILS. `tail` is a perfectly removable Item: had any run presented its
	// credential, Plaid would have accepted and MarkResolved would have purged it. That is the whole
	// point — the sacrifice does not need an undrainable group, only an undrainable HEAD.
	failing := tokensOf(headA, headB, behind)

	attempted := map[string]bool{}
	for run := 1; run <= 3; run++ {
		for tok := range runOrphanRetrySweep(t, pool, 2, 2, failing).attemptedSet() {
			attempted[tok] = true
		}
	}

	// The sibling positive, and it is mandatory: without it this test would also pass if the sweep
	// did nothing whatsoever. The exclusive advance is doing its job — the row RANKED BEHIND the
	// over-cap tie group is reached, which is what the trade buys.
	require.True(t, attempted[orphanRetryToken(behind)],
		"the row ranked behind the tie group must be reached by run 3; if it is not, the exclusive "+
			"advance is broken and this test's negative below proves nothing")

	// THE SACRIFICE, asserted as membership over the union of all three runs — never as a count
	// delta, which a broadened predicate would shift identically on both sides.
	require.False(t, attempted[orphanRetryToken(tail)],
		"THE DOCUMENTED PRICE OF THE EXCLUSIVE ADVANCE: the third row of an over-cap tie group is not "+
			"reached on ANY run, and a wrap does not help (a wrap restarts at the beginning and re-reads "+
			"the same first cap-worth). Note that only headA/headB fail here — %s is fully processable "+
			"and is skipped anyway. If this assertion ever fails because the cursor learned to advance "+
			"WITHIN a tie group, that is the fix landing, not a regression: delete this test and update "+
			"repository.NextPosition's comment and #1477's body together", tail)

	require.False(t, orphanRecordIsResolved(t, pool, tail),
		"and the consequence is not bookkeeping: %s's Plaid Item is still outstanding and still "+
			"possibly billing, with no run that will ever attempt it before its token_expires_at "+
			"deadline retires the only credential that could", tail)
}

func TestPlaidOrphanRetrySweepCursor_TieGroupTailRow_IsResolvedOnceTheCapReachesIt(t *testing.T) {
	pool := connectJobsTestDB(t)
	resetSweepCursor(t, pool, repository.SweepCursorOrphanRetry)
	enc := newJobsTestFieldEncryptor(t)

	// Identical fixture to 4b — same shape, same failing head, same tail row — with ONE difference:
	// a cap of 3 instead of 2, so the tie group is no longer over-cap. Everything else being equal is
	// what makes this a control for that test rather than a separate scenario.
	tied := time.Date(1992, 10, 1, 0, 0, 0, 0, time.UTC)
	ids := sortedOrphanIDs(3)
	headA := seedOrphanRetryCandidateWithID(t, pool, enc, ids[0], tied)
	headB := seedOrphanRetryCandidateWithID(t, pool, enc, ids[1], tied)
	tail := seedOrphanRetryCandidateWithID(t, pool, enc, ids[2], tied)
	behind := seedOrphanRetryCandidate(t, pool, enc, tied.Add(1*time.Second))
	assertOrphanSweepSeesExactlySeeded(t, pool, []string{headA, headB, tail, behind})

	failing := tokensOf(headA, headB, behind)

	attempted := runOrphanRetrySweep(t, pool, 2, 3, failing).attemptedSet()

	require.True(t, attempted[orphanRetryToken(tail)],
		"a cap of 3 admits the whole tie group, so the tail row IS presented to Plaid")
	require.True(t, orphanRecordIsResolved(t, pool, tail),
		"and it is resolved — which is what makes the row 4b never reaches a SACRIFICED row rather "+
			"than an unprocessable one")
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
