package repository

import (
	"context"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two source-scan assertions below pin the SQL SHAPE that #1683 Task 4 must not lose. They
// read the production files directly because the statements are embedded inside large functions
// (writeAuditLogEntry / DeleteUserCascade) whose full call paths need a real Postgres to exercise;
// the shape is what the task's acceptance criteria name, and it is exactly what a careless edit
// would silently drop.

// TestAuditLogRepository_LogInTxWithMetadata_WritesAllMetadataColumnsAndNullActor asserts the
// INSERT behind LogInTxWithMetadata names all four request-metadata columns (000003:19-26) and
// keeps user_id as the literal SQL NULL — the 40P01 lock-order invariant documented on
// LogInTx and the AuditLogRepository type. A regression to `$1` for user_id would take FOR KEY
// SHARE on a users row inside the erasure transaction and reintroduce the cross-user deadlock.
func TestAuditLogRepository_LogInTxWithMetadata_WritesAllMetadataColumnsAndNullActor(t *testing.T) {
	raw, err := os.ReadFile("audit_log_repository.go")
	require.NoError(t, err, "audit_log_repository.go must be readable from the package dir")
	src := string(raw)

	// Every `INSERT INTO audit_logs (<cols>) VALUES (<vals>)` in the file. The metadata variant is
	// the one naming all four metadata columns.
	re := regexp.MustCompile(`(?s)INSERT INTO audit_logs \(([^)]*)\)\s*VALUES\s*\(([^)]*)\)`)
	matches := re.FindAllStringSubmatch(src, -1)
	require.NotEmpty(t, matches, "no `INSERT INTO audit_logs ...` statement found in audit_log_repository.go — the scan is looking for the wrong thing")

	var metadataInsert string
	for _, m := range matches {
		cols := m[1]
		if strings.Contains(cols, "ip_address") && strings.Contains(cols, "user_agent") &&
			strings.Contains(cols, "request_id") && strings.Contains(cols, "changes") {
			metadataInsert = m[0]
			break
		}
	}
	require.NotEmpty(t, metadataInsert,
		"no INSERT INTO audit_logs names all four metadata columns (ip_address, user_agent, request_id, changes) — LogInTxWithMetadata is missing or does not write the metadata columns")
	assert.Contains(t, metadataInsert, "VALUES (NULL,",
		"user_id must be the literal SQL NULL in the metadata INSERT (40P01 invariant); got: %s", metadataInsert)
}

// TestDeleteUserCascade_AuditLogRedactionNullsNetworkIdentifiers asserts the erasure redaction in
// profile_repository.go nulls the network identifiers as well as the payload. `changes` alone is
// not enough: a row keyed by resource_id (= the erased subject's id) would otherwise retain
// ip_address/user_agent forever, which is personal data the erasure is required to remove.
func TestDeleteUserCascade_AuditLogRedactionNullsNetworkIdentifiers(t *testing.T) {
	raw, err := os.ReadFile("profile_repository.go")
	require.NoError(t, err, "profile_repository.go must be readable from the package dir")
	src := string(raw)

	// Capture the whole SET/WHERE clause, up to the closing backtick of the raw string literal.
	re := regexp.MustCompile("(?s)UPDATE audit_logs SET (.*?)`")
	m := re.FindStringSubmatch(src)
	require.NotEmpty(t, m, "no `UPDATE audit_logs SET ...` statement found in profile_repository.go — the redaction statement was renamed or removed")

	setClause := m[1]
	assert.Contains(t, setClause, "changes = NULL",
		"redaction must still null the PII payload `changes`; got SET clause: %q", setClause)
	assert.Contains(t, setClause, "ip_address = NULL",
		"redaction must null `ip_address` — a network identifier must not survive erasure on a resource_id-keyed row; got SET clause: %q", setClause)
	assert.Contains(t, setClause, "user_agent = NULL",
		"redaction must null `user_agent` — a network identifier must not survive erasure on a resource_id-keyed row; got SET clause: %q", setClause)
	assert.Contains(t, setClause, "WHERE resource_id = $1 OR user_id = $1",
		"redaction must keep reaching both the subject (resource_id) and the actor (user_id) keyed rows; got: %q", setClause)
}

// TestAuditLogRepository_LogInTxWithMetadata_WritesMetadata drives the real method through pgxmock
// and pins the full INSERT text plus every bound argument, including the nullable ones.
func TestAuditLogRepository_LogInTxWithMetadata_WritesMetadata(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewAuditLogMetadataRepository(mock)
	ctx := context.Background()

	const insertSQL = `INSERT INTO audit_logs (user_id, action, resource_type, resource_id, ip_address, user_agent, request_id, changes) VALUES (NULL, $1, $2, $3, $4, $5, $6, $7)`

	action := "privacy_settings.updated"
	resourceType := "privacy_settings"
	resourceID := uuid.New().String()
	ip := netip.MustParseAddr("203.0.113.7")
	ua := "BoostedBudget-Test/1.0"
	requestID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	changes := []byte(`{"gdpr_analytics_consent":true}`)

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec(regexp.QuoteMeta(insertSQL)).
		WithArgs(action, resourceType, resourceID, ip, ua, requestID, changes).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.LogInTxWithMetadata(ctx, tx, action, resourceType, resourceID, AuditLogMetadata{
		IP:        ip,
		UserAgent: ua,
		RequestID: requestID,
		Changes:   changes,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAuditLogRepository_LogInTxWithMetadata_NullsAbsentMetadata pins that an invalid IP, a
// uuid.Nil request id and a nil changes payload are bound as SQL NULL rather than as a zero value
// that would fail the INET/UUID column or fabricate metadata the request never carried.
func TestAuditLogRepository_LogInTxWithMetadata_NullsAbsentMetadata(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewAuditLogMetadataRepository(mock)
	ctx := context.Background()

	const insertSQL = `INSERT INTO audit_logs (user_id, action, resource_type, resource_id, ip_address, user_agent, request_id, changes) VALUES (NULL, $1, $2, $3, $4, $5, $6, $7)`

	action := "privacy_settings.updated"
	resourceType := "privacy_settings"
	resourceID := uuid.New().String()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	// The typed nil slice must be bound as NULL, so the expectation carries []byte(nil) — not an
	// untyped nil — because pgxmock compares with reflect.DeepEqual.
	mock.ExpectExec(regexp.QuoteMeta(insertSQL)).
		WithArgs(action, resourceType, resourceID, nil, "", nil, []byte(nil)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.LogInTxWithMetadata(ctx, tx, action, resourceType, resourceID, AuditLogMetadata{})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
