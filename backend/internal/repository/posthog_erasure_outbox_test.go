package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewPostHogErasureOutboxRepository(mock, nil)
	first := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("SET first_zero_verified_at=$2")).
		WithArgs("row-id", first).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	recorded, err := repo.RecordFirstZeroVerification(context.Background(), "row-id", first)
	require.NoError(t, err)
	require.True(t, recorded)

	mock.ExpectExec(regexp.QuoteMeta("first_zero_verified_at <= $2 - interval '5 minutes'")).
		WithArgs("row-id", first.Add(4*time.Minute)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	completed, err := repo.CompleteWithOperatorEvidence(context.Background(), "row-id", first.Add(4*time.Minute))
	require.NoError(t, err)
	require.False(t, completed)

	mock.ExpectExec(regexp.QuoteMeta("first_zero_verified_at <= $2 - interval '5 minutes'")).
		WithArgs("row-id", first.Add(5*time.Minute)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	completed, err = repo.CompleteWithOperatorEvidence(context.Background(), "row-id", first.Add(5*time.Minute))
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, mock.ExpectationsWereMet())
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
		"id", "identities_encrypted", "project_id", "region", "state", "phase", "attempt_count",
		"first_zero_verified_at", "person_verified_at", "events_verified_at", "recordings_verified_at",
		"surveys_verified_at", "support_verified_at",
	}).AddRow("row-id", "ENC$not-base64!", "42", "us", "pending", "person_delete", 1,
		nil, nil, nil, nil, nil, nil))
	mock.ExpectExec("SET state='dead_letter'").
		WithArgs("row-id", pgxmock.AnyArg(), "identity_decryption").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	record, err := repo.ClaimDue(context.Background(), now, time.Minute)
	require.ErrorContains(t, err, "identity_decryption")
	assert.Nil(t, record)
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
