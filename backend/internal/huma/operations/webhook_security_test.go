package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errRecordFailed = errors.New("record failed")

// mockInvalidWebhookAttemptRepository is a mock implementation of
// repository.InvalidWebhookAttemptRepository for handler-level tests.
type mockInvalidWebhookAttemptRepository struct {
	calls []invalidAttemptCall
	err   error
}

type invalidAttemptCall struct {
	provider   string
	bodyLength int
	bodySHA256 string
}

func (m *mockInvalidWebhookAttemptRepository) Record(ctx context.Context, provider string, bodyLength int, bodySHA256 string) error {
	m.calls = append(m.calls, invalidAttemptCall{provider: provider, bodyLength: bodyLength, bodySHA256: bodySHA256})
	return m.err
}

// GetStats24h is unused by these handler-level tests (they only exercise Record via
// recordInvalidWebhookAttempt) but is required to satisfy repository.InvalidWebhookAttemptRepository.
func (m *mockInvalidWebhookAttemptRepository) GetStats24h(ctx context.Context) (map[string]int64, *time.Time, error) {
	return nil, nil, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRecordInvalidWebhookAttempt_RecordsBoundedMetadata(t *testing.T) {
	repo := &mockInvalidWebhookAttemptRepository{}
	body := []byte(`{"attacker":"controlled payload"}`)

	recordInvalidWebhookAttempt(context.Background(), repo, "paddle", body, testLogger())

	require.Len(t, repo.calls, 1)
	call := repo.calls[0]
	assert.Equal(t, "paddle", call.provider)
	assert.Equal(t, len(body), call.bodyLength)

	expectedDigest := sha256.Sum256(body)
	assert.Equal(t, hex.EncodeToString(expectedDigest[:]), call.bodySHA256)
}

func TestRecordInvalidWebhookAttempt_NilRepo_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		recordInvalidWebhookAttempt(context.Background(), nil, "plaid", []byte("x"), testLogger())
	})
}

func TestRecordInvalidWebhookAttempt_RepoErrorDoesNotPanic(t *testing.T) {
	repo := &mockInvalidWebhookAttemptRepository{err: errRecordFailed}

	assert.NotPanics(t, func() {
		recordInvalidWebhookAttempt(context.Background(), repo, "paddle", []byte("x"), testLogger())
	})
	require.Len(t, repo.calls, 1)
}
