//go:build integration

package repository

// Integration tests for round 15's Tier A change to the deletion-residue mechanism.
//
// TWO THINGS ARE PROVED HERE, AND NEITHER CAN BE PROVED ANYWHERE ELSE:
//
//  1. THE NEW EFFECT NEEDS NO MIGRATION. DeletionResiduePaddleCustomerArchived was added to the
//     vocabulary for finding A1. Whether the existing namespace picks it up — the operator query's
//     `action LIKE 'user.deletion_residue.%'`, migration 000138's partial index, and both Prometheus
//     gauges — is a claim about a real Postgres and a real index, not about Go. It was verified, not
//     assumed.
//
//  2. A COMPLETED DELETION RETRACTS ITS OWN RESIDUE RECORDS, ATOMICALLY. Since round 15 each
//     irreversible external Paddle call records itself BEFORE it is issued, which is the only form of
//     the record that survives a lost acknowledgement, a panic, or process death. That is only safe
//     because a deletion that COMPLETES removes those rows inside the same transaction that removes
//     the user. pgxmock cannot express "these two statements committed together"; that is the entire
//     property.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration -p 1 -count=1 \
//	  -run 'TestDeleteUserCascade_Integration_Residue|TestRecordDeletionResidue_Integration_CustomerArchived' -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countResidueRows returns how many rows in the residue NAMESPACE name this subject. The pattern is
// spelled out here rather than taken from deletionResidueActionPrefix on purpose: this is the
// independent enumeration the production constant is being checked against, and sharing the constant
// would make a change to it invisible to this test.
//
// ⚠ context.Background(), never t.Context() — see the cleanup note on every fixture below.
func countResidueRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subjectUserID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action LIKE 'user.deletion_residue.%'`,
		subjectUserID).Scan(&n))
	return n
}

// TestRecordDeletionResidue_Integration_CustomerArchivedNeedsNoMigration is the "verify, do not
// assume" check the round-15 brief asked for by name: does the ALREADY-DEPLOYED reading surface pick
// up the new effect with no schema change?
//
// ⚠ THE ASSERTIONS ARE MEMBERSHIP, NOT A DELTA. GetDeletionResidueStats scans the whole audit_logs
// table (it is a health gauge, not a per-test query), so a delta over a measured baseline is blind to
// any mutation that moves the baseline and the result by the same amount — predicate WIDENING is
// exactly that class, and it has already passed a delta-based test in this repository once. So the
// row is pinned by exact action equality, computed here, and the gauge is pinned against an
// independent enumeration of the same population.
func TestRecordDeletionResidue_Integration_CustomerArchivedNeedsNoMigration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewProfileRepository(pool, newTestFieldEncryptor(t))
	health := NewJobHealthRepository(pool)
	ctx := context.Background()

	userID := createResidueStatsUser(t, pool, "archived")

	require.NoError(t, repo.RecordDeletionResidue(ctx, userID, DeletionResiduePaddleCustomerArchived))

	// 1. THE ACTION VALUE ITSELF. audit_logs.action is VARCHAR(100); a value that overflowed would be
	// rejected by the server and by nothing else.
	var action string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT action FROM audit_logs WHERE resource_id = $1`, userID).Scan(&action))
	assert.Equal(t, "user.deletion_residue.paddle_customer_archived", action)

	// 2. THE NAMESPACE, spelled as the operator query and migration 000138's index predicate spell it.
	// This is the claim "no migration is needed" reduces to.
	assert.Equal(t, 1, countResidueRows(t, ctx, pool, userID),
		"the new effect must answer to the SAME namespace the operator query and the partial index are written against")

	// 3. THE PARTIAL INDEX REALLY COVERS IT. Asked of Postgres rather than reasoned about: an index
	// whose predicate did not imply this row's action would report zero here even though the row
	// exists, which is precisely the failure a new namespace would cause.
	var indexedRows int
	require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'user.deletion_residue.%%' AND resource_id = %s::uuid`,
		quoteLiteralForTest(userID))).Scan(&indexedRows))
	require.Equal(t, 1, indexedRows)

	// 4. THE GAUGE COUNTS IT. Membership: the surface's count must equal an independent count of the
	// same population computed by this test, not "baseline plus one".
	gaugeCount, _, err := health.GetDeletionResidueStats(ctx)
	require.NoError(t, err)

	var independentCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'user.deletion_residue.%'`).Scan(&independentCount))
	assert.Equal(t, independentCount, gaugeCount,
		"the gauge and an independent enumeration of the namespace must agree once the new effect is in it")
	assert.GreaterOrEqual(t, gaugeCount, 1, "sanity: the row this test just wrote is in that population")
}

// TestDeleteUserCascade_Integration_ResidueRecordsAreRetractedByACompletedDeletion is the property
// that makes write-ahead recording safe. Without it, every ordinary deletion of a paying user would
// leave a permanent row behind and both Prometheus gauges would alert on it.
//
// ⚠ IT ALSO PINS THE SCOPE, which is the part most likely to rot: only THIS user's rows, and only the
// residue namespace. A DELETE that dropped the whole namespace would destroy another user's live work
// item; one that dropped this user's whole audit history would destroy the GDPR trail the redaction
// two statements earlier exists to preserve. Both are seeded here and both are asserted.
func TestDeleteUserCascade_Integration_ResidueRecordsAreRetractedByACompletedDeletion(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewProfileRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-31 * 24 * time.Hour)
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, nil, nil)
	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// which would leave these rows behind to break a different test on the shared database.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
	})

	// The two effects a real run records as it issues them.
	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleSubscriptionCancelled))
	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleCustomerArchived))
	require.Equal(t, 2, countResidueRows(t, ctx, pool, user.ID))

	// A DIFFERENT user's live residue record — the one a namespace-wide DELETE would destroy.
	bystanderID := createResidueStatsUser(t, pool, "bystander")
	require.NoError(t, repo.RecordDeletionResidue(ctx, bystanderID, DeletionResiduePaddleSubscriptionCancelled))

	// ...and an ordinary, non-residue audit row for the SUBJECT, which the GDPR trail keeps.
	_, err := pool.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, 'user.tier_changed', 'user', $1)`,
		user.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted, "sanity: an ordinary eligible user is still deleted")

	assert.Zero(t, countResidueRows(t, ctx, pool, user.ID),
		"a deletion that COMPLETED must retract its own residue records — the person the record is about no longer exists")
	assert.Equal(t, 1, countResidueRows(t, ctx, pool, bystanderID),
		"another user's work item must be untouched — the retraction is scoped to the subject")

	var keptTrailRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action = 'user.tier_changed'`,
		user.ID).Scan(&keptTrailRows))
	assert.Equal(t, 1, keptTrailRows,
		"the retraction is scoped to the residue namespace — the tamper-evident trail is redacted, never deleted")
}

// ⚠ SIBLING, MANDATORY. Without it the retraction above is satisfied just as well by a DELETE that
// runs unconditionally, outside the transaction, or on every call — which would erase the operator's
// work item on exactly the runs that created it. A cascade that REFUSES must leave the records
// standing, because that is the case they exist for.
//
// The refusal is produced the way production produces it: an expected deletion_requested_at that no
// longer matches the row (the user cancelled their deletion mid-flight), which is DeleteUserCascade's
// own in-transaction eligibility re-check returning (false, nil).
func TestDeleteUserCascade_Integration_ResidueRecordsSurviveARefusedCascade(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewProfileRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-31 * 24 * time.Hour)
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, nil, nil)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
	})

	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleSubscriptionCancelled))
	require.NoError(t, repo.RecordDeletionResidue(ctx, user.ID, DeletionResiduePaddleCustomerArchived))

	// The user cancelled: the observed occurrence no longer matches the row.
	stale := requestedAt.Add(-time.Hour)
	deleted, err := repo.DeleteUserCascade(ctx, user.ID, &stale)
	require.NoError(t, err, "a refusal is a normal, expected outcome — not an error")
	require.False(t, deleted)

	assertUserRowExists(t, ctx, pool, user.ID)
	assert.Equal(t, 2, countResidueRows(t, ctx, pool, user.ID),
		"the user survived with irreversible external work already done to them — that is exactly when the record must stay")
}
