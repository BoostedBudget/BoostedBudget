package river

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// motivatingError reconstructs the exact production failure from issue #1631: a holdings sync
// that dead-lettered five times, where every layer of context was flattened into one string and
// the PgError's Detail — the answer to the whole investigation — never reached the admin.
func motivatingError() error {
	pgErr := &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "unique_account_security"`,
		Detail:         "Key (account_id, security_id)=(ae944376-3d01-41eb-bc3e-f275a3c36c02, 7c1f0b2e-1111-4444-8888-999999999999) already exists.",
		Hint:           "",
		SchemaName:     "public",
		TableName:      "holdings",
		ConstraintName: "unique_account_security",
		Routine:        "_bt_check_unique",
	}
	inner := fmt.Errorf("failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02: %w", pgErr)
	mid := fmt.Errorf("investment sync failed: %w", inner)
	return fmt.Errorf("webhook processing failed: %w", mid)
}

func TestPgErrorContext_ExtractsEveryFieldPostgresSent(t *testing.T) {
	got := pgErrorContext(motivatingError())

	require.NotNil(t, got, "a wrapped *pgconn.PgError must be found through the chain, not by string match")
	assert.Equal(t, "23505", got.Code)
	assert.Equal(t, "unique_account_security", got.ConstraintName)
	assert.Equal(t, "holdings", got.TableName)
	assert.Equal(t, "public", got.SchemaName)
	assert.Equal(t, "ERROR", got.Severity)
	assert.Equal(t, "_bt_check_unique", got.Routine)
	assert.Contains(t, got.Detail, "Key (account_id, security_id)=")
	assert.Equal(t, `duplicate key value violates unique constraint "unique_account_security"`, got.Message)
}

func TestPgErrorContext_NilForNonPgError(t *testing.T) {
	assert.Nil(t, pgErrorContext(errors.New("plain failure")))
	assert.Nil(t, pgErrorContext(nil))
}

func TestErrorChain_KeepsEachWrappingLayerSeparate(t *testing.T) {
	chain := errorChain(motivatingError())

	require.Len(t, chain, 4, "each fmt.Errorf boundary plus the PgError is its own entry")
	assert.Equal(t, "webhook processing failed", chain[0])
	assert.Equal(t, "investment sync failed", chain[1])
	assert.Equal(t, "failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02", chain[2])
	assert.Contains(t, chain[3], "duplicate key value violates unique constraint")
}

func TestErrorChain_SingleErrorIsOneEntry(t *testing.T) {
	assert.Equal(t, []string{"boom"}, errorChain(errors.New("boom")))
	assert.Nil(t, errorChain(nil))
}

func TestErrorChain_JoinedErrorsAreAllRecorded(t *testing.T) {
	chain := errorChain(errors.Join(errors.New("left"), errors.New("right")))
	assert.Contains(t, chain, "left")
	assert.Contains(t, chain, "right")
}

func TestBuildErrorHistory_EnrichesTheCurrentAttemptWithoutChangingMessage(t *testing.T) {
	now := time.Now()
	job := &rivertype.JobRow{
		ID: 1, Kind: "sync_investments", Attempt: 2, MaxAttempts: 2,
		Errors: []rivertype.AttemptError{
			{Attempt: 1, Error: "first error", At: now.Add(-time.Hour)},
		},
	}

	history := buildErrorHistory(job, motivatingError())
	require.Len(t, history, 2)

	// Historical attempts arrive from River as strings only; nothing to enrich and nothing lost.
	assert.Equal(t, "first error", history[0].Message)
	assert.Nil(t, history[0].PgError)
	assert.Nil(t, history[0].Chain)

	current := history[1]
	// The existing flattened message is preserved byte-for-byte — old consumers keep working.
	assert.Equal(t, motivatingError().Error(), current.Message)
	require.NotNil(t, current.PgError)
	assert.Equal(t, "23505", current.PgError.Code)
	assert.Equal(t, "unique_account_security", current.PgError.ConstraintName)
	require.Len(t, current.Chain, 4)
}

// The column is an existing JSON column with production rows in the OLD
// {attempt, message, timestamp} shape. They must decode with the new fields simply absent.
func TestJobError_DecodesOldProductionShape(t *testing.T) {
	oldShape := []byte(`[{"attempt":1,"message":"webhook processing failed: boom","timestamp":"2026-04-01T11:00:00Z"}]`)

	var decoded []domain.JobError
	require.NoError(t, json.Unmarshal(oldShape, &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, 1, decoded[0].Attempt)
	assert.Equal(t, "webhook processing failed: boom", decoded[0].Message)
	assert.Nil(t, decoded[0].Chain)
	assert.Nil(t, decoded[0].PgError)
}

// And a JobError with no structured context must not write the new keys at all, so rows written
// for non-Postgres failures stay byte-identical to what the old code produced.
func TestJobError_OmitsNewKeysWhenAbsent(t *testing.T) {
	out, err := json.Marshal(domain.JobError{Attempt: 1, Message: "boom", Timestamp: time.Unix(0, 0).UTC()})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "chain")
	assert.NotContains(t, string(out), "pg_error")
}
