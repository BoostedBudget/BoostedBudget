//go:build integration

package repository

// Issue #1569, round 14 task 7 — COMPOSITION A: an account that has been RESTORED and then
// USER-DISCONNECTED, and only then purged.
//
// Every individual transition here already has coverage. What did not exist anywhere is the
// 5-step CHAIN:
//
//	link → provider-absent removal → restore → user-disconnect → PURGE
//
// The reason to run it is that a restore does not merely flip `status`: it CLEARS `removed_at`
// and `removed_reason`, so by step 4 the row's removal state has been set, cleared, and set again
// with a DIFFERENT cause. Nothing proved that the purge cascade, the audit write, or — more
// importantly — the restore REFUSAL behave correctly on a row with that history. The two things
// this composition is really asking:
//
//   - does the second removal's cause actually stick, so a later sync cannot resurrect an account
//     the user deliberately disconnected? (the negative assertion the brief demands)
//   - does the purge still destroy exactly what it should and retain exactly what it should, with
//     its audit row committed in the same transaction?
//
// This must run against real Postgres. pgxmock cannot enforce migration 000154's
// accounts_removed_reason_valid CHECK, cannot fire the account_status_history trigger, and cannot
// prove a CASCADE ran — and mocks are precisely what let two sides of a composition drift apart.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestAccountLifecycle_RestoredThenDisconnected -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// readRemovalState returns (status, removed_reason, removed_at) for one account.
func readRemovalState(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID) (string, *string, *time.Time) {
	t.Helper()
	var status string
	var reason *string
	var at *time.Time
	require.NoError(t, db.QueryRow(ctx,
		`SELECT status, removed_reason, removed_at FROM accounts WHERE id = $1`, accountID,
	).Scan(&status, &reason, &at))
	return status, reason, at
}

func countAuditRows(ctx context.Context, t *testing.T, db DB, action, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE action = $1 AND resource_type = 'account' AND resource_id = $2`,
		action, resourceID).Scan(&n))
	return n
}

// TestAccountLifecycle_RestoredThenDisconnectedThenPurged_Integration runs the whole 5-step chain
// against real Postgres, with the real repository methods at every step.
func TestAccountLifecycle_RestoredThenDisconnectedThenPurged_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// An outer transaction rolled back at the end keeps this fixture out of the shared database
	// entirely. PurgeAccount / RestoreProviderRemovedAccount each Begin() on the handle they are
	// given; against a pgx.Tx that is a SAVEPOINT, which preserves the property each of them
	// depends on — the check and the write are atomic with respect to each other — while still
	// letting this test discard everything at the end.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	auditRepo := NewAuditLogRepository(tx)

	f := seedRetainedFixture(ctx, t, tx, "restore-purge-"+uuid.New().String())
	acct := f.AccountID.String()
	user := f.UserID.String()

	// History that must SURVIVE the purge (a banking transaction, ON DELETE SET NULL) and history
	// that must be DESTROYED by it (a balance snapshot, explicitly deleted; a holding, CASCADE).
	txnID := seedBankingTxn(ctx, t, tx, f, time.Now().UTC().AddDate(0, 0, -20), "Coffee Before Everything")
	snapshotID := seedBalanceSnapshot(ctx, t, tx, f)
	holdingID := seedHolding(ctx, t, tx, f)

	// ── Step 1: linked and active ─────────────────────────────────────────────────────────────
	status, reason, removedAt := readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusActive), status)
	require.Nil(t, reason)
	require.Nil(t, removedAt)

	// ── Step 2: the PROVIDER stopped returning it ─────────────────────────────────────────────
	require.NoError(t, repo.MarkRemovedByProvider(ctx, acct, user))
	status, reason, removedAt = readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusRemoved), status)
	require.NotNil(t, reason)
	require.Equal(t, string(domain.RemovalReasonProviderAbsent), *reason,
		"the cause must be recorded, or step 3 cannot legitimately restore it")
	require.NotNil(t, removedAt)

	// ── Step 3: the bank started reporting it again — the ONE sanctioned un-removal ────────────
	require.NoError(t, repo.RestoreProviderRemovedAccount(ctx, acct, user))
	status, reason, removedAt = readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusActive), status)
	assert.Nil(t, reason, "removed_reason must be cleared — 000154's CHECK forbids it off a non-removed row")
	assert.Nil(t, removedAt,
		"removed_at must be cleared too: 000149's CHECK is one-directional and would tolerate a "+
			"stale timestamp on a live row, so this clearing is enforced only by the Go code")

	// The restore genuinely returned the row to LIVE, which is what makes the rest of this chain
	// meaningful rather than a relabelled tombstone: a purge must now be refused.
	require.ErrorIs(t, repo.PurgeAccount(ctx, acct, user, auditRepo), ErrAccountNotDisconnected,
		"a restored account is live again; purging it would let the next sync recreate the row")
	require.Equal(t, 1, countRows(ctx, t, tx, "accounts", "id", f.AccountID),
		"the refused purge must have rolled back")
	require.Equal(t, 0, countAuditRows(ctx, t, tx, "account.history_purged", acct),
		"a refused purge must leave no audit row claiming a purge happened")

	// ── Step 4: the USER disconnects the institution ──────────────────────────────────────────
	require.NoError(t, repo.MarkRemovedByLinkedItemID(ctx, f.LinkedItemID.String(), domain.RemovalReasonUserDisconnect))
	status, reason, removedAt = readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusRemoved), status,
		"MarkRemovedByLinkedItemID is scoped `AND status = 'active'` — the restore in step 3 is "+
			"what makes this row reachable at all")
	require.NotNil(t, reason)
	assert.Equal(t, string(domain.RemovalReasonUserDisconnect), *reason)
	assert.NotNil(t, removedAt, "removed_at must be re-stamped after having been cleared by the restore")

	// ⚠ THE NEGATIVE THE BRIEF ASKS FOR. Having been restorable once is not a property the row
	// keeps. A sync arriving now must NOT be able to undo the user's disconnect, and the only
	// thing standing between it and that is the cause column having been overwritten correctly by
	// step 4 rather than left at its step-2 value.
	require.ErrorIs(t, repo.RestoreProviderRemovedAccount(ctx, acct, user), ErrAccountNotRestorable,
		"a user_disconnect tombstone must never be undone by a sync, even on a row that was "+
			"legitimately restored from a provider_absent removal earlier in its life")
	status, _, _ = readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusRemoved), status,
		"the refused restore must change nothing")

	// ── Step 5: the user explicitly purges ────────────────────────────────────────────────────
	require.NoError(t, repo.PurgeAccount(ctx, acct, user, auditRepo))

	assert.Equal(t, 0, countRows(ctx, t, tx, "accounts", "id", f.AccountID), "account row must be gone")
	assert.Equal(t, 0, countRows(ctx, t, tx, "balance_snapshots", "id", snapshotID),
		"balance_snapshots are SET NULL on the FK and must be deleted explicitly by the purge")
	assert.Equal(t, 0, countRows(ctx, t, tx, "holdings", "id", holdingID), "holdings must CASCADE away")
	assert.Equal(t, 0, countRows(ctx, t, tx, "account_status_history", "account_id", f.AccountID),
		"the whole removed→active→removed transition history CASCADEs with the account row")

	// The audit row, written inside the purge's own transaction and therefore visible now.
	assert.Equal(t, 1, countAuditRows(ctx, t, tx, "account.history_purged", acct),
		"the purge must be audited, and in the same transaction as the destruction")

	// And what must NOT have been destroyed: the user's own transaction, orphaned rather than
	// deleted. This is the retention half of the whole issue.
	var accountIDAfter *uuid.UUID
	require.NoError(t, tx.QueryRow(ctx, `SELECT account_id FROM transactions WHERE id = $1`, txnID).Scan(&accountIDAfter))
	assert.Nil(t, accountIDAfter,
		"the transaction must survive with account_id NULL — a purge destroys the account's own "+
			"derived rows, never the user's financial history")
}

// TestAccountLifecycle_RestoreDoesNotResurrectALinkRemovedSweepTombstone_Integration is the third
// removal cause, which the chain above does not reach.
//
// MarkRemovedByLinkedItemID takes the cause as a PARAMETER, and the orphaned-Plaid-item sweep
// passes RemovalReasonLinkRemoved — a background sweep, not a user disconnect and not the provider
// dropping the account. Restoring a row this cause carries would be the same defect as restoring a
// user_disconnect: nobody asked for the account back.
//
// Run alongside the chain above because the chain proves the refusal for exactly ONE of the two
// non-restorable causes, and "the enumeration was incomplete" is this branch's other recurring
// defect class.
func TestAccountLifecycle_RestoreDoesNotResurrectALinkRemovedSweepTombstone_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	f := seedRetainedFixture(ctx, t, tx, "sweep-tombstone-"+uuid.New().String())

	require.NoError(t, repo.MarkRemovedByLinkedItemID(ctx, f.LinkedItemID.String(), domain.RemovalReasonLinkRemoved))
	status, reason, _ := readRemovalState(ctx, t, tx, f.AccountID)
	require.Equal(t, string(domain.AccountStatusRemoved), status)
	require.NotNil(t, reason)
	require.Equal(t, string(domain.RemovalReasonLinkRemoved), *reason)

	require.ErrorIs(t, repo.RestoreProviderRemovedAccount(ctx, f.AccountID.String(), f.UserID.String()),
		ErrAccountNotRestorable,
		"a sweep-removed account is not a provider-absent removal and must not auto-restore")

	// And the sweep cause must be refused at the SOURCE too: MarkRemovedByLinkedItemID must never
	// be talked into stamping 'provider_absent', which is the only value that would make these
	// rows silently resurrectable.
	require.Error(t, repo.MarkRemovedByLinkedItemID(ctx, f.LinkedItemID.String(), domain.RemovalReasonProviderAbsent),
		"this path is never the provider dropping an account")
}
