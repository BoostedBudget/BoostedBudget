//go:build integration

package repository

// Integration tests for AuditLogRepository (Issue #1505) — the REAL implementation of
// services.AuditLogger, replacing cmd/server/main.go's mockAuditLogger, which persisted nothing.
//
// A REAL DATABASE IS REQUIRED, AND NOT AS A FORMALITY. Every assertion below is about something a
// mocked pool structurally cannot see:
//
//   - audit_logs.resource_id is UUID with NO foreign key (migration 000003:17), and
//     audit_logs.user_id is a nullable UUID WITH `FK ... ON DELETE SET NULL`. Which of the two the
//     subject goes into decides whether the row can be written AT ALL after the subject is erased.
//     pgxmock validates call shape and would pass over exactly that.
//   - The `user.deleted` entry is written after the subject's users row is gone. Had the subject
//     been put in user_id, that INSERT would fail with a foreign-key violation every single time,
//     in production. Proven below.
//
// ⚠ SINCE ISSUE #1507 THAT ENTRY IS WRITTEN INSIDE DeleteUserCascade'S TRANSACTION, via LogInTx,
// as its last statement before COMMIT — not by ExecuteDeletion after the cascade committed. What
// these tests cover is unchanged (they exercise Log against the pool directly), but the property
// they establish now also underwrites the in-transaction write: see
// profile_repository_cascade_audit_integration_test.go for the transactional half.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAuditLogRepository -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestAuditLogRepository_Integration_WritesADurableRowWithTheSubjectInResourceID(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profiles := NewProfileRepository(pool, enc)
	repo := NewAuditLogRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_auditlog_" + suffix,
		Email:              "auditlog-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profiles.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	require.NoError(t, repo.Log(ctx, "user.deletion_requested", "user", user.ID))

	var action, resourceType, status string
	var subject string
	var actor *string
	var changes *string
	err = pool.QueryRow(ctx, `
		SELECT action, resource_type, resource_id::text, user_id::text, changes::text, status
		  FROM audit_logs WHERE resource_id = $1`, user.ID).
		Scan(&action, &resourceType, &subject, &actor, &changes, &status)
	require.NoError(t, err, "exactly one durable row must exist — the stub wrote none")

	assert.Equal(t, "user.deletion_requested", action)
	assert.Equal(t, "user", resourceType)
	assert.Equal(t, user.ID, subject, "resource_id is the SUBJECT (admin_user_repository.go: 'resource_id -> the TARGET')")
	assert.Nil(t, actor, "user_id is the ACTING party; the interface carries no actor, so it stays NULL rather than being fabricated")
	assert.Nil(t, changes, "nothing is written to changes — there is nothing here for erasure to redact")
	assert.Equal(t, "success", status)
}

// THE ASSERTION THE FEATURE IS POINTLESS WITHOUT. A deletion audit entry whose subject is then hard
// deleted must survive, and — because the `user.deleted` entry is written after the `DELETE FROM
// users` that erases its subject (since #1507, in the same transaction as that DELETE) — must still
// be WRITABLE once the subject is gone.
func TestAuditLogRepository_Integration_RowOutlivesAndIsWritableAfterTheSubjectsHardDeletion(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profiles := NewProfileRepository(pool, enc)
	repo := NewAuditLogRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_auditsurv_" + suffix,
		Email:              "auditsurv-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profiles.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	// Written before the erasure, exactly as RequestDeletion does.
	require.NoError(t, repo.Log(ctx, "user.deletion_requested", "user", user.ID))

	// DeleteUserCascade's erasure redaction, verbatim, then the hard delete. Simulated with the
	// exact statements rather than by running the whole cascade, because what is under test is the
	// redaction's and the DELETE's effect on THESE rows, not the cascade.
	_, err = pool.Exec(ctx, `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, user.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
	require.NoError(t, err)

	// The `user.deleted` entry is written HERE — after the subject no longer exists.
	// With the subject in resource_id (no FK) this succeeds. With the subject in user_id it would be
	// a foreign-key violation, i.e. the audit trail's single most important entry would never be
	// written at all.
	require.NoError(t, repo.Log(ctx, "user.deleted", "user", user.ID),
		"the terminal audit entry must be writable after the subject row is gone")

	rows, err := pool.Query(ctx, `SELECT action FROM audit_logs WHERE resource_id = $1 ORDER BY created_at`, user.ID)
	require.NoError(t, err)
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		actions = append(actions, a)
	}
	require.NoError(t, rows.Err())
	assert.ElementsMatch(t, []string{"user.deletion_requested", "user.deleted"}, actions,
		"both entries survive the erasure of the person they describe — that is the whole point of the record")
}

// A FAILED AUDIT WRITE MUST NOT SILENTLY SUCCEED. The stub returned nil unconditionally; the real
// implementation must surface the database's refusal so the call site can log it.
func TestAuditLogRepository_Integration_RejectedWriteReturnsAnError(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewAuditLogRepository(pool)
	ctx := context.Background()

	const sentinel = "00000000-0000-0000-0000-000000000001"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM audit_logs WHERE resource_id = $1 OR resource_id IS NULL", sentinel)
	})

	// resource_id is UUID: a non-UUID subject is rejected by the server, not by the query text —
	// which is precisely why this is an integration test.
	err := repo.Log(ctx, "user.deleted", "user", "not-a-uuid")
	require.Error(t, err, "a write the database refused must never be reported as a success")

	// A missing subject is refused before the statement is issued: a `user.deleted` row that names
	// nobody looks like an audit trail while recording nothing.
	require.Error(t, repo.Log(ctx, "user.deleted", "user", ""))

	// So is a missing action or resource type — a row whose action is the empty string is
	// indistinguishable from noise in the operator query that audit_logs exists to serve.
	require.Error(t, repo.Log(ctx, "", "user", sentinel))
	require.Error(t, repo.Log(ctx, "user.deleted", "", sentinel))

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 OR resource_id IS NULL`, sentinel).Scan(&rows))
	assert.Zero(t, rows, "a refused write must leave no row at all")
}
