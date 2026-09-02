//go:build integration

package repository

// Issue #1569, round 14 task 7 — COMPOSITION B: the real portfolio SYNC and the real portfolio
// REPAIR, back to back, against real Postgres.
//
// WHY THIS FILE EXISTS
// portfolio_snapshot_repair_integration_test.go proves the repair's logic given hand-seeded
// holding_snapshots rows; portfolio_snapshot_repository_integration_test.go proves Upsert's logic
// given hand-seeded holdings; portfolio_snapshot_worker_test.go drives the worker against
// mockHoldingRepo/mockSnapshotRepo. No test anywhere ran the REAL capture, then the REAL Upsert,
// then the REAL repair over one dataset — so nothing proved the rows the capture actually writes
// reconstruct the aggregate Upsert actually wrote.
//
// That is the round-10 shape exactly: the Upsert and the repair used different observation rules
// while a comment claimed they agreed, and it cost $9,000 on a $5,000 portfolio. The mocks are
// what let the two drift apart unnoticed, so this test uses none.
//
// HOW THE PAST DAY IS OBTAINED, and what is and is not fabricated
// CaptureForUser files rows under the SERVER's UTC today and takes no day parameter; the repair
// deliberately never examines today. The two can therefore only meet on a day that is today when
// written and past when repaired. Rather than hand-write what the capture "would have" produced —
// which is precisely the fidelity gap this test exists to close — the real code runs, and THEN the
// resulting rows are relabelled one day earlier, in all three tables at once. Every value, every
// row, and the aggregate itself are exactly what production wrote; only the calendar label moves,
// and the accounts' liveness intervals span both days so no liveness answer changes. If the
// relabel had broken liveness the repair would report refused_no_coverage rather than a wrong
// number, so this test cannot pass vacuously through it.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestPortfolioSyncThenRepair -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// shiftUserHistoryBackOneDay relabels everything the sync just wrote for this user as belonging to
// yesterday. See the file header for why this is a relabel and not a fabrication.
func shiftUserHistoryBackOneDay(ctx context.Context, t *testing.T, db DB, userID uuid.UUID) {
	t.Helper()
	hs, err := db.Exec(ctx,
		`UPDATE holding_snapshots SET as_of_date = as_of_date - 1 WHERE user_id = $1`, userID)
	require.NoError(t, err)
	require.Positive(t, hs.RowsAffected(),
		"the real capture must have written rows, or there is nothing for the repair to read")

	ho, err := db.Exec(ctx,
		`UPDATE holding_observations SET as_of_date = as_of_date - 1 WHERE user_id = $1`, userID)
	require.NoError(t, err)
	require.Positive(t, ho.RowsAffected(),
		"the per-account capture must have recorded the looking itself, or a row left behind on "+
			"today's date silently changes which day the repair sees coverage for")

	ps, err := db.Exec(ctx,
		`UPDATE portfolio_snapshots SET snapshot_date = snapshot_date - 1 WHERE user_id = $1`, userID)
	require.NoError(t, err)
	require.EqualValues(t, 1, ps.RowsAffected(), "the real Upsert must have written exactly one aggregate")
}

// readPortfolioSnapshotCTID returns the row's physical tuple pointer.
//
// It exists because updated_at CANNOT witness "the repair rewrote nothing" from inside a single
// transaction, and the assertion that tried to was vacuous twice over: portfolio_snapshots has a
// BEFORE UPDATE trigger setting updated_at = NOW(), NOW() is transaction_timestamp() and so is
// frozen for this whole test, and in the verified case the repair's IS DISTINCT FROM guard means
// no UPDATE executes at all. ctid is observable in both directions here: any UPDATE writes a new
// tuple version and moves it, whether or not it changed a single column value.
func readPortfolioSnapshotCTID(ctx context.Context, t *testing.T, db DB, userID uuid.UUID, day time.Time) string {
	t.Helper()
	var ctid string
	require.NoError(t, db.QueryRow(ctx, `
		SELECT ctid::text FROM portfolio_snapshots
		WHERE user_id = $1 AND snapshot_date = $2::date`, userID, day).Scan(&ctid))
	return ctid
}

// closeActiveIntervalInThePast rewrites an account's 'active' interval to [today-from, today-to),
// i.e. an account that was live and then stopped being live, both in the past.
func closeActiveIntervalInThePast(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID, from, to int) {
	t.Helper()
	ct, err := db.Exec(ctx, `
		UPDATE account_status_history ash
		SET valid_from = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - make_interval(days => $2::int),
		    valid_to   = (((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC') - make_interval(days => $3::int)
		WHERE ash.account_id = $1 AND ash.status = 'active'`, accountID, from, to)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one 'active' interval must exist to rewrite")
}

func countHoldingSnapshotRows(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM holding_snapshots WHERE account_id = $1 AND present`, accountID).Scan(&n))
	return n
}

// newSyncedHolding builds the domain row a Plaid holdings response turns into, in the shape
// holdingRepository.ReplaceForAccount validates (owner and account must match the call's scalars).
func newSyncedHolding(userID, accountID, secID uuid.UUID, costBasis, instValue float64) domain.Holding {
	cb, iv := costBasis, instValue
	return domain.Holding{
		UserID:           userID.String(),
		AccountID:        accountID.String(),
		SecurityID:       secID.String(),
		Quantity:         1,
		CostBasis:        &cb,
		InstitutionValue: &iv,
	}
}

// TestPortfolioSyncThenRepair_RealCaptureAndRealUpsertAgree_Integration is the composition.
//
// The topology is chosen so that BOTH mechanisms that have historically made the two sides
// disagree are live at once:
//
//   - a DISCONNECTED brokerage whose holdings are retained (task 10's Critical 1: the capture used
//     to re-copy them every day while Upsert had stopped counting them), and
//   - a same-day RE-LINK chain (task 9.1 item A: the capture writes rows for BOTH members, and
//     summing them doubles one logical portfolio).
//
// The correct total is 10,000. 10,500 means the disconnected account leaked back in; 14,000 means
// the re-link predecessor was double-counted; 5,000 means one of the two live accounts was
// dropped. All three are numbers a caller would believe.
func TestPortfolioSyncThenRepair_RealCaptureAndRealUpsertAgree_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// The re-link chain, plus the user everything else hangs off.
	userID, predID, succID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "syncrepair")
	backdateActiveIntervalStart(ctx, t, tx, predID, 30)
	backdateActiveIntervalStart(ctx, t, tx, succID, 30)

	// A plain active brokerage on its own item.
	liveItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, liveItemID, userID, "syncrepair-live-"+liveItemID.String())
	require.NoError(t, err)
	liveID := seedAccount(ctx, t, tx, userID, liveItemID, "syncrepair-live")
	backdateLiveness(ctx, t, tx, liveID, 30)

	// A brokerage the user disconnected ten days ago. Its holdings are RETAINED — that is the
	// whole point of #1569 — so they are still on disk for the capture to trip over.
	goneItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, goneItemID, userID, "syncrepair-gone-"+goneItemID.String())
	require.NoError(t, err)
	goneID := seedAccount(ctx, t, tx, userID, goneItemID, "syncrepair-gone")
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = 'user_disconnect'
		WHERE id = $1`, goneID)
	require.NoError(t, err)
	closeActiveIntervalInThePast(ctx, t, tx, goneID, 30, 10)

	// The two accounts today's sync actually reaches, written through the REAL holdings writer —
	// holdingRepository.ReplaceForAccount is the only non-test writer of `holdings` in the whole
	// backend, and it runs the per-account capture as part of the same transaction.
	holdingRepo := NewHoldingRepository(tx)
	_, err = holdingRepo.ReplaceForAccount(ctx, liveID.String(), userID.String(),
		[]domain.Holding{newSyncedHolding(userID, liveID, secID, 4000, 5000)})
	require.NoError(t, err, "the real holdings sync for the live brokerage")
	_, err = holdingRepo.ReplaceForAccount(ctx, succID.String(), userID.String(),
		[]domain.Holding{newSyncedHolding(userID, succID, secID, 3000, 5000)})
	require.NoError(t, err, "the real holdings sync for the re-link successor")

	// RETAINED rows from syncs that already happened: the superseded predecessor (stale) and the
	// brokerage disconnected ten days ago. No sync reaches either of them today, which is exactly
	// why their rows are still on disk for the daily sweep to trip over.
	secB := seedSecurity(ctx, t, tx, "syncrepair-b")
	seedCurrentHolding(ctx, t, tx, userID, predID, secID, 3000, 4000)
	seedCurrentHolding(ctx, t, tx, userID, goneID, secB, 400, 500)

	// ── The REAL sync, in the order portfolio_snapshot_worker.go runs it ──────────────────────
	histRepo := NewHoldingSnapshotRepository(tx)
	snapRepo := NewPortfolioSnapshotRepository(tx)

	day, err := histRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err, "the real row-level capture")

	snap, err := snapRepo.Upsert(ctx, userID.String(), day)
	require.NoError(t, err, "the real aggregate write, filed under the day the capture returned")
	require.InDelta(t, 10000.0, snap.TotalValue, 0.001,
		"the aggregate must count the live brokerage and the re-link SUCCESSOR, and neither the "+
			"disconnected brokerage nor the superseded predecessor")
	require.InDelta(t, 7000.0, snap.TotalCostBasis, 0.001)

	// The predecessor's captured rows really are on disk — otherwise "the chain is counted once"
	// below would be true for the wrong reason.
	require.Equal(t, 1, countHoldingSnapshotRows(ctx, t, tx, predID),
		"the capture applies liveness only, so a superseded predecessor live during the day IS "+
			"captured; the succession exclusion happens later, in Upsert and in the repair")
	require.Equal(t, 0, countHoldingSnapshotRows(ctx, t, tx, goneID),
		"an account not live on the day must not be captured at all")

	// ── Make that day past, changing nothing else ────────────────────────────────────────────
	shiftUserHistoryBackOneDay(ctx, t, tx, userID)
	pastDay := day.AddDate(0, 0, -1)
	before := readPortfolioSnapshot(ctx, t, tx, userID, pastDay)
	beforeCTID := readPortfolioSnapshotCTID(ctx, t, tx, userID, pastDay)
	require.InDelta(t, 10000.0, before.totalValue, 0.001, "the relabel must not have touched any value")

	// ── The REAL repair ──────────────────────────────────────────────────────────────────────
	sum, err := snapRepo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined, "exactly the one past day the sync produced")
	assert.Equal(t, 0, sum.RefusedNoCoverage,
		"a refusal here would mean the capture's own rows do not cover the day its own aggregate "+
			"was written for")
	assert.Equal(t, 0, sum.RefusedPartialCoverage,
		"the repair and Upsert must agree about WHICH accounts belong to the day, or the "+
			"reconstruction is judged against a universe the aggregate never used")
	assert.Equal(t, 0, sum.Repaired,
		"THE POINT OF THIS TEST: the repair rebuilding the day from the capture's own rows must "+
			"reproduce the aggregate Upsert wrote from the same data. A 'repaired' verdict here is "+
			"the two real implementations disagreeing — the round-10 defect, from the other side")
	assert.Equal(t, 1, sum.Verified)

	after := readPortfolioSnapshot(ctx, t, tx, userID, pastDay)
	assert.InDelta(t, 10000.0, after.totalValue, 0.001)
	assert.InDelta(t, 7000.0, after.totalCostBasis, 0.001)
	assert.InDelta(t, 3000.0, after.totalGainLoss, 0.001)
	require.NotNil(t, after.totalGainLossPct)
	require.NotNil(t, before.totalGainLossPct)
	assert.InDelta(t, *before.totalGainLossPct, *after.totalGainLossPct, 0.00001,
		"3000/7000 does not terminate: Upsert stores the raw quotient and the repair rounds to 4 "+
			"decimals, so a divergence in that rounding would show up as a spurious rewrite")
	assert.Equal(t, beforeCTID, readPortfolioSnapshotCTID(ctx, t, tx, userID, pastDay),
		"a verified day must not be rewritten at all: the IS DISTINCT FROM guard in the repair's "+
			"UPDATE must exclude it, so the row keeps the tuple the Upsert wrote")

	l, ok := readLedger(ctx, t, tx, userID, pastDay)
	require.True(t, ok, "every examined day is recorded")
	assert.Equal(t, domain.PortfolioRepairOutcomeVerified, l.outcome)
	assert.Equal(t, 2, l.expectedCount,
		"the live brokerage and the re-link chain, the chain counted ONCE")
	assert.Equal(t, 2, l.observedCount)
}

// TestPortfolioSyncThenRepair_ConfirmedEmptyAccountSurvivesTheRoundTrip_Integration is the second
// half of the composition, and it is the one arm that CANNOT be exercised by the whole-user
// capture: holding_observations.
//
// markHoldingSnapshotsAbsentForUserSQL can only mark rows that already exist, so an account
// observed holding nothing with no prior history leaves no holding_snapshots row at all. The
// per-account capture records the looking itself in holding_observations; the daily whole-user
// sweep deliberately does not. This test drives the per-account capture (the real one that a
// holdings sync calls), then the real Upsert, then the real repair, and asserts a confirmed-empty
// account does not read back as an unobserved one.
func TestPortfolioSyncThenRepair_ConfirmedEmptyAccountSurvivesTheRoundTrip_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, predID, succID, secID := seedRelinkedInvestmentChain(ctx, t, tx, "syncrepair-empty")
	backdateActiveIntervalStart(ctx, t, tx, predID, 30)
	backdateActiveIntervalStart(ctx, t, tx, succID, 30)

	// The predecessor's retained, now-stale valuation. The successor has synced and reported ZERO
	// positions, so it has no holdings row of its own.
	seedCurrentHolding(ctx, t, tx, userID, predID, secID, 3000, 7000)

	histRepo := NewHoldingSnapshotRepository(tx)
	snapRepo := NewPortfolioSnapshotRepository(tx)

	// The real sync writer, handed an EMPTY position set — what investment_sync_service does after
	// a confirmed-complete /investments/holdings/get that listed the account with no positions.
	day, err := NewHoldingRepository(tx).ReplaceForAccount(ctx, succID.String(), userID.String(), nil)
	require.NoError(t, err, "the real empty-response holdings sync")
	// And the daily whole-user sweep on top, exactly as the worker would.
	sweepDay, err := histRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, day, sweepDay)

	snap, err := snapRepo.Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	require.InDelta(t, 0.0, snap.TotalValue, 0.001,
		"the successor was observed holding nothing, so the retained predecessor's stale 7000 "+
			"must not be reported as the portfolio")

	shiftUserHistoryBackOneDay(ctx, t, tx, userID)
	pastDay := day.AddDate(0, 0, -1)

	sum, err := snapRepo.RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	assert.Equal(t, 0, sum.RefusedNoCoverage,
		"the observation marker the real capture wrote is what stops a confirmed-empty successor "+
			"reading as never-synced")
	assert.Equal(t, 0, sum.Repaired, "the repair must agree with the zero Upsert already wrote")
	assert.Equal(t, 1, sum.Verified)

	got := readPortfolioSnapshot(ctx, t, tx, userID, pastDay)
	assert.InDelta(t, 0.0, got.totalValue, 0.001,
		"7000 means the retained predecessor was rebuilt into a day on which its successor had "+
			"already been observed holding nothing")
}
