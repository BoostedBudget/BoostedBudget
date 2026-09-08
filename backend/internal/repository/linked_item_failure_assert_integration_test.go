//go:build integration

package repository

// Integration tests for LinkedItemRepository.MarkFailureAsserted and RepairStatus's notNewerThan
// bound (Issue #1623).
//
// A real database is mandatory here, not a convenience. The central claim — that
// TransitionErrorStatus writes NOTHING for a same-status re-assert, and therefore cannot order two
// consecutive failures of the same kind — is a property of a WHERE clause evaluated by Postgres.
// pgxmock matches query text and would happily report a row affected for a statement that, in
// reality, matched none; the whole defect hides in exactly that gap. The timestamps compared here
// are all written by now(), so this is one database's clock against itself.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemFailureAssert -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func readFailureClocks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) (assertedAt, statusChangedAt *time.Time) {
	t.Helper()
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT last_failure_asserted_at, status_changed_at FROM linked_items WHERE id = $1`, itemID,
	).Scan(&assertedAt, &statusChangedAt))
	return assertedAt, statusChangedAt
}

func newFailureAssertFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status domain.LinkedItemStatus) (LinkedItemRepository, string, *domain.LinkedItem) {
	t.Helper()
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, "fassert"+suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, "fassert"+suffix, status)
	return repo, userID, item
}

// TestLinkedItemFailureAssert_Integration_SameStatusReAssertAdvancesTheClockThatStatusChangedAtCannot
// is the falsifiable form of #1623's premise. It asserts BOTH halves in one test on purpose: that
// status_changed_at does NOT move (proving TransitionErrorStatus really is a no-op here, rather
// than the test having set up something else), and that last_failure_asserted_at DOES.
//
// If someone "simplifies" MarkFailureAsserted away by folding the column into
// TransitionErrorStatus's SET list, the second assertion fails — because the predicate, not the
// SET list, is what suppresses the write.
func TestLinkedItemFailureAssert_Integration_SameStatusReAssertAdvancesTheClockThatStatusChangedAtCannot(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, _, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusActive)

	// First failure: a real transition.
	transitioned, err := repo.TransitionErrorStatus(ctx, item.ID, domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "creds changed", []domain.LinkedItemStatus{domain.LinkedItemStatusDisconnected})
	require.NoError(t, err)
	require.True(t, transitioned)
	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, []domain.LinkedItemStatus{domain.LinkedItemStatusDisconnected}))

	firstAsserted, firstStatusChanged := readFailureClocks(t, ctx, pool, item.ID)
	require.NotNil(t, firstAsserted)
	require.NotNil(t, firstStatusChanged)

	time.Sleep(10 * time.Millisecond)

	// SECOND failure, same kind. This is the case #1623 exists for.
	transitioned, err = repo.TransitionErrorStatus(ctx, item.ID, domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "creds changed again", []domain.LinkedItemStatus{domain.LinkedItemStatusDisconnected})
	require.NoError(t, err)
	require.False(t, transitioned,
		"a same-status re-assert must match ZERO rows — this no-op is the premise of the whole issue")
	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, []domain.LinkedItemStatus{domain.LinkedItemStatusDisconnected}))

	secondAsserted, secondStatusChanged := readFailureClocks(t, ctx, pool, item.ID)
	require.NotNil(t, secondAsserted)
	require.NotNil(t, secondStatusChanged)

	assert.True(t, secondStatusChanged.Equal(*firstStatusChanged),
		"status_changed_at must NOT have moved — it is exactly why it cannot order two same-status failures")
	assert.True(t, secondAsserted.After(*firstAsserted),
		"last_failure_asserted_at MUST advance on a same-status re-assert; got %s then %s",
		firstAsserted, secondAsserted)
}

// TestLinkedItemFailureAssert_Integration_RepairStatusRefusesToClearANewerFailure is the defect
// itself, end to end: failure A, a LOGIN_REPAIRED webhook received for A, failure B asserted
// before that webhook is processed. The clear must be refused, and the item must still read as
// broken afterwards — a false 'active' here is a connection that reports healthy and never syncs.
func TestLinkedItemFailureAssert_Integration_RepairStatusRefusesToClearANewerFailure(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, _, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusLoginRequired)

	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, nil))

	// The webhook for failure A is received now...
	var webhookReceivedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT now()`).Scan(&webhookReceivedAt))

	time.Sleep(10 * time.Millisecond)

	// ...but failure B is asserted before it is processed.
	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, nil))

	repaired, err := repo.RepairStatus(ctx, item.ID, "",
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired}, &webhookReceivedAt)
	require.NoError(t, err)
	assert.False(t, repaired, "a clear must not reach past a failure asserted after the webhook was received")

	var status *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM linked_items WHERE id = $1`, item.ID).Scan(&status))
	require.NotNil(t, status)
	assert.Equal(t, string(domain.LinkedItemStatusLoginRequired), *status,
		"the newer failure must survive intact — refusing the clear is only useful if nothing else moved")
}

// TestLinkedItemFailureAssert_Integration_RepairStatusStillClearsAStaleFailure is the other
// direction, and it is not optional: a bound that refuses everything would pass the test above
// while breaking every legitimate repair. Same setup, opposite ordering.
func TestLinkedItemFailureAssert_Integration_RepairStatusStillClearsAStaleFailure(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, _, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusLoginRequired)

	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, nil))
	time.Sleep(10 * time.Millisecond)

	// The webhook is received AFTER the only failure on record.
	var webhookReceivedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT now()`).Scan(&webhookReceivedAt))

	repaired, err := repo.RepairStatus(ctx, item.ID, "",
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired}, &webhookReceivedAt)
	require.NoError(t, err)
	assert.True(t, repaired, "a webhook received after the failure it reports must still clear it")
}

// TestLinkedItemFailureAssert_Integration_NullAssertClockIsNotTreatedAsNewer covers every row that
// existed before migration 000189. If NULL were treated as "newer", the bound would make every
// pre-migration item permanently unrepairable by the webhook path — a silent, total outage of the
// LOGIN_REPAIRED handler that no unit test would show.
func TestLinkedItemFailureAssert_Integration_NullAssertClockIsNotTreatedAsNewer(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, _, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusLoginRequired)

	assertedAt, _ := readFailureClocks(t, ctx, pool, item.ID)
	require.Nil(t, assertedAt, "fixture precondition: no failure assert on record")

	var webhookReceivedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT now()`).Scan(&webhookReceivedAt))

	repaired, err := repo.RepairStatus(ctx, item.ID, "",
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired}, &webhookReceivedAt)
	require.NoError(t, err)
	assert.True(t, repaired, "an item with no assert on record must stay repairable")
}

// TestLinkedItemFailureAssert_Integration_TerminalStatusesAreNotStamped: MarkFailureAsserted
// excludes the same terminal statuses TransitionErrorStatus does, so the two writes agree about
// which items a background error signal may touch at all. Without this, a stale signal would
// advance the clock on an item the user deliberately disconnected.
func TestLinkedItemFailureAssert_Integration_TerminalStatusesAreNotStamped(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, _, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusDisconnected)

	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID,
		[]domain.LinkedItemStatus{domain.LinkedItemStatusDisconnected, domain.LinkedItemStatusReplaced, domain.LinkedItemStatusRevoked}))

	assertedAt, _ := readFailureClocks(t, ctx, pool, item.ID)
	assert.Nil(t, assertedAt, "a disconnected item must not have a failure-assert clock advanced on it")
}

// TestLinkedItemFailureAssert_Integration_RepairDoesNotResetTheAssertClock pins a deliberate
// non-behaviour. RepairStatus leaves last_failure_asserted_at where it is, so that a
// repaired-then-rebroken item's stamp advances to the NEW failure on the next assert. Clearing it
// to NULL on repair would be harmless in isolation but would silently widen the window in which a
// delayed webhook is accepted.
func TestLinkedItemFailureAssert_Integration_RepairDoesNotResetTheAssertClock(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo, userID, item := newFailureAssertFixture(t, ctx, pool, domain.LinkedItemStatusLoginRequired)

	require.NoError(t, repo.MarkFailureAsserted(ctx, item.ID, nil))
	before, _ := readFailureClocks(t, ctx, pool, item.ID)
	require.NotNil(t, before)

	repaired, err := repo.RepairStatus(ctx, item.ID, userID,
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired}, nil)
	require.NoError(t, err)
	require.True(t, repaired)

	after, _ := readFailureClocks(t, ctx, pool, item.ID)
	require.NotNil(t, after, "repair must not clear last_failure_asserted_at")
	assert.True(t, after.Equal(*before))
}

// ── #1613 P4-T3 item E ───────────────────────────────────────────────────────────────────────

// TestLinkedItem_SetInvestmentsSkippedAtLink_WritesBothDirections pins that the setter is a real
// setter and not a one-way latch. The re-link path (LinkAccounts reusing an existing
// linked_items row) can call it with FALSE on a row already carrying TRUE, and a latch would
// leave that item permanently reported as "investments skipped" after its institution started
// serving them.
//
// Integration rather than pgxmock because the column itself is new (migration 000189) and a mock
// cannot tell a column that exists from one that does not.
func TestLinkedItem_SetInvestmentsSkippedAtLink_WritesBothDirections(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, _, _ := seedInvestmentFixture(ctx, t, tx, "li-skipflag")
	repo := NewLinkedItemRepository(tx, newTestFieldEncryptor(t))

	read := func() bool {
		var v bool
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT investments_skipped_at_link FROM linked_items WHERE id = $1`, linkedItemID).Scan(&v))
		return v
	}

	require.False(t, read(), "migration 000189's default is FALSE for a freshly created row")

	require.NoError(t, repo.SetInvestmentsSkippedAtLink(ctx, linkedItemID.String(), true))
	require.True(t, read(), "the skip must be recorded")

	require.NoError(t, repo.SetInvestmentsSkippedAtLink(ctx, linkedItemID.String(), false))
	require.False(t, read(),
		"FALSE must overwrite a stored TRUE — a write-only-TRUE setter strands a re-linked item "+
			"as permanently 'investments skipped' after its institution starts serving them")

	// It must not disturb the item's health columns: this records what Plaid granted, not how the
	// connection is doing.
	var status string
	var lastFailure *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT status, last_failure_asserted_at FROM linked_items WHERE id = $1`, linkedItemID).
		Scan(&status, &lastFailure))
	require.Equal(t, "active", status)
	require.Nil(t, lastFailure)

	_ = userID
}
