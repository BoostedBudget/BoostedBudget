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
// The properties, one test each. Items 5 and 6 are #1569 round 15 task 9; items 2, 3 and 4 were
// REWRITTEN by it or by its fix round: the wrap test did not exercise the wrap at all (verified by
// mutation — the whole branch could be deleted and it still passed), and the tie-group tests pinned
// first a permanent skip and then a ceiling-bounded one, both of which are gone now that the resume
// position carries a COUNT into the tie group.
//  1. across boots the run RESUMES, so a repairable row behind an over-cap wall of unrepairable
//     rows is reached (the starvation fix);
//  2. the persisted cursor WRAPS on the SAME boot that finds its watermark stale, so a row created
//     BEHIND the watermark is repaired then rather than one boot later;
//  3. an OVER-CAP tie group is WALKED THROUGH — the run resumes INSIDE it, at a stored count — so
//     no reachable row in it is skipped and no boot does extra work to avoid skipping one;
//  4. that count's one imprecision (a consumed prefix that shrinks because it was repaired)
//     CONVERGES rather than losing rows;
//  5. a run that starts at the beginning and exhausts its scan on an exact page boundary neither
//     wraps nor stores a watermark (the ping-pong);
//  6. two boots racing on the shared cursor converge and never lose a repairable row;
//  7. the resume boundary treats the all-zero uuid exactly like every other id.
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
	stored, _ := backfillCursorRow(t, pool)
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

// TestBackfillAccountsCursor_OverCapTieGroupIsWalkedThroughWithoutSkippingIt is defect C2, and its
// fix-1 replacement.
//
// THE DEFECT. The persisted position is a TIMESTAMP only (never a row id — see
// backfill_accounts_cursor.go on why that is a GDPR invariant), so a run that spent its whole cap
// inside ONE created_at had no way to say "resume in the middle of this group". The committed code
// advanced EXCLUSIVELY past the whole group, and its rows past the first cap-worth were reached on
// no boot, ever. An interim fix DRAINED the group in-run instead, which removed the skip only below
// a ceiling (5000 rows) and paid for it with up to five caps of synchronous work at boot.
//
// THE FIX ASSERTED HERE: the position carries a COUNT of rows already consumed at that timestamp
// (sweep_cursors.ts1_offset), so the run stops exactly at its cap and the next boot resumes exactly
// where it stopped. No ceiling, no skip, no extra boot work — and still no row identity anywhere.
//
// RED against BOTH previous shapes: the exclusive advance never repairs `sacrificed`, and an
// offset-less inclusive resume replays the same first page forever so it never reaches it either.
func TestBackfillAccountsCursor_OverCapTieGroupIsWalkedThroughWithoutSkippingIt(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	tie := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// ids are explicit so the tie group's order under `ORDER BY a.created_at, a.id` is pinned. FOUR
	// unrepairable rows, i.e. TWO caps' worth, so the group cannot be crossed in one resume — that
	// is what makes the stored count have to ACCUMULATE across runs rather than merely exist. They
	// stay in the candidate set (which is what makes a group sticky), and the repairable row sorts
	// last inside the group.
	for _, id := range []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
	} {
		f.seedAccountWithID(t, id, f.badItemID, tie)
	}
	sacrificed := f.seedAccountWithID(t, "55555555-5555-4555-8555-555555555555", f.goodItemID, tie)
	ahead := f.seedAccount(t, f.goodItemID, tie.Add(10*time.Minute))

	// Boot 1 stops at its cap two rows into the group, and says so durably.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	ts, off := backfillCursorRow(t, pool)
	require.NotNil(t, ts, "a run capped inside a tie group must persist where it stopped")
	require.True(t, ts.Equal(tie), "the persisted timestamp is the group's")
	require.Equal(t, 2, off, "the persisted position must say HOW FAR into the group the run got; "+
		"without the count the next boot can only replay the group or step over it")

	// Boot 2 resumes INSIDE the group and must ADD to that count, not restart it.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	ts, off = backfillCursorRow(t, pool)
	require.NotNil(t, ts, "boot 2 is still inside the group, so it too stops at its cap")
	require.True(t, ts.Equal(tie), "still the same group")
	require.Equal(t, 4, off, "the count must ACCUMULATE across runs: a run that resumes at 2 and "+
		"consumes 2 more is 4 rows into the group. Saving its own 2 pegs the cursor there forever "+
		"and nothing past it is ever reached")

	// Boot 3 crosses the tail of the group and the row after it.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	require.NotNil(t, f.userAccountID(t, sacrificed),
		"a repairable row inside an over-cap tie group must be reached by resuming into the group; "+
			"stepping past the group loses it permanently and replaying the group never gets to it")
	require.NotNil(t, f.userAccountID(t, ahead),
		"walking through the group must not cost forward progress: the row after it is reached too")
}

// TestBackfillAccountsCursor_EveryRowOfAnOverCapRepairableTieGroupIsReached pins the one imprecision
// the offset has, by pinning that it CONVERGES rather than losing rows.
//
// The offset counts rows CONSUMED. When the consumed prefix is repairable — i.e. when the backfill
// is doing its job — those rows leave the candidate set, so on the next boot the offset points
// further into the group than it should and some rows are passed over. They are NOT lost: a run that
// exhausts its scan CLEARS the cursor, so a later boot starts from the beginning of the group with a
// smaller set, and repairs only ever shrink it.
//
// Five rows, a cap of two, all repairable — the worst case for the overshoot, since every consumed
// row leaves. Every one of them must end up repaired.
func TestBackfillAccountsCursor_EveryRowOfAnOverCapRepairableTieGroupIsReached(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	tie := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ids := []string{
		"aaaaaaaa-0000-4000-8000-000000000001",
		"aaaaaaaa-0000-4000-8000-000000000002",
		"aaaaaaaa-0000-4000-8000-000000000003",
		"aaaaaaaa-0000-4000-8000-000000000004",
		"aaaaaaaa-0000-4000-8000-000000000005",
	}
	for _, id := range ids {
		f.seedAccountWithID(t, id, f.goodItemID, tie)
	}

	// Generous but finite: five rows at two per boot needs three boots with an exact cursor, and a
	// few more when the overshoot forces a wrap. If this ever needs raising, the offset stopped
	// converging and that is the finding, not the number.
	for boot := 1; boot <= 12; boot++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		done := true
		for _, id := range ids {
			if f.userAccountID(t, id) == nil {
				done = false
			}
		}
		if done {
			return
		}
	}
	for _, id := range ids {
		require.NotNil(t, f.userAccountID(t, id),
			"every row of an over-cap tie group must be reached within a bounded number of boots; "+
				"an offset that overshoots must be recovered by the clear-and-restart, not left as a hole")
	}
}

// TestBackfillAccountsCursor_ResumeDoesNotSingleOutTheAllZeroUUID is defect D3.
//
// THE DEFECT. The resume position used to be the stored timestamp paired with a synthesized uuid
// sentinel, and the "inclusive" sentinel was the ALL-ZERO uuid compared with `>`. So at the resume
// boundary an account whose id IS the all-zero uuid was excluded while every other id at the same
// timestamp was included. That id is legal (PostgreSQL permits it as an explicitly inserted PK and
// AccountRepository.CreateBatch preserves a caller-supplied one), and round 14 had already fixed the
// same asymmetry on the COLD path — the sentinel reintroduced it on the RESUMED one.
//
// ⚠ WHAT THIS TEST ASSERTS IS EQUALITY, NOT PROMPTNESS, and the distinction is the honest one. A row
// that appears BEHIND an already-stored resume position waits for the scan to be exhausted and the
// cursor cleared, whatever its id — that is true of a row at an earlier timestamp too, and buying
// promptness back would mean re-reading the whole group on every boot, which is the starvation this
// cursor exists to remove. What must NOT happen is that ONE id waits and the others do not. So the
// two cases below are identical fixtures differing only in the inserted row's id, and the assertion
// is that they behave the same.
//
// RED against the committed code: the 0001 row is picked up on the resumed boot and the all-zero row
// is not, so the two cases differ.
func TestBackfillAccountsCursor_ResumeDoesNotSingleOutTheAllZeroUUID(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	// bootsToRepair runs the identical scenario for one id and returns the boot on which that row
	// was repaired. It runs as a SUBTEST so its fixture rows are cleaned up before the next case
	// starts: this scan is global, and one case's leftover unrepairable rows sort ahead of the
	// other's and would silently consume its first boot's cap.
	bootsToRepair := func(t *testing.T, caseTag, arrivingID string, base time.Time) int {
		t.Helper()
		withBackfillPageBounds(t, 2, 2)
		f := newBackfillCursorFixture(t, pool, enc)
		f.cleanupSynthesised(t)

		early, tie := base, base.Add(time.Minute)
		// Two permanently unrepairable rows, one at each timestamp: boot 1 consumes both, stops at
		// its cap and stores a resume position at `tie` with one row of it consumed. The ids carry
		// the case tag because both cases run against the same database.
		f.seedAccountWithID(t, "cccccccc-1111-4111-8111-cccccccccc"+caseTag, f.badItemID, early)
		f.seedAccountWithID(t, "cccccccc-2222-4222-8222-cccccccccc"+caseTag, f.badItemID, tie)
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		ts, _ := backfillCursorRow(t, pool)
		require.NotNil(t, ts, "boot 1 must stop at its cap and store a resume position")
		require.True(t, ts.Equal(tie), "the stored position must be at the tie timestamp")

		// The row under test arrives AT that timestamp, sorting ahead of the row already consumed.
		arriving := f.seedAccountWithID(t, arrivingID, f.goodItemID, tie)

		for boot := 2; boot <= 8; boot++ {
			require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
			if f.userAccountID(t, arriving) != nil {
				return boot
			}
		}
		t.Fatalf("the row %s was never repaired", arrivingID)
		return 0
	}

	const allZero = "00000000-0000-0000-0000-000000000000"
	const nonZero = "00000000-0000-0000-0000-000000000001"
	var zeroBoot, nonZeroBoot int
	t.Run("all-zero uuid", func(t *testing.T) {
		zeroBoot = bootsToRepair(t, "01", allZero, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	})
	t.Run("ordinary uuid", func(t *testing.T) {
		nonZeroBoot = bootsToRepair(t, "02", nonZero, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	})
	require.NotZero(t, zeroBoot, "the all-zero case must have produced a result")
	require.NotZero(t, nonZeroBoot, "the ordinary-id case must have produced a result")

	require.Equal(t, nonZeroBoot, zeroBoot,
		"an account whose id is the all-zero uuid must be treated at the resume boundary exactly like "+
			"any other id at the same timestamp. A synthesized all-zero 'inclusive' sentinel compared "+
			"with `>` excludes precisely that one id, which is the cold-path defect round 14 fixed, "+
			"reintroduced on the resumed path")
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

// backfillCursorRow returns the stored resume position — the watermark and how many rows of it the
// last run consumed — or (nil, 0) when no row is stored.
func requireNoStoredCursor(t *testing.T, pool *pgxpool.Pool, msg string) {
	t.Helper()
	ts, offset := backfillCursorRow(t, pool)
	require.Nil(t, ts, msg)
	require.Zero(t, offset, msg)
}

func backfillCursorRow(t *testing.T, pool *pgxpool.Pool) (*time.Time, int) {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT ts1, ts1_offset FROM sweep_cursors WHERE sweep_name = $1", backfillAccountsSweepCursorName)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		return nil, 0
	}
	var (
		ts     *time.Time
		offset *int32
	)
	require.NoError(t, rows.Scan(&ts, &offset))
	require.NoError(t, rows.Err())
	if offset == nil {
		return ts, 0
	}
	return ts, int(*offset)
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
	requireNoStoredCursor(t, pool,
		"a run that exhausted its scan must leave NO stored watermark; storing one here is the "+
			"ping-pong: every later boot resumes at it, replays the same full page, wraps, caps and "+
			"re-stores it forever")

	// And the second boot must be identical, not a resumption: the state above is genuinely terminal.
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
	requireNoStoredCursor(t, pool,
		"the second boot must also leave no watermark; a watermark appearing here is the ping-pong "+
			"re-establishing itself")
}
