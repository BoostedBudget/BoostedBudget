package services

// deletion_service_audit_log_integration_test.go — end-to-end proof, against a REAL Postgres
// instance, that DeletionService wired with the REAL AuditLogger (repository.AuditLogRepository,
// Issue #1506) writes a durable audit_logs row, and that the row outlives the person it describes.
//
// WHY THIS IS SEPARATE FROM THE REPOSITORY-LEVEL TESTS. Those prove the INSERT is correct.
// This proves the SERVICE is actually wired to something that persists — which is exactly what was
// wrong: cmd/server/main.go injected a stub whose Log() logged a line and returned nil, and every
// unit test in this package injected a MockAuditLogger that also returned nil. Both agreed
// perfectly, and both were wrong about the only thing that mattered. A mock cannot notice that.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestDeletionServiceAuditLog -tags integration -p 1 -count=1 -v

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestDeletionServiceAuditLog_Integration_RequestDeletionWritesADurableRowThatSurvivesErasure(t *testing.T) {
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	user := insertIntegrationTestUser(t, f.profileRepo, "auditwire-"+t.Name()+"@example.com", "premium", "active")
	// context.Background(), never t.Context(): the test context is cancelled before t.Cleanup runs,
	// so a cleanup keyed to it would silently DELETE nothing and leak rows into another test.
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, user.ID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})

	// The REAL logger — the same constructor call cmd/server/main.go now makes.
	ds := NewDeletionService(
		f.profileRepo,
		NewEmailServiceMock("", "", "https://app.example.com"),
		new(MockClerkService),
		repository.NewAuditLogRepository(f.pool),
		nil, 30, "test-secret-key",
	)
	ds.SetDB(f.pool)

	_, _, err := ds.RequestDeletion(ctx, user.ID, user.Email)
	require.NoError(t, err)

	var action, subject string
	var actor *string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT action, resource_id::text, user_id::text FROM audit_logs WHERE resource_id = $1`,
		user.ID).Scan(&action, &subject, &actor),
		"the request must leave exactly one durable row — the stub left none")
	assert.Equal(t, "user.deletion_requested", action)
	assert.Equal(t, user.ID, subject, "resource_id carries the SUBJECT")
	assert.Nil(t, actor, "the interface carries no actor, so user_id stays NULL rather than being fabricated")

	// And it survives the erasure it exists to evidence: DeleteUserCascade's redaction, then the
	// hard delete of the subject. resource_id has no FK, so nothing cascades the row away.
	_, err = f.pool.Exec(ctx, `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, user.ID)
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
	require.NoError(t, err)

	var survivors int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1 AND action = 'user.deletion_requested'`,
		user.ID).Scan(&survivors))
	assert.Equal(t, 1, survivors, "a deletion audit entry that dies with its subject records nothing")
}
