//go:build integration

package repository

// Integration tests for LinkedItemRepository.MarkPlaidRemoved (Issue #1495).
//
// WHY THESE MUST RUN AGAINST REAL POSTGRES AND NOT pgxmock. The whole design rests on one claim:
// that a 'replaced' row CAN be stamped with plaid_removed_at, unlike status, which
// MarkItemsDisconnected deliberately refuses to overwrite for such a row (pinned by
// TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted). pgxmock matches
// query TEXT — it would happily "prove" a stamp that the real WHERE clause never applied, and it
// cannot show that the row's status and replaced_at survived. If that claim were false, tightening
// DeleteUserCascade's C2 guard onto this column would deadlock every re-linked user's GDPR erasure
// forever, so it is proved here against the real schema.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_MarkPlaidRemoved -tags integration -v -p 1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinkedItemRepository_MarkPlaidRemoved_Integration_StampsReplacedRowWithoutRevertingStatus is
// THE test the whole change depends on: the deadlock is genuinely resolved.
//
// It asserts both halves, because either alone would be misleading:
//   - the stamp LANDS on a 'replaced' row (so C2 can demand proof without blocking such users
//     forever), and
//   - status/replaced_at/replaced_by_id are UNTOUCHED (so the re-link chain survives — the reason
//     MarkItemsDisconnected must not be widened to cover this case, and therefore the reason a second
//     column exists at all).
//
// Falsifiability: adding `AND (status IS NULL OR status NOT IN ('disconnected','replaced'))` to
// MarkPlaidRemoved's WHERE — i.e. copying MarkItemsDisconnected's guard, the obvious "for symmetry"
// edit — makes this test fail with a zero-row ErrNoLinkedItemsUpdated. Verified by doing exactly that.
func TestLinkedItemRepository_MarkPlaidRemoved_Integration_StampsReplacedRowWithoutRevertingStatus(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)
	replacement := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-new")

	// Exactly what the re-link path leaves behind on the OLD item (account_service.go, "re-link
	// detected, soft-deleting old linked item"): a terminal soft-delete state carrying the chain
	// forward, and no Plaid /item/remove call.
	_, err := pool.Exec(ctx, `
		UPDATE linked_items
		SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2
		WHERE id = $1`, item.ID, replacement.ID)
	require.NoError(t, err)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{item.ID}),
		"a 'replaced' row MUST be stampable — it is the one row shape MarkItemsDisconnected refuses, "+
			"and therefore the only reason this column exists")

	var status string
	var stamp, replacedAt *time.Time
	var replacedByID *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, plaid_removed_at, replaced_at, replaced_by_id FROM linked_items WHERE id = $1`,
		item.ID).Scan(&status, &stamp, &replacedAt, &replacedByID))

	require.NotNil(t, stamp, "the stamp must actually be present in the column, not merely implied by a nil return")
	assert.Equal(t, "replaced", status, "MarkPlaidRemoved must not touch status: reverting 'replaced' would "+
		"destroy the re-link chain, which is precisely why MarkItemsDisconnected refuses to and why this "+
		"second column was added instead of widening that guard")
	assert.NotNil(t, replacedAt, "replaced_at must survive the stamp")
	if assert.NotNil(t, replacedByID, "replaced_by_id must survive the stamp") {
		assert.Equal(t, replacement.ID, *replacedByID, "the re-link chain must still point at the replacement item")
	}
}

// TestLinkedItemRepository_MarkPlaidRemoved_Integration_OnlyStampsGivenItems is the falsifiability
// control: an implementation that dropped `WHERE id = ANY($2)` (or correlated on user_id instead)
// would satisfy every other test in this file while silently declaring a live sibling item's Plaid
// side done — which, with C2 now reading this column, would let a GDPR erasure complete over that
// live Item. Exactly the class of bug MarkItemsDisconnected's own OnlyMarksGivenItems test guards.
func TestLinkedItemRepository_MarkPlaidRemoved_Integration_OnlyStampsGivenItems(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	itemA := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-a")
	itemB := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-b")

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{itemA.ID}))

	var aStamped, bStamped bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at IS NOT NULL FROM linked_items WHERE id = $1`, itemA.ID).Scan(&aStamped))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at IS NOT NULL FROM linked_items WHERE id = $1`, itemB.ID).Scan(&bStamped))

	assert.True(t, aStamped, "the given item must be stamped")
	assert.False(t, bStamped, "an item NOT in ids must be left alone — stamping it would tell DeleteUserCascade "+
		"that a live Plaid Item had been removed, which is the orphaned-billing-Item bug this column exists to close")
}

// TestLinkedItemRepository_MarkPlaidRemoved_Integration_ReStampKeepsFirstInstant pins the
// COALESCE(plaid_removed_at, $1) form. A re-stamp must be a no-op on the VALUE, not a refresh: the
// earlier instant is the closer one to the truth, and a retry that moved it later would silently
// rewrite history on every attempt.
//
// It also pins that a re-stamp is not an ERROR — which is what makes the write safe to retry. That
// property is why the WHERE clause deliberately does NOT carry `AND plaid_removed_at IS NULL`: with
// that guard, this legitimate re-run would report zero rows and be indistinguishable from a row that
// had vanished.
//
// Falsifiability: replacing COALESCE with a bare `plaid_removed_at = $1` makes the equality assertion
// fail; adding `AND plaid_removed_at IS NULL` to the WHERE makes the NoError assertion fail.
func TestLinkedItemRepository_MarkPlaidRemoved_Integration_ReStampKeepsFirstInstant(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{item.ID}))
	var first time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, item.ID).Scan(&first))

	// A measurable gap, so "unchanged" cannot be confused with "rewritten to an indistinguishable
	// value" by clock resolution.
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{item.ID}),
		"a re-stamp must be an idempotent success, not an error — otherwise a retried worker never converges")
	var second time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, item.ID).Scan(&second))

	assert.True(t, first.Equal(second),
		"the FIRST stamp must win: %v became %v. A refresh on every retry would move a recorded fact forward "+
			"in time for no reason", first, second)
}

// TestLinkedItemRepository_MarkPlaidRemoved_Integration_MissingIDReturnsSentinel pins the one meaning
// a zero-row result can carry here, and it is NOT the meaning MarkItemsDisconnected's identical
// sentinel carries. MarkPlaidRemoved has no status guard, so no existing row can be filtered out of
// it: zero rows can only mean the id does not exist. Both service call sites rely on that reading to
// treat it as success (there is nothing left to record, and an absent row cannot block C2's EXISTS)
// rather than as a permanent failure.
func TestLinkedItemRepository_MarkPlaidRemoved_Integration_MissingIDReturnsSentinel(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, enc)

	err := repo.MarkPlaidRemoved(ctx, []string{uuid.NewString()})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoLinkedItemsUpdated),
		"a stamp against a non-existent id must be the errors.Is-checkable sentinel, so callers can tell it "+
			"apart from a genuine database failure and not spin forever on a row that no longer exists")
}

// TestLinkedItemRepository_MarkPlaidRemoved_Integration_EmptyIDsIsNoop mirrors
// TestLinkedItemRepository_MarkItemsDisconnected_EmptyIDsIsNoop: an empty ids must not error and must
// not touch any row. Without the early return, `id = ANY('{}')` matches nothing and the RowsAffected
// check would turn "nothing to do" into the sentinel — which cleanupItems would then log as an
// anomaly on every pass that removed no items.
func TestLinkedItemRepository_MarkPlaidRemoved_Integration_EmptyIDsIsNoop(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, nil))
	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{}))

	var stamped bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at IS NOT NULL FROM linked_items WHERE id = $1`, item.ID).Scan(&stamped))
	assert.False(t, stamped, "an empty ids must leave every row untouched")
}
