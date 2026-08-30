package database

// #1569 round 14 task 8 item 4 — the accounts leg of BackfillUserAccountIDs is called
// SYNCHRONOUSLY from cmd/server/main.go on every boot, with os.Exit(1) on any row's error. Before
// this test it issued ONE unbounded `SELECT ... WHERE a.user_account_id IS NULL`, materialised
// every match, and looped serially doing decrypt + lookup + INSERT + UPDATE per row. Steady state
// is a cheap indexed no-op, but the backfill exists to repair a KNOWN RECURRING write-path bug
// (#1306); if that recurs and accumulates a large backlog, every rolling restart reprocesses the
// whole backlog before the process accepts traffic, and fails CLOSED.
//
// These two tests pin the bound, not the repair (the repair is covered by
// TestBackfillUserAccountIDs_MultipleMatch_Warns and the integration tests):
//
//  1. the scan is a keyset-paged query — LIMIT plus a `(created_at, id) > $cursor` position — and a
//     FULL page is followed by another query starting after the last row it returned;
//  2. one run stops at backfillAccountsMaxRowsPerRun and WARNs, instead of draining the backlog;
//  3. #1569 round 14 task 9: the run LOADS its resume position before the first page, SAVES it when
//     it stops at the cap, and CLEARS it when the scan is exhausted.
//
// ⚠ WHAT THESE TESTS CANNOT DO, stated here because a previous round's version of them implied
// otherwise: pgxmock does not evaluate the SQL predicate. It matches the query text and the
// ARGUMENTS, so it can prove the second page is asked for with the first page's last (created_at,
// id) — and the old version could not even do that, because it generated the second page's id
// randomly, leaving it not necessarily greater than the cursor. It can never prove the query
// actually SELECTs the right rows. That is what
// backfill_user_account_cursor_integration_test.go exists for, against real PostgreSQL.
//
// ⚠ Failure semantics are DELIBERATELY UNCHANGED: an error still propagates and main.go still
// exits. Bounding the work per boot is not the same decision as degrading instead of exiting, and
// only the first was in scope.

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// withBackfillPageBounds temporarily shrinks the paging constants so a test can express "a full
// page" in two rows instead of five hundred. Restored via t.Cleanup.
func withBackfillPageBounds(t *testing.T, pageSize, maxRows int) {
	t.Helper()
	origPage, origMax := backfillAccountsPageSize, backfillAccountsMaxRowsPerRun
	backfillAccountsPageSize, backfillAccountsMaxRowsPerRun = pageSize, maxRows
	t.Cleanup(func() {
		backfillAccountsPageSize, backfillAccountsMaxRowsPerRun = origPage, origMax
	})
}

// backfillPageRow is one seeded row of a mocked page: an EXPLICIT (created_at, id) pair, because
// (created_at, id) is the scan's sort key and therefore its cursor. Both components are pinned
// rather than random — the previous version of this file generated the second page's id randomly,
// so it was not guaranteed to be greater than the cursor and the test could pass over a fixture the
// real query would never produce.
type backfillPageRow struct {
	id        string
	createdAt time.Time
}

// backfillPageRows returns n rows whose ids ascend within a single created_at instant, then the
// next instant, so the fixture is a valid ordering under `ORDER BY a.created_at, a.id`.
func backfillPageRows(base time.Time, n int) []backfillPageRow {
	out := make([]backfillPageRow, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, backfillPageRow{
			id:        fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1),
			createdAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	return out
}

// expectCursorLoad queues the sweep_cursors read every run issues before its first page. `ts` nil
// means "no stored position" (the row is absent), which is how absence is encoded.
func expectCursorLoad(mock pgxmock.PgxPoolIface, ts *time.Time, exclusive bool) {
	rows := pgxmock.NewRows([]string{"ts1", "exclusive"})
	if ts != nil {
		rows.AddRow(ts, exclusive)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ts1, exclusive FROM sweep_cursors`)).
		WithArgs(backfillAccountsSweepCursorName).
		WillReturnRows(rows)
}

// expectCursorSave queues the upsert a CAPPED run performs. Only the timestamp is persisted; the
// id component of the position is reconstituted from a sentinel on resume.
func expectCursorSave(mock pgxmock.PgxPoolIface, ts time.Time, exclusive bool) {
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO sweep_cursors`)).
		WithArgs(backfillAccountsSweepCursorName, ts, exclusive).
		WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))
}

// expectCursorClear queues the delete a run performs when it reaches the end of its scan.
func expectCursorClear(mock pgxmock.PgxPoolIface) {
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM sweep_cursors`)).
		WithArgs(backfillAccountsSweepCursorName).
		WillReturnResult(pgconn.NewCommandTag("DELETE 1"))
}

// expectRepairablePage queues the mocks for one page of accounts that each resolve to an existing
// user_accounts row and get UPDATEd, asserting the page was asked for at exactly `cursor` (nil =
// from the beginning, which carries no position predicate).
func expectRepairablePage(t *testing.T, mock pgxmock.PgxPoolIface, enc *appcrypto.FieldEncryptor, cursor *backfillAccountsCursor, page []backfillPageRow) {
	t.Helper()
	acctType, acctSubtype := "depository", "checking"
	rows := pgxmock.NewRows([]string{"id", "user_id", "mask", "institution_id", "type", "subtype", "created_at"})
	type pending struct{ id, userID, instBlind, uaID string }
	var queued []pending
	for _, r := range page {
		userID := uuid.New().String()
		plainInstID := "ins-" + r.id[:8]
		encInstID, err := enc.EncryptString(plainInstID)
		require.NoError(t, err)
		encInstIDPtr := &encInstID
		// mask is NULL, so the backfill takes lookupUserAccountID's mask_blind_idx IS NULL branch,
		// whose args are (user_id, institution_id_blind_idx, identity_type, identity_subtype).
		rows.AddRow(r.id, userID, (*string)(nil), encInstIDPtr, &acctType, &acctSubtype, r.createdAt)
		queued = append(queued, pending{
			id:        r.id,
			userID:    userID,
			instBlind: enc.BlindIndex(plainInstID),
			uaID:      uuid.New().String(),
		})
	}
	var (
		cursorTS *time.Time
		cursorID = minBackfillSentinelID
	)
	if cursor != nil {
		ts := cursor.createdAt
		cursorTS, cursorID = &ts, cursor.id
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT a.id, a.user_id, a.mask, li.institution_id`)).
		WithArgs(cursorTS, cursorID, backfillAccountsPageSize).
		WillReturnRows(rows)
	for _, p := range queued {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM user_accounts`)).
			WithArgs(p.userID, p.instBlind, &acctType, &acctSubtype).
			WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(p.uaID))
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE accounts SET user_account_id`)).
			WithArgs(p.uaID, p.id).
			WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))
	}
}

// TestBackfillAccounts_PagesWithAKeysetCursor proves the scan is bounded AND that it makes
// progress: a full page is followed by a second query whose cursor is the last (created_at, id) of
// the first page, and the run ends when a short page comes back.
//
// RED before the fix: the unbounded query took no arguments at all, so WithArgs could not match and
// no second query was ever issued.
func TestBackfillAccounts_PagesWithAKeysetCursor(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 100)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	firstPage := backfillPageRows(base, 2)
	expectCursorLoad(mock, nil, false)
	expectRepairablePage(t, mock, enc, nil, firstPage)
	// The second page starts strictly after the last row of the first, and comes back short (one
	// row), which ends the run without a third query.
	last := firstPage[len(firstPage)-1]
	secondPage := backfillPageRows(base.Add(time.Hour), 1)
	expectRepairablePage(t, mock, enc, &backfillAccountsCursor{createdAt: last.createdAt, id: last.id}, secondPage)
	// The scan was exhausted, so the run stores nothing — it CLEARS, which is what makes a
	// past-the-end watermark an unrepresentable state.
	expectCursorClear(mock)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBackfillAccounts_StopsAtTheRunCap proves ONE boot cannot be made arbitrarily long by a large
// backlog: the run stops at backfillAccountsMaxRowsPerRun and says so, rather than draining — and
// PERSISTS where it stopped, so the next boot does not re-read the same rows.
//
// RED before the fix: there was no cap, so a second page was fetched and the WARN never appeared.
// RED before task 9: nothing was persisted, so the Save expectation could not be met.
func TestBackfillAccounts_StopsAtTheRunCap(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	var logBuf bytes.Buffer
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Exactly one full page is fetched. The cap is reached, so no second page query is queued — and
	// pgxmock is in ordered mode, so an unexpected extra query fails the run.
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	page := backfillPageRows(base, 2)
	expectCursorLoad(mock, nil, false)
	expectRepairablePage(t, mock, enc, nil, page)
	// Inclusive (exclusive=false): this run started from the beginning, so it made progress along
	// the created_at axis by definition and re-visiting the boundary tie is harmless.
	expectCursorSave(mock, page[len(page)-1].createdAt, false)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())

	assert.Contains(t, logBuf.String(), "per-run cap",
		"hitting the cap must WARN so a persistent backlog is visible rather than silent")
}

// TestBackfillAccounts_ResumesFromTheStoredPosition proves the stored watermark is turned back into
// a keyset position and handed to the FIRST page query — the whole point of persisting it. The
// stored form is a timestamp plus an `exclusive` flag; the id component is the min sentinel here
// (inclusive), which makes the predicate mean "created_at >= T".
func TestBackfillAccounts_ResumesFromTheStoredPosition(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 100)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	stored := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	expectCursorLoad(mock, &stored, false)
	expectRepairablePage(t, mock, enc,
		&backfillAccountsCursor{createdAt: stored, id: minBackfillSentinelID},
		backfillPageRows(stored.Add(time.Hour), 1))
	expectCursorClear(mock)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBackfillAccounts_CapReachedOnOneTimestampSavesExclusive pins the one case where an inclusive
// save would be unbounded: the run RESUMED at a stored key and spent its entire cap on rows sharing
// that key, so saving it inclusively would hand the next boot the identical page forever. The saved
// position goes exclusive instead, which guarantees forward progress along the created_at axis.
//
// ⚠ The price is stated in nextBackfillCursorExclusive: the rest of that one over-cap tie group is
// then reached on no run. It is the same residual #1477 accepted, for the same reason.
func TestBackfillAccounts_CapReachedOnOneTimestampSavesExclusive(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 2)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	stored := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	tie := []backfillPageRow{
		{id: "00000000-0000-4000-8000-000000000001", createdAt: stored},
		{id: "00000000-0000-4000-8000-000000000002", createdAt: stored},
	}
	expectCursorLoad(mock, &stored, false)
	expectRepairablePage(t, mock, enc, &backfillAccountsCursor{createdAt: stored, id: minBackfillSentinelID}, tie)
	expectCursorSave(mock, stored, true)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBackfillAccounts_StaleWatermarkWrapsWithinTheRun pins the reset half of the mechanism at the
// query level: a stored position whose first page comes back EMPTY is a stale watermark, not "no
// work to do". The run clears it and re-asks FROM THE BEGINNING within the same run, so a row
// sorting behind the watermark is repaired on that boot rather than never.
//
// Without this, a forward-only persisted cursor would sit past the end of the scan permanently and
// starve every later row at the tail — the same defect at the other end.
func TestBackfillAccounts_StaleWatermarkWrapsWithinTheRun(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 100)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	stored := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	expectCursorLoad(mock, &stored, false)
	// Resumed page: empty.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT a.id, a.user_id, a.mask, li.institution_id`)).
		WithArgs(&stored, minBackfillSentinelID, backfillAccountsPageSize).
		WillReturnRows(pgxmock.NewRows([]string{"id", "user_id", "mask", "institution_id", "type", "subtype", "created_at"}))
	// The wrap: clear, then re-ask with NO position at all.
	expectCursorClear(mock)
	expectRepairablePage(t, mock, enc, nil, backfillPageRows(stored.Add(-24*time.Hour), 1))
	expectCursorClear(mock)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())
}
