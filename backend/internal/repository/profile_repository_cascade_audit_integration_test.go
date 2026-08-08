//go:build integration

package repository

// Integration tests for Issue #1507 — the `user.deleted` audit entry is now written INSIDE
// DeleteUserCascade's own transaction, so it commits or rolls back with the erasure it describes.
//
// Before this, ExecuteDeletion wrote that entry as a separate statement AFTER the cascade had
// committed, and swallowed any failure into a log line: the deletion happened and nothing durable
// said so. That is the exact inversion of what an audit trail is for.
//
// A REAL DATABASE IS REQUIRED, AND NOT AS A FORMALITY. Every assertion below is about transactional
// semantics — whether one statement's failure un-does another statement's committed effect — which
// pgxmock cannot represent at all: it validates the SHAPE of calls, and a rollback is precisely the
// case where the calls all happened and none of their effects did.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeleteUserCascade_Integration_Audit -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installAuditInsertPoison makes any INSERT of a `user.deleted` audit row for exactly this one
// subject raise, and removes the trigger again on cleanup.
//
// A trigger, rather than dropping the table or revoking a grant, because it is the only injection
// that is BOTH deterministic and scoped to a single fixture row: `-p 1` serialises packages but not
// the tests within one, and a table-wide failure would take out any concurrently-running sibling.
// The predicate matches on resource_id, which is a fresh UUID per fixture user.
//
// ⚠ context.Background(), never t.Context(), in the cleanup — t.Context() is already cancelled by
// the time cleanups run, so the DROP would silently no-op and leak the trigger into every later
// test in the package.
func installAuditInsertPoison(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subjectUserID, suffix string) {
	t.Helper()

	fn := "z1507_poison_" + suffix
	trg := "z1507_poison_trg_" + suffix

	_, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $poison$
		BEGIN
			IF NEW.resource_id = %s::uuid AND NEW.action = 'user.deleted' THEN
				RAISE EXCEPTION 'z3/#1507: injected audit-write failure';
			END IF;
			RETURN NEW;
		END;
		$poison$ LANGUAGE plpgsql;
	`, fn, quoteLiteralForTest(subjectUserID)))
	require.NoError(t, err, "install the audit-write poison function")

	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE TRIGGER %s BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION %s();`, trg, fn))
	require.NoError(t, err, "install the audit-write poison trigger")

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON audit_logs`, trg))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, fn))
	})
}

// quoteLiteralForTest single-quotes a value for interpolation into DDL, which cannot take bind
// parameters. Only ever applied to a UUID this test itself just generated.
func quoteLiteralForTest(s string) string {
	return "'" + s + "'"
}

// countUserDeletedAuditRows returns how many `user.deleted` rows name this subject.
func countUserDeletedAuditRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subjectUserID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action = 'user.deleted'`,
		subjectUserID).Scan(&n))
	return n
}

// TEST 1 — THE DECISION, ASSERTED ON THE STATE AND NOT ONLY ON THE ERROR.
//
// A failing audit write ABORTS the whole cascade. The choice is argued at the write site in
// profile_repository.go; what is pinned here is that the choice is REAL — that the users row and
// everything cascaded from it are still there afterwards, not merely that an error came back.
//
// This test cannot pass before #1507: today the audit write is not part of this transaction at all,
// so no failure of it can reach the cascade, and the user is erased regardless.
func TestDeleteUserCascade_Integration_AuditWriteFailureAbortsTheWholeCascade(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
	})

	installAuditInsertPoison(t, ctx, pool, user.ID, suffix)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)

	require.Error(t, err, "a refused audit write must be reported, not swallowed — the trail is the point")
	assert.False(t, deleted, "a cascade that could not record itself must not report success")
	assert.Contains(t, err.Error(), "audit",
		"the error must name the audit write, so an operator is not left guessing which statement failed")

	// THE ASSERTION THE ERROR ALONE DOES NOT MAKE. Both halves rolled back together: the user is
	// still there, so the erasure can simply be retried on the next scheduled run.
	assertUserRowExists(t, ctx, pool, user.ID)
	assert.Zero(t, countUserDeletedAuditRows(t, ctx, pool, user.ID),
		"a rolled-back cascade must leave no `user.deleted` row claiming an erasure that did not happen")
}

// TEST 2 — THE MANDATORY SIBLING. Without it, TEST 1 passes just as well if the audit write is
// broken outright and never produces a row under any circumstances.
//
// An ordinary, uneventful deletion must still write EXACTLY ONE durable `user.deleted` row. "Exactly
// one" is asserted, not "at least one": moving the write into the transaction without removing
// ExecuteDeletion's post-commit call would produce two, and a duplicated erasure record is its own
// defect in a tamper-evident trail.
func TestDeleteUserCascade_Integration_AuditEntryIsWrittenExactlyOncePerDeletion(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
	})

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted, "sanity: an ordinary eligible user is still deleted")

	assert.Equal(t, 1, countUserDeletedAuditRows(t, ctx, pool, user.ID),
		"exactly one durable `user.deleted` row — not zero (no record) and not two (the write left in both places)")

	var actor, changes *string
	var status string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT user_id::text, changes::text, status
		  FROM audit_logs WHERE resource_id = $1 AND action = 'user.deleted'`, user.ID).
		Scan(&actor, &changes, &status))
	assert.Nil(t, actor, "user_id (the ACTOR) stays NULL — see LogInTx's #1506 warning; this is what keeps the INSERT free of an FK check on users")
	assert.Nil(t, changes, "nothing is written to changes; the cascade's own redaction would null it anyway")
	assert.Equal(t, "success", status)
}

// TEST 3 — THE EXISTING PROPERTY, RE-PINNED AT ITS NEW WRITE SITE.
//
// The row must outlive the hard deletion of the person it describes. That was previously guaranteed
// trivially, because the write happened after the cascade had committed. It is no longer trivial:
// the INSERT now runs in the SAME transaction as `DELETE FROM users`, and the row's durability
// depends entirely on user_id being NULL — audit_logs.user_id is an FK to users (migration 000003),
// and resource_id has none at all.
//
// The second half of this test is the load-bearing one: it shows what the NULL actor is buying, by
// proving the same INSERT with the subject in user_id is a foreign-key violation. That is also the
// mechanism behind LogInTx's #1506 warning — a non-NULL actor makes this INSERT take FOR KEY SHARE
// on a users row, which is what reintroduces the 40P01 cycle.
func TestDeleteUserCascade_Integration_AuditEntrySurvivesItsSubjectsHardDeletion(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
	})

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	// The subject is gone, committed, and the record of its erasure is still here.
	assertUserRowGone(t, ctx, pool, user.ID)
	var action, resourceType string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT action, resource_type FROM audit_logs WHERE resource_id = $1 AND action = 'user.deleted'`,
		user.ID).Scan(&action, &resourceType),
		"the erasure record must outlive the erasure — writing it inside the transaction must not have made it depend on the row being deleted")
	assert.Equal(t, "user.deleted", action)
	assert.Equal(t, "user", resourceType)

	// WHY user_id STAYS NULL, demonstrated rather than asserted in prose.
	_, err = pool.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES ($1, 'user.deleted', 'user', $1)`,
		user.ID)
	require.Error(t, err,
		"the same entry with the SUBJECT in user_id is an FK violation once the subject is erased — the NULL actor is load-bearing, not a stylistic choice (#1506)")
}
