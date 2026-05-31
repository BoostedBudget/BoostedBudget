package repository_test

// Issue #1087 GAP-3: Server-side idempotency key storage
// Integration tests verify idempotency key uniqueness + caching behavior.
// Uses pgxmock for unit-level coverage; DB integration tests go in
// integration_test.go files (pgxmock catches logic, not SQL type errors).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdempotencyRepository_DeleteExpired(t *testing.T) {
	ctx := context.Background()

	t.Run("R4-7: deletes rows past expires_at and returns count", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("DELETE FROM idempotency_keys").
			WillReturnResult(pgxmock.NewResult("DELETE", 3))

		repo := repository.NewIdempotencyRepository(mock)
		deleted, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), deleted)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestIdempotencyRepository_GetOrCreate(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	opID := "createBudget"
	key := "idem-key-abc"
	cachedBody := map[string]interface{}{"id": "budget-1", "name": "My Budget"}
	bodyJSON, _ := json.Marshal(cachedBody)

	t.Run("returns cached response when key already exists", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// R8-4: advisory lock Exec fires first; swallowed via _ = lockErr so pgxmock returns
		// an error for the unregistered Exec — but the test still passes because lockErr is
		// intentionally non-fatal. Register the expectation to make coverage explicit.
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Simulate: key found in DB
		mock.ExpectQuery("SELECT response_body FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnRows(pgxmock.NewRows([]string{"response_body"}).
				AddRow(bodyJSON))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}
		called := false

		_, err = repo.GetOrCreate(ctx, key, userID, opID, func() (interface{}, error) {
			called = true
			return nil, nil
		}, &result)

		require.NoError(t, err)
		assert.False(t, called, "fn must not be called when key already exists")
		assert.Equal(t, "budget-1", result["id"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("calls fn and stores response when key does not exist", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// R8-4: advisory lock Exec fires first
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Simulate: key not found
		mock.ExpectQuery("SELECT response_body FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnError(errors.New("no rows in result set"))

		// Simulate: INSERT succeeds
		mock.ExpectExec("INSERT INTO idempotency_keys").
			WithArgs(key, userID, opID, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}
		fnResult := map[string]interface{}{"id": "new-budget", "name": "New Budget"}

		_, err = repo.GetOrCreate(ctx, key, userID, opID, func() (interface{}, error) {
			return fnResult, nil
		}, &result)

		require.NoError(t, err)
		assert.Equal(t, "new-budget", result["id"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("propagates fn error without storing", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// R8-4: advisory lock Exec fires first
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Simulate: key not found
		mock.ExpectQuery("SELECT response_body FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnError(errors.New("no rows in result set"))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}

		_, err = repo.GetOrCreate(ctx, key, userID, opID, func() (interface{}, error) {
			return nil, errors.New("service error")
		}, &result)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service error")
		// INSERT must NOT be called when fn fails
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
