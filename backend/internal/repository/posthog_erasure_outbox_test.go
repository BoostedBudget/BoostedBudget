package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reversibleTestEncryption struct{}

func (reversibleTestEncryption) Encrypt(value string) (string, error) {
	return base64.RawStdEncoding.EncodeToString([]byte(value)), nil
}

func TestOperatorCompletionRequiresTwoSeparatedZeroConfirmations(t *testing.T) {
	raw, err := os.ReadFile("posthog_erasure_outbox.go")
	require.NoError(t, err)
	source := string(raw)
	assert.Contains(t, source, "clock_timestamp()-interval '5 minutes'")
	assert.NotContains(t, source, "verifiedAt time.Time")
}

func TestClaimDueDeadLettersUndecryptableIdentityEnvelope(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewPostHogErasureOutboxRepository(mock, enc)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("WITH candidate AS").WithArgs(now, pgxmock.AnyArg(), "1m0s").WillReturnRows(pgxmock.NewRows([]string{
		"id", "identities_encrypted", "provider_person_uuids_encrypted", "provider_deletion_baseline_encrypted", "project_id", "region", "phase", "attempt_count",
		"resolution_cursor", "baseline_cursor", "poll_cursor", "requires_manual_event_verification", "provider_request_started_at", "accepted_at",
	}).AddRow("row-id", "ENC$not-base64!", nil, nil, "42", "us", "person_resolve", 1, 0, 0, 0, false, nil, nil))
	mock.ExpectExec("SET state='dead_letter'").
		WithArgs("row-id", pgxmock.AnyArg(), "identity_decryption").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	record, err := repo.ClaimDue(context.Background(), now, time.Minute)
	require.ErrorContains(t, err, "identity_decryption")
	assert.Nil(t, record)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompleteErasureClearsAllRecoverableProviderIdentityCiphertext(t *testing.T) {
	raw, err := os.ReadFile("posthog_erasure_outbox.go")
	require.NoError(t, err)
	source := string(raw)
	assert.Contains(t, source, "identities_encrypted=NULL")
	assert.Contains(t, source, "provider_person_uuids_encrypted=NULL")
	assert.Contains(t, source, "provider_deletion_baseline_encrypted=NULL")
}

func TestRecordRetryReturnsDeadLetterBoundaryState(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewPostHogErasureOutboxRepository(mock, nil)
	mock.ExpectQuery("UPDATE posthog_erasure_outbox").WithArgs("row", "lease", "provider", pgxmock.AnyArg(), 12).WillReturnRows(pgxmock.NewRows([]string{"state"}).AddRow("dead_letter"))
	state, err := repo.RecordRetry(context.Background(), "row", "lease", "provider", time.Now(), 12)
	require.NoError(t, err)
	assert.Equal(t, "dead_letter", state)
}

func TestOperatorEvidenceUsesDatabaseClockAndBoundedSurface(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewPostHogErasureOutboxRepository(mock, enc)
	mock.ExpectBegin()
	mock.ExpectQuery("clock_timestamp").WithArgs("row", "provider_zero").
		WillReturnRows(pgxmock.NewRows([]string{"state"}).AddRow("manual_pending"))
	mock.ExpectExec("INSERT INTO posthog_erasure_operator_audit").WithArgs("row", pgxmock.AnyArg(), "recordings", "provider_zero").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("state='completed'").WithArgs("row").WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()
	_, err = repo.RecordOperatorEvidence(context.Background(), "row", "admin-uuid", "recordings", "provider_zero")
	require.NoError(t, err)
}

func TestRequeueDeadLetterIsAuditedAndPreservesAttemptHistory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewPostHogErasureOutboxRepository(mock, enc)
	mock.ExpectBegin()
	mock.ExpectExec("state='retrying'").WithArgs("row").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("posthog_erasure_operator_audit").WithArgs("row", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	ok, err := repo.RequeueDeadLetter(context.Background(), "row", "admin")
	require.NoError(t, err)
	assert.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRetargetAcceptsEitherMissingComponentAndAuditsActualPriorTarget(t *testing.T) {
	raw, err := os.ReadFile("posthog_erasure_outbox.go")
	require.NoError(t, err)
	source := string(raw)
	assert.Contains(t, source, "project_id='unconfigured' OR region='unconfigured'")
	assert.Contains(t, source, "from_project_id,from_region")
	assert.NotContains(t, source, "VALUES ($1,$2,'retarget','unconfigured','unconfigured'")
}

func TestListOutstandingExplicitlyIdentifiesRequiredManualSurfaces(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewPostHogErasureOutboxRepository(mock, nil)
	requestedAt := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT id,project_id,region,state,phase,requested_at").WithArgs(50).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "project_id", "region", "state", "phase", "requested_at", "requires_manual_event_verification",
		}).
			AddRow("fully-correlated", "42", "us", "manual_pending", "support_manual", requestedAt, false).
			AddRow("mixed-or-ambiguous", "42", "us", "manual_pending", "support_manual", requestedAt, true))

	rows, err := repo.ListOutstanding(context.Background(), 50)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"recordings", "surveys", "support"}, rows[0].RequiredManualSurfaces)
	assert.Equal(t, []string{"events", "recordings", "surveys", "support"}, rows[1].RequiredManualSurfaces)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolutionCheckpointResetsFailureBudgetForNextIdentity(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewPostHogErasureOutboxRepository(mock, enc)
	mock.ExpectExec("attempt_count=0").
		WithArgs("row", "lease", pgxmock.AnyArg(), false, 1, 1000, true).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	updated, err := repo.MarkPersonUUIDsResolved(context.Background(), "row", "lease", []string{"11111111-1111-4111-8111-111111111111"}, false, 1, 1000)
	require.NoError(t, err)
	assert.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteUserCascadeCreatesPostHogOutboxBeforeDeletingIdentity(t *testing.T) {
	raw, err := os.ReadFile("profile_repository.go")
	require.NoError(t, err)
	source := string(raw)
	start := strings.Index(source, "func (r *profileRepository) DeleteUserCascade")
	require.NotEqual(t, -1, start)
	body := source[start:]
	identityRead := strings.Index(body, "SELECT clerk_user_id FROM users")
	outboxInsert := strings.Index(body, "INSERT INTO posthog_erasure_outbox")
	userDelete := strings.Index(body, "tx.Exec(ctx, `DELETE FROM users WHERE id = $1`")
	require.Greater(t, identityRead, -1)
	require.Greater(t, outboxInsert, identityRead)
	require.Greater(t, userDelete, outboxInsert)
	assert.Contains(t, body[:userDelete], "buildPostHogErasureEnvelope")
}
func (reversibleTestEncryption) Decrypt(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	return string(raw), err
}

func TestBuildPostHogErasureEnvelopeIncludesCurrentHistoricalAndLegacyInternalIDs(t *testing.T) {
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)

	envelope, err := buildPostHogErasureEnvelope(enc, "internal-uuid", []string{
		"ENC$Y2xlcmstY3VycmVudA", "ENC$Y2xlcmstaGlzdG9yaWNhbA", "ENC$Y2xlcmstY3VycmVudA",
	})
	require.NoError(t, err)
	assert.NotContains(t, envelope.Ciphertext, "clerk-current")
	assert.NotContains(t, envelope.IdempotencyKey, "internal-uuid")

	raw, err := enc.DecryptJSON(envelope.Ciphertext)
	require.NoError(t, err)
	var identities []string
	require.NoError(t, json.Unmarshal(raw, &identities))
	assert.ElementsMatch(t, []string{"internal-uuid", "clerk-current", "clerk-historical"}, identities)
}
