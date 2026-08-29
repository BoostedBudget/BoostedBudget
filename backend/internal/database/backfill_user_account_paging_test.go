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
//  1. the scan is a keyset-paged query — LIMIT plus `a.id > $cursor`, ordered by a.id — and a FULL
//     page is followed by another query starting after the last id it returned;
//  2. one run stops at backfillAccountsMaxRowsPerRun and WARNs, instead of draining the backlog.
//
// ⚠ Failure semantics are DELIBERATELY UNCHANGED: an error still propagates and main.go still
// exits. Bounding the work per boot is not the same decision as degrading instead of exiting, and
// only the first was in scope.

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"testing"

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

// expectRepairablePage queues the mocks for one page of `n` accounts that each resolve to an
// existing user_accounts row and get UPDATEd. Returns the ids it seeded, in the order the page
// yields them (ascending, matching the query's ORDER BY a.id).
func expectRepairablePage(t *testing.T, mock pgxmock.PgxPoolIface, enc *appcrypto.FieldEncryptor, cursor string, ids []string) {
	t.Helper()
	acctType, acctSubtype := "depository", "checking"
	rows := pgxmock.NewRows([]string{"id", "user_id", "mask", "institution_id", "type", "subtype"})
	type pending struct{ id, userID, instBlind, uaID string }
	var queued []pending
	for _, id := range ids {
		userID := uuid.New().String()
		plainInstID := "ins-" + id[:8]
		encInstID, err := enc.EncryptString(plainInstID)
		require.NoError(t, err)
		encInstIDPtr := &encInstID
		// mask is NULL, so the backfill takes lookupUserAccountID's mask_blind_idx IS NULL branch,
		// whose args are (user_id, institution_id_blind_idx, identity_type, identity_subtype).
		rows.AddRow(id, userID, (*string)(nil), encInstIDPtr, &acctType, &acctSubtype)
		queued = append(queued, pending{
			id:        id,
			userID:    userID,
			instBlind: enc.BlindIndex(plainInstID),
			uaID:      uuid.New().String(),
		})
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT a.id, a.user_id, a.mask, li.institution_id`)).
		WithArgs(cursor, backfillAccountsPageSize).
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

// sortedUUIDs returns n uuids in ascending string order, so the fixture matches the ORDER BY a.id
// the paged query relies on for its cursor to make progress.
func sortedUUIDs(n int) []string {
	out := make([]string, 0, n)
	for len(out) < n {
		id := uuid.New().String()
		inserted := false
		for i := range out {
			if id < out[i] {
				out = append(out[:i], append([]string{id}, out[i:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			out = append(out, id)
		}
	}
	return out
}

// TestBackfillAccounts_PagesWithAKeysetCursor proves the scan is bounded AND that it makes
// progress: a full page is followed by a second query whose cursor is the last id of the first
// page, and the run ends when a short page comes back.
//
// RED before the fix: the unbounded query took no arguments at all, so WithArgs(cursor, pageSize)
// could not match and no second query was ever issued.
func TestBackfillAccounts_PagesWithAKeysetCursor(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()
	withBackfillPageBounds(t, 2, 100)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	firstPage := sortedUUIDs(2)
	expectRepairablePage(t, mock, enc, backfillAccountsCursorStart, firstPage)
	// The second page starts strictly after the last id of the first, and comes back short (one
	// row), which ends the run without a third query.
	secondPage := []string{uuid.New().String()}
	expectRepairablePage(t, mock, enc, firstPage[len(firstPage)-1], secondPage)

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBackfillAccounts_StopsAtTheRunCap proves ONE boot cannot be made arbitrarily long by a large
// backlog: the run stops at backfillAccountsMaxRowsPerRun and says so, rather than draining.
//
// RED before the fix: there was no cap, so a second page was fetched and the WARN never appeared.
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

	// Exactly one full page is fetched. The cap is reached, so no second query is queued — and
	// pgxmock is in ordered mode, so an unexpected extra query fails the run.
	expectRepairablePage(t, mock, enc, backfillAccountsCursorStart, sortedUUIDs(2))

	require.NoError(t, backfillAccountsUserAccountIDsWithDB(ctx, mock, enc))
	require.NoError(t, mock.ExpectationsWereMet())

	assert.Contains(t, logBuf.String(), "per-run cap",
		"hitting the cap must WARN so a persistent backlog is visible rather than silent")
}
