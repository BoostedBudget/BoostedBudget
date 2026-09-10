//go:build integration

package repository

// Integration tests for the two repository methods the periodic balance-snapshot gap-filler needs
// (#1626 Phase 3, Defect 3 / Task G):
//
//   - CreateDailyCapture         — the once-per-UTC-day, conflict-tolerant write.
//   - AccountsMissingDailySnapshot — the COHORT question the job's "skip with zero Plaid calls"
//     decision rests on.
//
// ⚠ WHY THESE ARE INTEGRATION TESTS AND NOT pgxmock TESTS, stated because the cheaper option is
// available and would have been WRONG in two separate ways.
//
//  1. pgxmock scans a nil cell into a plain Go `string` WITHOUT error. A pgxmock test asserting
//     "this method tolerates a NULL current_balance" therefore passes against code that has no NULL
//     tolerance whatsoever — it is testing the mock, not the scan. This exact failure mode was found
//     systemically on this branch. TestDailyCapture_Integration_NullCurrentBalanceSurvivesTheRoundTrip
//     below is the test that would have lied.
//  2. CreateDailyCapture's whole behaviour IS the partial unique index (migration 000194) and
//     Postgres's ON CONFLICT arbiter inference against it. A mock cannot have an index, so a mock
//     cannot distinguish "idempotent" from "inserts twice", and it cannot tell us that the index's
//     predicate scopes the constraint to daily_capture rows only — which is the property that keeps
//     migration 000017's deliberate REMOVAL of per-day uniqueness intact for webhook history.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable \
//	CACHE_REDIS_TEST_URL=redis://localhost:6380 REDIS_TEST_URL=redis://localhost:6379 \
//	  go test ./internal/repository/... -run TestDailyCapture -tags integration -p 1 -v

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// dailyCaptureRow is one daily_capture snapshot for the given account and instant.
func dailyCaptureRow(f carryFixture, accountID uuid.UUID, at time.Time, current, available *float64) *domain.BalanceSnapshot {
	return &domain.BalanceSnapshot{
		UserID:           f.userID.String(),
		AccountID:        accountID.String(),
		CurrentBalance:   current,
		AvailableBalance: available,
		CurrencyCode:     "USD",
		SnapshotAt:       at,
		Source:           domain.SnapshotSourceDailyCapture,
	}
}

// countSnapshots counts rows for one account on one UTC calendar day, optionally by source.
func countSnapshots(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID, day time.Time, source string) int {
	t.Helper()
	var n int
	err := db.QueryRow(ctx, `
		SELECT count(*) FROM balance_snapshots
		WHERE account_id = $1
		  AND (snapshot_at AT TIME ZONE 'UTC')::date = $2::date
		  AND ($3 = '' OR source = $3)`, accountID, day, source).Scan(&n)
	require.NoError(t, err)
	return n
}

// ── CreateDailyCapture ────────────────────────────────────────────────────────────────────────

// TestDailyCapture_Integration_SecondWriteForTheSameDayIsAReportedNoOp pins both halves of the
// contract: the row is not duplicated, AND the caller is told it was not inserted.
//
// The boolean matters. Without it the job cannot distinguish "I filled the gap" from "somebody else
// already had", and a `Create` that silently swallowed the conflict would be indistinguishable from
// one that wrote — which is how the pre-existing writer's warn-and-skip loop managed to report
// success while committing a partial cohort.
func TestDailyCapture_Integration_SecondWriteForTheSameDayIsAReportedNoOp(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-dup")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, tx, f, "dc-dup-dep", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	inserted, err := repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.Add(21*time.Hour), amt(100), amt(100)))
	require.NoError(t, err)
	assert.True(t, inserted, "the first daily capture of the day must actually insert")

	// A DIFFERENT instant on the SAME UTC day, and a DIFFERENT balance — the index keys on the day,
	// not the instant, so this must still be refused. A fixture reusing the same instant would pass
	// against a (account_id, snapshot_at) key too and would prove nothing about the day.
	inserted, err = repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.Add(22*time.Hour), amt(999), amt(999)))
	require.NoError(t, err, "a same-day conflict must be a no-op, not an error")
	assert.False(t, inserted, "the second daily capture of the same UTC day must report that it did not insert")

	assert.Equal(t, 1, countSnapshots(ctx, t, tx, acct, today, domain.SnapshotSourceDailyCapture))
}

// TestDailyCapture_Integration_TheNextUTCDayIsANewRow is the falsifying twin of the test above: the
// constraint must be per DAY, not per account. Without this, an index on account_id alone — or a
// CreateDailyCapture that simply never inserted twice for an account — would keep the duplicate
// test green while making the whole job a one-shot.
func TestDailyCapture_Integration_TheNextUTCDayIsANewRow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-nextday")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, tx, f, "dc-nextday-dep", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// 23:00 on day one and 01:00 on day two — deliberately only two hours apart, so the ONLY thing
	// separating them is the UTC date boundary. A fixture placed at noon on each day would also pass
	// against code comparing something coarser than the day, and would tell us less.
	inserted, err := repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.Add(23*time.Hour), amt(100), amt(100)))
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.AddDate(0, 0, 1).Add(1*time.Hour), amt(101), amt(101)))
	require.NoError(t, err)
	assert.True(t, inserted, "a capture on the NEXT UTC day is a new observation and must insert")

	assert.Equal(t, 1, countSnapshots(ctx, t, tx, acct, today, domain.SnapshotSourceDailyCapture))
	assert.Equal(t, 1, countSnapshots(ctx, t, tx, acct, today.AddDate(0, 0, 1), domain.SnapshotSourceDailyCapture))
}

// TestDailyCapture_Integration_DoesNotConstrainWebhookHistoryOnTheSameDay is why the index is
// PARTIAL.
//
// Migration 000017 dropped UNIQUE(account_id, snapshot_date) deliberately, because a webhook sync
// may legitimately record several rows for one account in one day and per-day uniqueness would
// discard real balance changes. A generic per-day key would have re-broken that. This test fails
// against an index whose WHERE clause was dropped.
func TestDailyCapture_Integration_DoesNotConstrainWebhookHistoryOnTheSameDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-partial")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, tx, f, "dc-partial-dep", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// Two webhook rows on the same day, through the UNCHANGED writer.
	snap(ctx, t, repo, f, acct, today.Add(9*time.Hour), amt(100), amt(100))
	snap(ctx, t, repo, f, acct, today.Add(15*time.Hour), amt(140), amt(140))
	assert.Equal(t, 2, countSnapshots(ctx, t, tx, acct, today, domain.SnapshotSourcePlaidWebhook),
		"webhook history must still allow several rows per account per day")

	// And a daily_capture row on the same day is not blocked by them either — the constraint is
	// scoped to daily_capture, so a webhook row is not an arbiter for it.
	inserted, err := repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.Add(21*time.Hour), amt(150), amt(150)))
	require.NoError(t, err)
	assert.True(t, inserted, "a webhook row on the same day must not suppress the gap-filler's own row")
	assert.Equal(t, 3, countSnapshots(ctx, t, tx, acct, today, ""))
}

// TestDailyCapture_Integration_NullCurrentBalanceSurvivesTheRoundTrip is T6, and the reason it is
// here rather than in the pgxmock file is stated at the top of this file: a mock would scan a nil
// cell into a string without complaint and prove nothing.
//
// The DECISION being pinned is Task E's, and it is a decision not to be clever: an account whose
// current_balance is NULL and whose available_balance is present is written AS IS. available_balance
// is NOT coalesced into current_balance, because on a credit card it is the remaining credit LINE —
// substituting it would report a large positive asset where a debt belongs. The aggregated read
// excludes such a row from the VALUE cohort and lets the per-account carry-forward supply the last
// known real balance, which is why the day still renders instead of collapsing.
func TestDailyCapture_Integration_NullCurrentBalanceSurvivesTheRoundTrip(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-null")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	acct := seedCarryAccount(ctx, t, tx, f, "dc-null-credit", "credit", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// Yesterday: a real current balance. Today: current unknown, available present — the shape a
	// credit card returns when Plaid has a credit line but no posted balance.
	snap(ctx, t, repo, f, acct, yesterday.Add(12*time.Hour), amt(-500), amt(4500))
	inserted, err := repo.CreateDailyCapture(ctx, dailyCaptureRow(f, acct, today.Add(21*time.Hour), nil, amt(4500)))
	require.NoError(t, err, "a NULL current_balance with an available_balance must be storable")
	require.True(t, inserted)

	// (a) The NULL is preserved, not coerced to zero and not backfilled from available_balance.
	var current, available *float64
	err = tx.QueryRow(ctx, `
		SELECT current_balance, available_balance FROM balance_snapshots
		WHERE account_id = $1 AND source = $2`, acct, domain.SnapshotSourceDailyCapture).
		Scan(&current, &available)
	require.NoError(t, err, "scanning a NULL current_balance back out must not error")
	assert.Nil(t, current, "current_balance must stay NULL — coalescing available_balance into it "+
		"would report a credit card's remaining credit line as an asset")
	require.NotNil(t, available)
	assert.InDelta(t, 4500.0, *available, 0.01)

	// (b) The day still renders, carrying yesterday's real balance rather than dropping the account
	// or returning a zero. This is the behavioural half — without it, "the NULL is stored" would be
	// satisfied by a writer whose rows make the chart 500.
	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), yesterday, today, nil)
	require.NoError(t, err, "a day whose only row has a NULL current_balance must not fail the read")
	p := pointOn(history, today)
	require.NotNil(t, p, "today must still appear")
	assert.InDelta(t, -500.0, p.TotalBalance, 0.01,
		"the NULL-current row is excluded from the value cohort and yesterday's real balance is carried")
}

// TestDailyCapture_Integration_ConcurrentCapturesYieldOneRowPerAccountPerDay is T9.
//
// ⚠ IT MUST BE CONCURRENT. The cohort skip and the insert are separate statements and the capture
// job takes no per-item lock, so the duplicate this index exists for is produced by two capture
// jobs racing — a retry racing its timed-out predecessor, or a deploy's RunOnStart run racing the
// scheduled one. Sequential re-runs prove nothing about that: they are already covered above, and
// the earlier draft of this test ("run it twice") could not have failed for the right reason.
//
// This test uses the POOL, not a transaction: concurrent writers on one transaction are serialised
// by pgx and there would be no race to observe.
func TestDailyCapture_Integration_ConcurrentCapturesYieldOneRowPerAccountPerDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	f := seedCarryFixture(ctx, t, pool, "dc-race")
	t.Cleanup(func() {
		// Fixture teardown, not history destruction: these rows were created by this test seconds
		// ago and belong to nobody. The users cascade removes the accounts and snapshots with it.
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.userID)
	})
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, pool, f, "dc-race-dep", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(pool)

	const racers = 8
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		insertedNum int
		errs        []error
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// Distinct instants inside the same UTC day, so each racer carries a different
			// snapshot_at and the ONLY thing that can deduplicate them is the day key.
			ok, err := repo.CreateDailyCapture(ctx,
				dailyCaptureRow(f, acct, today.Add(time.Duration(i)*time.Minute).Add(20*time.Hour), amt(float64(1000+i)), nil))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if ok {
				insertedNum++
			}
		}(i)
	}
	close(start)
	wg.Wait()

	require.Empty(t, errs, "a lost race must be a reported no-op, never an error the job retries on")
	assert.Equal(t, 1, insertedNum, "exactly one concurrent capture may report an insert")
	assert.Equal(t, 1, countSnapshots(ctx, t, pool, acct, today, domain.SnapshotSourceDailyCapture),
		"concurrent capture of one item must leave one row per account per UTC day")
}

// ── AccountsMissingDailySnapshot ──────────────────────────────────────────────────────────────

// TestDailyCapture_Integration_CohortCompleteWhenEveryActiveAccountHasTodaysRow is the T3
// precondition at the repository level: the job's "skip with ZERO Plaid calls" decision is only as
// good as this answer.
func TestDailyCapture_Integration_CohortCompleteWhenEveryActiveAccountHasTodaysRow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-cohort-full")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	a := seedCarryAccount(ctx, t, tx, f, "dc-cohort-full-a", "depository", today.AddDate(0, 0, -30))
	b := seedCarryAccount(ctx, t, tx, f, "dc-cohort-full-b", "credit", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// A WEBHOOK row counts. The cohort question is "does this account have a row for today", not
	// "did the gap-filler write one" — the whole point is to make no Plaid call when a webhook has
	// already covered the day.
	snap(ctx, t, repo, f, a, today.Add(3*time.Hour), amt(10), nil)
	snap(ctx, t, repo, f, b, today.Add(4*time.Hour), amt(20), nil)

	missing, err := repo.AccountsMissingDailySnapshot(ctx, f.linkedItemID.String(), today)
	require.NoError(t, err)
	assert.Empty(t, missing, "a full cohort must report nothing missing")
}

// TestDailyCapture_Integration_PartialCohortIsNotDone is T4, and it is the incomplete-cohort defect
// itself: "any row today" would wrongly report this item as covered and the missing accounts would
// never get a row for the day.
func TestDailyCapture_Integration_PartialCohortIsNotDone(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-cohort-part")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var accounts []uuid.UUID
	for i, kind := range []string{"depository", "depository", "credit", "loan", "investment"} {
		accounts = append(accounts, seedCarryAccount(ctx, t, tx, f,
			"dc-cohort-part-"+string(rune('a'+i)), kind, today.AddDate(0, 0, -30)))
	}
	repo := NewBalanceSnapshotRepository(tx)

	// 3 of 5 — the exact shape a webhook sync leaves when it snapshots part of an item's roster.
	for _, acct := range accounts[:3] {
		snap(ctx, t, repo, f, acct, today.Add(6*time.Hour), amt(1), nil)
	}

	missing, err := repo.AccountsMissingDailySnapshot(ctx, f.linkedItemID.String(), today)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{accounts[3].String(), accounts[4].String()}, missing,
		"a 3-of-5 day must report the two uncovered accounts, so the item is still processed")
}

// TestDailyCapture_Integration_YesterdaysRowDoesNotCoverToday keeps the cohort question anchored to
// the day it was asked about. Without it, an implementation that ignored the day argument entirely
// would satisfy the "full cohort" test above and would make the job a one-shot forever.
func TestDailyCapture_Integration_YesterdaysRowDoesNotCoverToday(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-cohort-yday")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	acct := seedCarryAccount(ctx, t, tx, f, "dc-cohort-yday-dep", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// 23:00 yesterday — one hour before the boundary, so only a DAY comparison separates it from
	// today. This is the UTC-boundary assertion for the cohort read.
	snap(ctx, t, repo, f, acct, today.Add(-1*time.Hour), amt(10), nil)

	missing, err := repo.AccountsMissingDailySnapshot(ctx, f.linkedItemID.String(), today)
	require.NoError(t, err)
	assert.Equal(t, []string{acct.String()}, missing, "a row at 23:00 yesterday does not cover today")

	missing, err = repo.AccountsMissingDailySnapshot(ctx, f.linkedItemID.String(), today.AddDate(0, 0, -1))
	require.NoError(t, err)
	assert.Empty(t, missing, "and it DOES cover yesterday")
}

// TestDailyCapture_Integration_RemovedAccountIsNotPartOfTheCohort is the repository half of T5.
//
// A removed account must accrue NO balance snapshots — that is what stops a user-disconnected
// account quietly resuming data collection because the bank still reports it. If such an account
// were counted as part of the cohort, the item could NEVER report a complete cohort and would be
// processed every single day forever, making the "zero Plaid calls" property unreachable.
func TestDailyCapture_Integration_RemovedAccountIsNotPartOfTheCohort(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-cohort-removed")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	live := seedCarryAccount(ctx, t, tx, f, "dc-cohort-removed-live", "depository", today.AddDate(0, 0, -30))
	gone := seedCarryAccount(ctx, t, tx, f, "dc-cohort-removed-gone", "depository", today.AddDate(0, 0, -30))
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'removed', removed_at = now(), removed_reason = 'user_disconnect' WHERE id = $1`, gone)
	require.NoError(t, err)

	repo := NewBalanceSnapshotRepository(tx)
	snap(ctx, t, repo, f, live, today.Add(6*time.Hour), amt(10), nil)

	missing, err := repo.AccountsMissingDailySnapshot(ctx, f.linkedItemID.String(), today)
	require.NoError(t, err)
	assert.Empty(t, missing,
		"a removed account is not part of the cohort — counting it would make the item eligible every "+
			"day forever and would invite a snapshot for an account the user disconnected")
}

// ── LinkedItemRepository.GetItemsForBalanceCapturePage ────────────────────────────────────────────

// seedCaptureItem inserts one linked item for f's user with the given status and returns its ID.
//
// replaced_at is stamped for status 'replaced' because linked_items_replaced_at_consistency requires
// it — a 'replaced' row without a replaced_at is not a state this schema permits, so the fixture has
// to produce the real one rather than a shape the check constraint rejects.
func seedCaptureItem(ctx context.Context, t *testing.T, db DB, f carryFixture, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var replacedAt *time.Time
	if status == string(domain.LinkedItemStatusReplaced) {
		now := time.Now().UTC()
		replacedAt = &now
	}
	_, err := db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status, replaced_at)
		VALUES ($1, $2, $3, 'enc-test-token', $4, $5)`,
		id, f.userID, "capture-item-"+id.String(), status, replacedAt)
	require.NoError(t, err, "seed linked_item with status %s", status)
	return id
}

// TestDailyCapture_Integration_EnumerationSkipsNeedsReauthAndTerminalItems is T11 at the repository
// level, and it is the whole eligibility rule in one assertion.
//
// An item in ITEM_LOGIN_REQUIRED would fail its Plaid call every single day forever — a permanent
// error retried on a daily schedule is a permanent error retried on a daily schedule. The existing
// item-error state is what says so (linked_items.status, written by ItemErrorService); this
// deliberately invents no second notion of "broken".
//
// ⚠ EVERY non-active status is enumerated here on purpose. A test that only excluded
// 'login_required' would pass against a predicate like `status <> 'login_required'`, which would
// then happily call Plaid daily for a REVOKED item until its token was removed.
func TestDailyCapture_Integration_EnumerationSkipsNeedsReauthAndTerminalItems(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-enum")
	// seedCarryFixture's own item defaults to status 'active' (the column is NOT NULL DEFAULT
	// 'active'), so it is an eligible item too and is expected in the result.
	activeSecond := seedCaptureItem(ctx, t, tx, f, string(domain.LinkedItemStatusActive))
	for _, ineligible := range []domain.LinkedItemStatus{
		domain.LinkedItemStatusError,
		domain.LinkedItemStatusLoginRequired,
		domain.LinkedItemStatusPendingExpiration,
		domain.LinkedItemStatusRevoked,
		domain.LinkedItemStatusReplaced,
		domain.LinkedItemStatusDisconnected,
	} {
		seedCaptureItem(ctx, t, tx, f, string(ineligible))
	}

	repo := NewLinkedItemRepository(tx, newTestFieldEncryptor(t))
	targets, err := repo.GetItemsForBalanceCapturePage(ctx, nil, 1000)
	require.NoError(t, err)

	var got []string
	for _, tgt := range targets {
		if tgt.UserID == f.userID.String() {
			got = append(got, tgt.LinkedItemID)
		}
	}
	assert.ElementsMatch(t, []string{f.linkedItemID.String(), activeSecond.String()}, got,
		"only active items are eligible for a daily balance capture")
}

// TestDailyCapture_Integration_EnumerationRespectsItsLimit keeps the dispatcher's per-run bound
// real. Without it, a limit argument that the SQL ignored would leave one run enqueueing a capture
// job for every item in the database at once — the exact unstaggered burst the dispatcher split
// exists to avoid.
func TestDailyCapture_Integration_EnumerationRespectsItsLimit(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-enum-limit")
	for i := 0; i < 4; i++ {
		seedCaptureItem(ctx, t, tx, f, string(domain.LinkedItemStatusActive))
	}

	repo := NewLinkedItemRepository(tx, newTestFieldEncryptor(t))
	targets, err := repo.GetItemsForBalanceCapturePage(ctx, nil, 3)
	require.NoError(t, err)
	assert.Len(t, targets, 3, "the limit must be applied in SQL, not by the caller")
}

// ── T13: daily continuity, and what a MISSED day actually looks like ──────────────────────────

// TestDailyCapture_Integration_ARowPerRunAndAMissedDayRendersAsCarryForward is T13, restated TWICE —
// once because the original specification was impossible, and once because writing it revealed the
// carry-forward's real scope, which is narrower than the brief's phrasing implied.
//
// ⚠ THE ORIGINAL SPEC ASKED FOR SOMETHING THIS DESIGN CANNOT DELIVER: "run on day 0 and day 2,
// assert a row on day 1". Nothing here produces a row for a day it did not run — there is no
// backfill, deliberately, and inventing one would mean writing an observation nobody made.
//
// ⚠ AND "A MISSED DAY RENDERS VIA THE LOCF" IS TRUE ONLY WHERE THE DAY EXISTS IN THE SERIES AT ALL.
// GetAggregatedHistory builds its grid as observed_days × observed_keys, so the carry-forward fills
// an ACCOUNT that has no row on a day SOME account reported — it does not manufacture a day on which
// nothing whatsoever was captured. That day is ABSENT from the response, which is a materially
// different thing from a zero and is the correct behaviour (a gap is recoverable information; a
// fabricated point is not), but it is not what a reader of the brief's one-line phrasing would
// expect. Both halves are asserted below so neither can drift:
//
//	(a) one row per RUN, on the UTC day that run happened, and no row for a day with no run;
//	(b) an item that MISSED a day still contributes its last known balance to that day's total,
//	    provided the day exists — which is exactly the user-reported symptom (a day on which one
//	    item synced collapsed the total toward zero); and
//	(c) a day on which NOTHING was captured is absent from the series entirely — NOT a zero.
func TestDailyCapture_Integration_ARowPerRunAndAMissedDayRendersAsCarryForward(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedCarryFixture(ctx, t, tx, "dc-continuity")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day0 := today.AddDate(0, 0, -4)
	day1 := today.AddDate(0, 0, -3) // item A missed this day; item B did not
	day2 := today.AddDate(0, 0, -2)
	day3 := today.AddDate(0, 0, -1) // nothing at all was captured on this day

	// TWO accounts, so day1 is a day item A missed rather than a day nothing happened. A
	// single-account fixture cannot express the incomplete-cohort case at all, which is why the
	// earlier draft of this test could not have been about the reported symptom.
	acctA := seedCarryAccount(ctx, t, tx, f, "dc-continuity-a", "depository", today.AddDate(0, 0, -30))
	acctB := seedCarryAccount(ctx, t, tx, f, "dc-continuity-b", "depository", today.AddDate(0, 0, -30))
	repo := NewBalanceSnapshotRepository(tx)

	// Item A: runs on day0 and day2. Item B: runs on day0, day1 and day2.
	for _, r := range []struct {
		acct    uuid.UUID
		day     time.Time
		balance float64
	}{
		{acctA, day0, 1000}, {acctB, day0, 50},
		{acctB, day1, 60},
		{acctA, day2, 1200}, {acctB, day2, 70},
	} {
		inserted, err := repo.CreateDailyCapture(ctx,
			dailyCaptureRow(f, r.acct, r.day.Add(21*time.Hour), amt(r.balance), nil))
		require.NoError(t, err)
		require.True(t, inserted, "each run's first capture of its own UTC day must insert")
	}

	// (a) A row per RUN, filed under the day the run happened — not per calendar day.
	assert.Equal(t, 1, countSnapshots(ctx, t, tx, acctA, day0, domain.SnapshotSourceDailyCapture))
	assert.Equal(t, 0, countSnapshots(ctx, t, tx, acctA, day1, domain.SnapshotSourceDailyCapture),
		"there is NO backfill: an account the job did not capture on a day has no row for it")
	assert.Equal(t, 1, countSnapshots(ctx, t, tx, acctA, day2, domain.SnapshotSourceDailyCapture))

	history, err := repo.GetAggregatedHistory(ctx, f.userID.String(), day0, day3, nil)
	require.NoError(t, err)

	p0 := pointOn(history, day0)
	require.NotNil(t, p0)
	assert.InDelta(t, 1050.0, p0.TotalBalance, 0.01, "both accounts reported")

	// (b) THE HEADLINE. On day1 only item B reported. Before the carry-forward the day's total was
	// the sum of only the accounts that received a row — 60, a 94% collapse — which is the user's
	// screenshot. It must instead be item A's last known balance plus item B's new one.
	p1 := pointOn(history, day1)
	require.NotNil(t, p1, "a day one item reported on must be on the chart")
	assert.InDelta(t, 1060.0, p1.TotalBalance, 0.01,
		"the account that missed the day must contribute its last known balance, not nothing — "+
			"summing only the accounts that reported IS the reported defect")

	p2 := pointOn(history, day2)
	require.NotNil(t, p2)
	assert.InDelta(t, 1270.0, p2.TotalBalance, 0.01,
		"and the next run's own observation supersedes the carry rather than being averaged with it")

	// (c) day3: nothing at all was captured. It is ABSENT, and that is the honest answer — a
	// fabricated point would be worse. Asserted explicitly because the alternative failure (a point
	// reporting 0) is the one that looks like data.
	assert.Nil(t, pointOn(history, day3),
		"a day on which nothing was captured is absent from the series, never a zero — the "+
			"carry-forward fills an ACCOUNT on a day that exists, it does not manufacture the day")
}
