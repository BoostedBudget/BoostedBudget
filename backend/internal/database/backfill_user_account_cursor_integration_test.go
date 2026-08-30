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
// The three properties, one test each:
//  1. across boots the run RESUMES, so a repairable row behind an over-cap wall of unrepairable
//     rows is reached (the starvation fix);
//  2. the persisted cursor WRAPS when the scan is exhausted, so a row created BEHIND the watermark
//     is still repaired — otherwise the fix trades head-of-line starvation for tail starvation;
//  3. an OVER-CAP tie group on one created_at advances rather than replaying forever, and the tail
//     it sacrifices to do so is pinned as the documented price rather than left implicit;
//  4. two boots racing on the shared cursor converge and never lose a repairable row.
//
// A fifth test covers the separate all-zero-uuid defect: see
// TestBackfillAccountsCursor_AllZeroUUIDAccountIsReached.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

// TestBackfillAccountsCursor_WrapsSoARowBehindTheWatermarkIsRepaired pins the OTHER starvation —
// the one the fix would introduce if the persisted cursor only ever moved forward. A row created
// with a created_at BEHIND the stored watermark sorts before it and would never be selected again.
func TestBackfillAccountsCursor_WrapsSoARowBehindTheWatermarkIsRepaired(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	f := newBackfillCursorFixture(t, pool, enc)
	f.cleanupSynthesised(t)

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		f.seedAccount(t, f.badItemID, base.Add(time.Duration(i)*time.Minute))
	}
	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc)) // caps, saves a watermark

	// A row that becomes eligible BEHIND the watermark.
	behind := f.seedAccount(t, f.goodItemID, base.Add(-time.Hour))

	// Boot 2 finishes the wall and stores nothing (scan exhausted); boot 3 starts from the
	// beginning and finds the row behind the watermark.
	for i := 0; i < 3; i++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		if f.userAccountID(t, behind) != nil {
			return
		}
	}
	t.Fatal("a row sorting behind the persisted watermark was never repaired: the cursor must wrap " +
		"back to the start when the scan is exhausted, or the fix trades head starvation for tail starvation")
}

// TestBackfillAccountsCursor_OverCapTieGroupAdvancesAndSacrificesItsTail constructs the one
// degenerate case an inclusive resume cannot survive, and pins BOTH halves of the trade made for it.
//
// The case: more than a cap's worth of rows share ONE created_at, and the first cap-worth are
// permanently unrepairable. An inclusive resume re-reads that same first cap-worth on every boot
// forever — the identical starvation the persisted cursor exists to remove, reintroduced at a single
// timestamp. nextBackfillCursorExclusive detects "resumed at a key and made no progress along the
// created_at axis" and saves the position EXCLUSIVE, which steps past the whole group.
//
// The price, asserted rather than merely described: the group's rows past the first cap-worth are
// reached on NO boot, including after a wrap (a wrap restarts at the beginning and re-reads the same
// first cap-worth). The tail row here is fully REPAIRABLE and is skipped anyway, because the
// exclusive advance is taken without ever looking at it. This is #1477's residual, accepted for its
// reason: without the advance the group stalls the scan completely and nothing behind it is reached
// either. In production it needs >1000 accounts sharing one created_at whose first 1000 are all
// unrepairable — one Plaid Item's CreateBatch, so single digits. Not reachable.
func TestBackfillAccountsCursor_OverCapTieGroupAdvancesAndSacrificesItsTail(t *testing.T) {
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
	behind := f.seedAccount(t, f.goodItemID, tie.Add(10*time.Minute))

	reached := false
	for boot := 1; boot <= 6; boot++ {
		require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, pool, enc))
		if f.userAccountID(t, behind) != nil {
			reached = true
			break
		}
	}
	require.True(t, reached,
		"a run whose whole cap was consumed by ONE created_at must save an EXCLUSIVE position; "+
			"an inclusive save replays the identical page on every boot forever")

	require.Nil(t, f.userAccountID(t, sacrificed),
		"the documented price of the exclusive advance: the over-cap tie group's tail is reached on no "+
			"boot, even though it is repairable. If this ever starts passing, the trade changed and both "+
			"nextBackfillCursorExclusive's comment and this test must be re-derived together")
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

	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = backfillAccountsUserAccountIDsWithDB(ctx, pool, enc)
			}(i)
		}
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		if f.userAccountID(t, repairable) != nil {
			return
		}
	}
	t.Fatal("two concurrent boots never reached the repairable row behind the unrepairable wall")
}
