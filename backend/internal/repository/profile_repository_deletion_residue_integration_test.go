//go:build integration

package repository

// Integration test for RecordDeletionResidue (Issue #1504) — the durable, queryable record that an
// account deletion cancelled a user's Paddle subscription and then did not complete.
//
// A REAL DATABASE IS REQUIRED, AND NOT AS A FORMALITY. Every assertion below is about something
// pgxmock structurally cannot see: audit_logs.resource_id is UUID and audit_logs.user_id is a
// nullable UUID with an FK to users, so whether a Go string parameter is accepted for the former and
// whether a NULL is accepted for the latter are facts about Postgres, not about the query text. A
// unit test with a mocked pool passes on a statement the server would reject outright.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRecordDeletionResidue -tags integration -p 1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestRecordDeletionResidue_Integration_WritesAQueryableRowThatSurvivesErasure(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_residue_" + suffix,
		Email:              "residue-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// which would leave these rows behind to break a different test on the shared database.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleSubscriptionCancelled))

	// THE OPERATOR'S QUERY, verbatim from RecordDeletionResidue's doc comment. If the prefix and the
	// effect ever drift apart, this is what notices.
	var action, resourceType string
	var actorUserID *string
	var changes *string
	err = pool.QueryRow(ctx, `
		SELECT action, resource_type, user_id::text, changes::text
		  FROM audit_logs
		 WHERE action LIKE 'user.deletion_residue.%' AND resource_id = $1`,
		user.ID).Scan(&action, &resourceType, &actorUserID, &changes)
	require.NoError(t, err, "exactly one queryable row must exist for this user")

	assert.Equal(t, "user.deletion_residue.paddle_subscription_cancelled", action)
	assert.Equal(t, "user", resourceType)
	assert.Nil(t, actorUserID, "a system action has no acting party; the FK column stays NULL")
	assert.Nil(t, changes, "nothing is written to changes — there is nothing here for erasure to redact")

	// The record survives the erasure it may outlive, with nothing to redact. DeleteUserCascade's
	// redaction is `SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, which this row
	// matches by resource_id — deliberately, since it belongs to the user. Simulated with that exact
	// statement rather than by running the whole cascade, because the point under test is the
	// redaction's effect on THIS row, not the cascade.
	_, err = pool.Exec(ctx, `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, user.ID)
	require.NoError(t, err)

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		 WHERE action = 'user.deletion_residue.paddle_subscription_cancelled' AND resource_id = $1`,
		user.ID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere,
		"the action and the user it names are what an operator queries on, and redaction does not touch either")
}

// The empty-effect refusal, proven against the real database so it is clear nothing is written when
// it fires — a row whose action reads 'user.deletion_residue.' would look like the operator had been
// told something while naming no effect at all.
func TestRecordDeletionResidue_Integration_EmptyEffectWritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_residue_empty_" + suffix,
		Email:              "residue-empty-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	require.Error(t, repo.RecordDeletionResidue(ctx, user.ID, ""))

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1", user.ID).Scan(&rows))
	assert.Zero(t, rows, "a refused effect must leave no row at all")
}

// The vocabulary widened in round 12 (finding AB-1): a cascade that fails with a non-transient error
// after the Paddle cancel has landed records the same harm under a DIFFERENT kind, so an operator can
// tell "the deletion is over, restore the subscription" from "the deletion is still stuck, fix the
// database fault too".
//
// A REAL DATABASE IS REQUIRED FOR THE PART THAT MATTERS. audit_logs.action is VARCHAR(100) and the
// new value is the longest in the vocabulary; a value that overflowed would be rejected by the server
// and by nothing else. The second assertion is the one a unit test cannot make at all: that the
// widened vocabulary is still inside the namespace the operator query and migration 000138's partial
// index are both written against.
func TestRecordDeletionResidue_Integration_CascadeFailedKindIsWrittenAndStaysInTheNamespace(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_residue_casc_" + suffix,
		Email:              "residue-casc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// which would leave these rows behind to break a different test on the shared database.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleSubscriptionCancelledCascadeFailed))

	// THE OPERATOR'S QUERY, verbatim from RecordDeletionResidue's doc comment — the same one the
	// sibling test above runs for the other kind. Both kinds must answer to it.
	var action string
	var actorUserID *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT action, user_id::text
		  FROM audit_logs
		 WHERE action LIKE 'user.deletion_residue.%' AND resource_id = $1`,
		user.ID).Scan(&action, &actorUserID))

	assert.Equal(t, "user.deletion_residue.paddle_subscription_cancelled_cascade_failed", action)
	assert.Nil(t, actorUserID, "a system action has no acting party; the FK column stays NULL")

	// ...and it is DISTINGUISHABLE from the other kind, which is the entire reason it exists. Asserted
	// as membership of this specific action value, never as a count of the namespace: a count would be
	// satisfied by either kind and would say nothing about which was written.
	var wrongKind int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		 WHERE action = 'user.deletion_residue.paddle_subscription_cancelled' AND resource_id = $1`,
		user.ID).Scan(&wrongKind))
	assert.Zero(t, wrongKind, "the two kinds must not collapse into one action value")
}
