//go:build integration

package repository

// Integration test for GetOrphanedFKlessUserScopedRowCounts — Issue #1440 round-3 Finding 5's
// self-healing/observability backstop (Codex review, coordinator's #1440 follow-up brief).
//
// DeleteUserCascade's explicit erasure of dead_letter_jobs, idempotency_keys, and mcp_audit_log
// (the three user-scoped tables with NO foreign key to users at all — see that method's own doc
// comment for the full per-table rationale) runs inside the SAME transaction as the hard
// `DELETE FROM users`, immediately before commit. But because none of these three tables has any
// FK to users, the FOR UPDATE lock DeleteUserCascade holds on the users row never blocks a
// concurrent writer from inserting into any of them — an in-flight MCP audit call
// (mcp_audit.go's RecordAuditEntry), an idempotency write (idempotency_repository.go, whose
// advisory lock is per (user_id, key), unrelated to this transaction's own lock), or a River
// dead-letter insert (queue/river/dead_letter.go) can all land at any point during the
// transaction's lifetime, and a write landing after DeleteUserCascade's own delete step (however
// narrow that window now is, post-reordering) survives — silently, forever, with nothing in the
// schema left to catch it.
//
// This backstop makes that residual OBSERVABLE rather than silent: it reports, per table, how many
// rows currently reference a user_id/actor_user_id that no longer exists in `users` at all — a
// state that can ONLY arise from exactly the race this doc comment describes, never from ordinary
// operation. Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetOrphanedFKlessUserScopedRowCounts_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetOrphanedFKlessUserScopedRowCounts_Integration_DetectsOrphans seeds one row in each of the
// three FK-less tables for a real user, then deletes that user directly via SQL — bypassing
// DeleteUserCascade's own erasure entirely — to reproduce exactly the end state the race this
// backstop exists for would leave behind: a row in one of these tables whose user_id/actor_user_id
// no longer names any real user. Asserts the count for each table increases by exactly 1, isolating
// this test from whatever else may already be orphaned in a shared test database.
func TestGetOrphanedFKlessUserScopedRowCounts_Integration_DetectsOrphans(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "orphan-fkless-"+suffix, nil, nil, nil)

	before, err := repo.GetOrphanedFKlessUserScopedRowCounts(ctx)
	require.NoError(t, err)

	var dlqID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, user_id)
		VALUES (2001, 'account_deletion', 'critical', '{}'::jsonb, '[]'::jsonb, 5, NOW(), $1)
		RETURNING id`, user.ID,
	).Scan(&dlqID))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM dead_letter_jobs WHERE id = $1", dlqID) })

	execNoErr(t, ctx, pool, `
		INSERT INTO idempotency_keys (key, user_id, operation_id, response_body)
		VALUES ($1, $2, 'op-orphan', '{}'::jsonb)`, "idem-orphan-"+suffix, user.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM idempotency_keys WHERE user_id = $1", user.ID)
	})

	var auditID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO mcp_audit_log (actor_user_id, grant_id, operation_class, operation_id, result)
		VALUES ($1, 'grant-orphan', 'read', 'op-orphan', 'success') RETURNING id`, user.ID,
	).Scan(&auditID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM mcp_audit_log WHERE actor_user_id = $1", user.ID)
	})

	// Delete the user directly — bypassing DeleteUserCascade entirely — so the three rows above
	// are left pointing at a user_id/actor_user_id that no longer exists, exactly reproducing the
	// end state the race this backstop exists for would leave behind.
	_, err = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	require.NoError(t, err)

	after, err := repo.GetOrphanedFKlessUserScopedRowCounts(ctx)
	require.NoError(t, err)

	assert.Equal(t, before.DeadLetterJobs+1, after.DeadLetterJobs, "dead_letter_jobs orphan count must increase by exactly 1")
	assert.Equal(t, before.IdempotencyKeys+1, after.IdempotencyKeys, "idempotency_keys orphan count must increase by exactly 1")
	assert.Equal(t, before.MCPAuditLog+1, after.MCPAuditLog, "mcp_audit_log orphan count must increase by exactly 1")
}

// TestGetOrphanedFKlessUserScopedRowCounts_Integration_NoOrphansForLiveUser is the control case: a
// live user's own rows in these three tables must never be counted as orphaned.
func TestGetOrphanedFKlessUserScopedRowCounts_Integration_NoOrphansForLiveUser(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "live-fkless-"+suffix, nil, nil, nil)

	before, err := repo.GetOrphanedFKlessUserScopedRowCounts(ctx)
	require.NoError(t, err)

	var dlqID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, user_id)
		VALUES (2002, 'account_deletion', 'critical', '{}'::jsonb, '[]'::jsonb, 5, NOW(), $1)
		RETURNING id`, user.ID,
	).Scan(&dlqID))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM dead_letter_jobs WHERE id = $1", dlqID) })

	after, err := repo.GetOrphanedFKlessUserScopedRowCounts(ctx)
	require.NoError(t, err)

	assert.Equal(t, before.DeadLetterJobs, after.DeadLetterJobs, "a live user's own dead_letter_jobs row must never be counted as orphaned")
}
