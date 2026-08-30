//go:build integration

package database

// #1569 round 14 task 9 — the accounts leg of BackfillUserAccountIDs must not STARVE across boots.
//
// WHAT THESE TESTS EXIST TO CATCH. Round 14 task 8 bounded this backfill (page 500, cap 1000 per
// run) so a large backlog could not block boot. The bound introduced a liveness REGRESSION that the
// unbounded original did not have: the keyset cursor restarted at the beginning on EVERY
// invocation, and rows that are PERMANENTLY unrepairable (linked_item with no institution_id, or an
// undecryptable one) are skipped but STAY in the `a.user_account_id IS NULL` predicate. So more
// than a cap's worth of such rows sorting ahead of a repairable row consumed every future boot's
// budget and the repairable row was reached NEVER — not slowly, never. Idempotence does not help:
// it only removes rows that actually get UPDATEd.
//
// ⚠ THESE MUST BE INTEGRATION TESTS, NOT pgxmock ONES. pgxmock does not evaluate the SQL predicate
// at all — a mock-only test passes while the real keyset query is wrong (this branch's recorded
// lesson, and the reason backfill_user_account_paging_test.go's second page could not prove
// forward progress). Everything asserted here is asserted against real PostgreSQL.
//
// The properties, one test each. Items 5 and 6 are #1569 round 15 task 9, and items 2 and 3 were
// REWRITTEN by it: the wrap test did not exercise the wrap at all (verified by mutation — the whole
// branch could be deleted and it still passed) and the tie-group test pinned a skip that has since
// been narrowed to the ceiling.
//  1. across boots the run RESUMES, so a repairable row behind an over-cap wall of unrepairable
//     rows is reached (the starvation fix);
//  2. the persisted cursor WRAPS on the SAME boot that finds its watermark stale, so a row created
//     BEHIND the watermark is repaired then rather than one boot later;
//  3. an OVER-CAP tie group on one created_at is DRAINED rather than stepped over, so no reachable
//     row inside it is skipped;
//  4. a tie group too large to drain within the ceiling is still escaped — the residual, pinned as
//     the documented price rather than left implicit;
//  5. a run that starts at the beginning and exhausts its scan on an exact page boundary neither
//     wraps nor stores a watermark (the ping-pong);
//  6. two boots racing on the shared cursor converge and never lose a repairable row.
//
// A further test covers the separate all-zero-uuid defect: see
// TestBackfillAccountsCursor_AllZeroUUIDAccountIsReached.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// backfillCursorFixture is one user with two linked_items: `badItemID` has a NULL institution_id,
// so every account under it is PERMANENTLY unrepairable (the backfill cannot compute a blind index
// and skips it forever); `goodItemID` has a decryptable one, so accounts under it are repaired.
type backfillCursorFixture struct {
	pool       *pgxpool.Pool
	userID     string
	badItemID  string
	goodItemID string
}

func newBackfillCursorFixture(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor) *backfillCursorFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.New().String()[:8]

	// Start from a clean cursor: the row is global (no user scope), so a leftover from an earlier
	// test would silently change what this one measures.
	clearBackfillCursorRow(t, pool)
	t.Cleanup(func() { clearBackfillCursorRow(t, pool) })

	userID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|cur-"+userID, "cur-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	encInstID, err := enc.EncryptString("ins-cur-" + suffix)
	require.NoError(t, err)

	badItemID, goodItemID := uuid.New().String(), uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', NULL),
		       ($4, $2, $5, 'enc-test-token', $6)
	`, badItemID, userID, "item-bad-"+badItemID, goodItemID, "item-good-"+goodItemID, encInstID)
	require.NoError(t, err)

	return &backfillCursorFixture{pool: pool, userID: userID, badItemID: badItemID, goodItemID: goodItemID}
}

// seedAccount inserts one account with a NULL user_account_id at an EXPLICIT created_at, which is
// the leading component of the scan's sort key — the whole point of these tests is which rows a
// bounded run reaches, so the order must be pinned rather than left to insertion timing.
func (f *backfillCursorFixture) seedAccount(t *testing.T, itemID string, createdAt time.Time) string {
	t.Helper()
	return f.seedAccountWithID(t, uuid.New().String(), itemID, createdAt)
}

func (f *backfillCursorFixture) seedAccountWithID(t *testing.T, accID, itemID string, createdAt time.Time) string {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask, created_at)
		VALUES ($1, $2, $3, $4, 'Cursor Test Account', 'depository', 'checking', NULL, $5)
	`, accID, itemID, f.userID, "plaid-"+accID, createdAt)
	require.NoError(t, err)
	t.Cleanup(func() { f.pool.Exec(context.Background(), "DELETE FROM accounts WHERE id = $1", accID) }) //nolint:errcheck
	return accID
}

func (f *backfillCursorFixture) userAccountID(t *testing.T, accID string) *string {
	t.Helper()
	var ua *string
	require.NoError(t, f.pool.QueryRow(context.Background(),
		"SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&ua))
	return ua
}

// cleanupSynthesised removes any user_accounts rows the backfill minted for this fixture's user.
// Registered as a t.Cleanup by every test that lets a repair happen.
func (f *backfillCursorFixture) cleanupSynthesised(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), "DELETE FROM user_accounts WHERE user_id = $1", f.userID) //nolint:errcheck
	})
}

func clearBackfillCursorRow(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"DELETE FROM sweep_cursors WHERE sweep_name = $1", backfillAccountsSweepCursorName)
	require.NoError(t, err)
}

// TestBackfillAccountsCursor_ResumesAcrossBootsPastAnUnrepairableWall is the starvation test.
//
// RED before the persisted cursor: boot 1 reaches the two unrepairable rows and stops at the cap;
// boot 2 restarts from the beginning and reaches the SAME two; the repairable row is never fetched
// and its user_account_id stays NULL forever. The assertion below fails on exactly that.
func TestBackfillAccountsCursor_ResumesAcrossBootsPastAnUnrepairableWall(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2) // page 2, cap 2 — the production shape, expressed in 2 rows

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Three permanently-unrepairable rows sort BEFORE the repairable one. With a cap of 2 they are
	// more than one run's budget, which is the whole precondition of the defect.
	for i := 0; i < 3; i++ {
		f.seedAccount(t, f.badItemID, base.Add(time.Duration(i)*time.Minute))
	}
	repairable := f.seedAccount(t, f.goodItemID, base.Add(10*time.Minute))

	// Boot 1: consumed entirely by the wall.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	require.Nil(t, f.userAccountID(t, repairable),
		"boot 1's whole budget is the unrepairable wall; the repairable row must not have been reached yet")

	// Later boots must RESUME rather than restart. The bound is #1477's, restated for this scan and
	// asserted rather than hand-waved: for N candidates with distinct sort keys and a cap C > 1,
	// every candidate is REACHED within ceil(N/(C-1)) + 1 runs. C-1, not C, because an inclusive
	// resume re-visits the single boundary row the previous run stopped on, so a run's net advance
	// is C-1 previously-unreached rows. Here N=4 (3 unrepairable + 1 repairable) and C=2, so 5 runs.
	// A cursor that restarts at the beginning reaches the repairable row on NONE of them.
	const n, capPerRun = 4, 2
	bound := (n+(capPerRun-1)-1)/(capPerRun-1) + 1
	for boot := 2; boot <= bound; boot++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		if f.userAccountID(t, repairable) != nil {
			return
		}
	}
	t.Fatalf("the repairable row behind an over-cap unrepairable wall was not reached within %d boots; "+
		"a per-run cursor that restarts at the beginning starves it forever", bound)
}

// TestBackfillAccountsCursor_WrapsSoARowBehindTheWatermarkIsRepairedOnTheSameBoot pins the OTHER
// starvation — the one the persisted cursor would introduce if it only ever moved forward. A row
// created with a created_at BEHIND the stored watermark sorts before it and would never be selected
// again.
//
// ⚠ THIS TEST DID NOT DISCRIMINATE UNTIL #1569 ROUND 15 TASK 9, and it was reported as
// mutant-validated when it was not. Its previous fixture left a row AHEAD of the watermark, so
// boot 2 took the ordinary "found rows, then exhausted the scan, so clear" path and the wrap branch
// was never entered; the row behind the watermark was then repaired on boot 3 by the clear, and the
// test — which allowed three boots — passed. Verified by mutation: with the wrap branch disabled
// outright the old test still passed.
//
// What makes this version a discriminator is that the wrap's ONLY observable effect is timing —
// it repairs, on THIS boot, rows that the clear-on-exhaustion path would leave to the NEXT one — so
// the fixture must make the resumed page genuinely EMPTY and the assertion must allow exactly ONE
// boot. Nothing here depends on scheduling or on generated ids.
func TestBackfillAccountsCursor_WrapsSoARowBehindTheWatermarkIsRepairedOnTheSameBoot(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	f.seedAccountWithID(t, "bbbbbbbb-1111-4111-8111-bbbbbbbbbbbb", f.badItemID, base.Add(time.Minute))
	last := f.seedAccountWithID(t, "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb", f.badItemID, base.Add(2*time.Minute))

	// Boot 1 spends its whole cap and therefore STORES a watermark at the last row it reached.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	stored := backfillCursorRowTS(t, pool)
	require.NotNil(t, stored, "boot 1 must stop at its cap and store a watermark; without one there is no wrap to test")
	require.True(t, stored.Equal(base.Add(2*time.Minute)), "the watermark must be the last row boot 1 reached")

	// Make the watermark STALE: the row it points at leaves the candidate set (repaired elsewhere,
	// or the account deleted), so a resumed page finds NOTHING at or after it. This is the only
	// state in which the wrap branch is reachable at all.
	_, err := pool.Exec(ctx, "DELETE FROM accounts WHERE id = $1", last)
	require.NoError(t, err)

	// And a repairable row appears BEHIND the watermark.
	behind := f.seedAccount(t, f.goodItemID, base.Add(-time.Hour))

	// ONE boot. It must find its resume page empty, recognise the watermark as past the end, clear
	// it and re-ask FROM THE BEGINNING within this same run.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))

	require.NotNil(t, f.userAccountID(t, behind),
		"a row sorting behind a STALE persisted watermark must be repaired on the very boot that finds "+
			"the watermark empty: the run has to clear it and re-query from the start within the same "+
			"run. Leaving it to the next boot is the tail-starvation half of the trade, and this scan's "+
			"clock is restarts, not a schedule")
}

// TestBackfillAccountsCursor_OverCapTieGroupIsDrainedRatherThanSkipped is defect C2.
//
// THE BUG IT FAILS AGAINST, which is the CURRENT committed code. The persisted position is a
// TIMESTAMP only (never a row id — see backfill_accounts_cursor.go on why that is a GDPR
// invariant), so a resumed run that spends its whole cap inside ONE created_at could not say
// "resume in the middle of this tie group". It advanced EXCLUSIVELY past the whole group instead,
// and the group's rows past the first cap-worth were then reached on no boot, ever — including
// after a wrap, which restarts at the beginning and re-reads the same first cap-worth. The rows
// lost that way may be perfectly repairable; they are skipped without ever being looked at.
//
// THE FIX ASSERTED HERE: the exclusive advance is now conditional on having actually EXHAUSTED the
// tie group. A run that reaches its cap inside the group it resumed at keeps paging — past the cap,
// bounded by backfillAccountsTieGroupCeiling — until it leaves that created_at, at which point an
// ordinary inclusive save is both safe and forward. See the sibling test for what happens when a
// group is bigger than the ceiling.
//
// RED against the committed code on the `sacrificed` assertion: that row is repairable, sorts last
// inside the tie group, and the exclusive advance steps over it permanently.
func TestBackfillAccountsCursor_OverCapTieGroupIsDrainedRatherThanSkipped(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	tie := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// ids are explicit so the tie group's order under `ORDER BY a.created_at, a.id` is pinned: the
	// two unrepairable rows fill the cap, the repairable tail row sorts last inside the group.
	f.seedAccountWithID(t, "11111111-1111-4111-8111-111111111111", f.badItemID, tie)
	f.seedAccountWithID(t, "22222222-2222-4222-8222-222222222222", f.badItemID, tie)
	sacrificed := f.seedAccountWithID(t, "33333333-3333-4333-8333-333333333333", f.goodItemID, tie)
	ahead := f.seedAccount(t, f.goodItemID, tie.Add(10*time.Minute))

	// Boot 1 spends its cap on the wall from a cold start; boot 2 resumes INSIDE the tie group and
	// must drain it. Six boots is far more headroom than either needs.
	for boot := 1; boot <= 6; boot++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		if f.userAccountID(t, sacrificed) != nil && f.userAccountID(t, ahead) != nil {
			return
		}
	}
	require.NotNil(t, f.userAccountID(t, sacrificed),
		"a repairable row inside an over-cap tie group must be REACHED: the exclusive advance may only "+
			"be taken once the group has actually been exhausted, never as a way to escape it")
	require.NotNil(t, f.userAccountID(t, ahead),
		"draining the tie group must not cost forward progress: the row after it must still be reached")
}

// TestBackfillAccountsCursor_TieGroupBeyondTheCeilingIsSkippedAndAlarmed pins the RESIDUAL the
// drain above deliberately keeps, so it is a written, tested trade rather than a silent one.
//
// The drain cannot be unbounded: this backfill runs SYNCHRONOUSLY at boot, and a tie group of
// arbitrary size would put arbitrary latency in front of the server starting. So it is bounded by
// backfillAccountsTieGroupCeiling, and a group larger than that is escaped exclusively — losing its
// tail, exactly as the pre-fix code did for every over-cap group. The two things that changed are
// that the threshold moved from the per-run cap to the (much larger) ceiling, and that crossing it
// is logged at ERROR with the timestamp, so it is an alarm instead of a silent loss.
//
// Here the ceiling is lowered to the cap, which switches the drain off entirely and reproduces the
// pre-fix behaviour on purpose.
func TestBackfillAccountsCursor_TieGroupBeyondTheCeilingIsSkippedAndAlarmed(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)
	withBackfillTieGroupCeiling(t, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	tie := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	f.seedAccountWithID(t, "44444444-1111-4111-8111-444444444444", f.badItemID, tie)
	f.seedAccountWithID(t, "44444444-2222-4222-8222-444444444444", f.badItemID, tie)
	sacrificed := f.seedAccountWithID(t, "44444444-3333-4333-8333-444444444444", f.goodItemID, tie)
	ahead := f.seedAccount(t, f.goodItemID, tie.Add(10*time.Minute))

	reached := false
	for boot := 1; boot <= 6; boot++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		if f.userAccountID(t, ahead) != nil {
			reached = true
			break
		}
	}
	require.True(t, reached,
		"a tie group that cannot be drained within the ceiling must still be ESCAPED; leaving it in "+
			"place would stall the scan and starve everything behind it, which is worse than the skip")
	require.Nil(t, f.userAccountID(t, sacrificed),
		"the documented price, above the ceiling only: the group's tail is reached on no boot. If this "+
			"ever starts passing, the trade changed and nextBackfillCursorExclusive's comment, "+
			"backfillAccountsTieGroupCeiling's comment and this test must be re-derived together")
}

// TestBackfillAccountsCursor_AllZeroUUIDAccountIsReached covers the second defect. The old scan's
// first page was `a.id > '00000000-…'`, so a row whose id IS the all-zero uuid could never satisfy
// `>` and was invisible on every run. PostgreSQL permits an explicitly inserted all-zero uuid PK
// and AccountRepository.CreateBatch preserves caller-supplied ids.
func TestBackfillAccountsCursor_AllZeroUUIDAccountIsReached(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 100)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	const allZero = "00000000-0000-0000-0000-000000000000"
	f.seedAccountWithID(t, allZero, f.goodItemID, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))

	require.NotNil(t, f.userAccountID(t, allZero),
		"an account whose id is the all-zero uuid must be repaired; a `> all-zero` first page makes it permanently invisible")
}

// TestBackfillAccountsCursor_ConcurrentBootsConverge pins that the cursor — new state SHARED by
// every booting process — does not break the concurrent-boot safety the per-run cursor had. Two
// simultaneous boots may re-do each other's work (every per-row action is idempotent) and may
// overwrite each other's saved position, but neither may lose a repairable row: the worst a lost
// Save can do is send the next boot back to the start.
func TestBackfillAccountsCursor_ConcurrentBootsConverge(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		f.seedAccount(t, f.badItemID, base.Add(time.Duration(i)*time.Minute))
	}
	repairable := f.seedAccount(t, f.goodItemID, base.Add(10*time.Minute))

	const boots = 2
	for round := 0; round < 8; round++ {
		// ⚠ THE BARRIER IS THE TEST (#1569 round 15 task 9). Without it the goroutines were merely
		// STARTED in a loop, and a run of this fixture takes microseconds, so the first could — and
		// on a busy machine routinely did — finish before the second was scheduled. The test then
		// asserted nothing about concurrency at all. `ready` is closed only after every goroutine
		// is parked on it, so all of them leave the starting line together. That guarantees genuine
		// OVERLAP; it cannot guarantee any particular interleaving, and no test can. The interleaving
		// that matters is argued structurally in saveBackfillAccountsCursor's comment and is
		// exercised here under -race across eight rounds.
		var wg, atLine sync.WaitGroup
		ready := make(chan struct{})
		errs := make([]error, boots)
		for i := 0; i < boots; i++ {
			wg.Add(1)
			atLine.Add(1)
			go func(i int) {
				defer wg.Done()
				atLine.Done()
				<-ready
				errs[i] = backfillAccountsUserAccountIDsWithDB(ctx, pool, enc)
			}(i)
		}
		atLine.Wait()
		close(ready)
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		if f.userAccountID(t, repairable) != nil {
			return
		}
	}
	t.Fatal("two concurrent boots never reached the repairable row behind the unrepairable wall")
}

// countingBackfillDB counts the PAGE queries a single run issues, so a test can assert that a run
// visited each row once rather than inferring it from the rows' end state. It is a thin decorator
// over the pool — every statement still reaches real PostgreSQL.
type countingBackfillDB struct {
	backfillQuerier
	pageQueries int
}

func (c *countingBackfillDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM accounts a") {
		c.pageQueries++
	}
	return c.backfillQuerier.Query(ctx, sql, args...)
}

// backfillCursorRowTS returns the stored watermark for this sweep, or nil when no row is stored.
func backfillCursorRowTS(t *testing.T, pool *pgxpool.Pool) *time.Time {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT ts1 FROM sweep_cursors WHERE sweep_name = $1", backfillAccountsSweepCursorName)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	var ts *time.Time
	require.NoError(t, rows.Scan(&ts))
	require.NoError(t, rows.Err())
	return ts
}

// TestBackfillAccountsCursor_ExhaustionOnAPageBoundaryNeitherWrapsNorPingPongs is defect C1.
//
// THE BUG IT FAILS AGAINST, which is the CURRENT committed code. The empty-page branch wrapped on
// `cursor != nil && !wrapped`, and `cursor` is assigned after EVERY non-empty page — so a run that
// started from the BEGINNING and whose final page happened to be exactly backfillAccountsPageSize
// long reached the empty page with a non-nil cursor and wrapped when it should simply have stopped.
//
// The consequence is not merely wasted work. The wrap resets `start` to nil, so the run re-reads the
// very rows it just processed, reaches its per-run CAP on the replay, and therefore takes the
// cappedEarly branch — storing an INCLUSIVE watermark for a scan it had in fact exhausted. Every
// later boot then resumes at that watermark, replays the same full page, wraps again, caps again and
// re-stores it: a permanent ping-pong on a database whose real work is finished.
//
// The discriminator is the one sweepResume.WrapNow already uses (queue/jobs/sweep_resume.go): wrap
// only when the run RESUMED FROM A STORED POSITION and has processed ZERO rows.
//
// Both assertions are deterministic: the fixture is exactly one full page of permanently
// unrepairable rows sharing one created_at, and nothing here depends on scheduling or on ids.
func TestBackfillAccountsCursor_ExhaustionOnAPageBoundaryNeitherWrapsNorPingPongs(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 4) // cap deliberately ABOVE the row count: only a replay can reach it

	f := newBackfillCursorFixture(t, pool, enc)

	tie := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Exactly one FULL page, all permanently unrepairable so they stay in the predicate and a
	// replay is observable. Sharing one created_at makes the replayed page identical, which is what
	// makes the ping-pong permanent rather than self-correcting.
	f.seedAccountWithID(t, "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa", f.badItemID, tie)
	f.seedAccountWithID(t, "aaaaaaaa-2222-4222-8222-aaaaaaaaaaaa", f.badItemID, tie)

	counting := &countingBackfillDB{backfillQuerier: pool}
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, counting, enc))

	require.Equal(t, 2, counting.pageQueries,
		"a run that started from the beginning and exhausted its scan must issue one full page and one "+
			"empty page and then STOP; a third page query means it wrapped and re-read rows it had just "+
			"processed")
	require.Nil(t, backfillCursorRowTS(t, pool),
		"a run that exhausted its scan must leave NO stored watermark; storing one here is the "+
			"ping-pong: every later boot resumes at it, replays the same full page, wraps, caps and "+
			"re-stores it forever")

	// And the second boot must be identical, not a resumption: the state above is genuinely terminal.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	require.Nil(t, backfillCursorRowTS(t, pool),
		"the second boot must also leave no watermark; a watermark appearing here is the ping-pong "+
			"re-establishing itself")
}
