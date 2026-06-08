package repository_test

// Issue #1087 GAP-3: Server-side idempotency key storage
// Integration tests verify idempotency key uniqueness + caching behavior.
// Uses pgxmock for unit-level coverage; DB integration tests go in
// integration_test.go files (pgxmock catches logic, not SQL type errors).
//
// GAP AW: Tests for 409 on same key reuse with a different operation_id.

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

		// Simulate: key found in DB with same operation_id (GAP AW: now SELECT includes operation_id)
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnRows(pgxmock.NewRows([]string{"response_body", "operation_id"}).
				AddRow(bodyJSON, opID))

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

		// Simulate: key not found (GAP AW: now SELECT includes operation_id)
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
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
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
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

// TestGetOrCreate_SameKeyDifferentOp_Returns409 verifies GAP AW: when the same
// idempotency key is presented with a different operation_id, ErrIdempotencyKeyConflict
// is returned and the second fn is NOT called.
func TestGetOrCreate_SameKeyDifferentOp_Returns409(t *testing.T) {
	ctx := context.Background()
	userID := "user-aw"
	key := "idem-key-aw"

	confirmBody, _ := json.Marshal(map[string]interface{}{"id": "sug-1", "status": "confirmed"})

	t.Run("GAP AW: same key different operation returns ErrIdempotencyKeyConflict", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Advisory lock
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Cache hit: key exists, but stored operation_id is "confirmTransferSuggestionMatch"
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnRows(pgxmock.NewRows([]string{"response_body", "operation_id"}).
				AddRow(confirmBody, "confirmTransferSuggestionMatch"))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}
		declineFnCalled := false

		_, err = repo.GetOrCreate(ctx, key, userID, "declineTransferSuggestionMatch", func() (interface{}, error) {
			declineFnCalled = true
			return nil, nil
		}, &result)

		assert.ErrorIs(t, err, repository.ErrIdempotencyKeyConflict, "expected ErrIdempotencyKeyConflict")
		assert.False(t, declineFnCalled, "decline fn must not be called on key conflict")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestGetOrCreate_SameKeyAndSameOp_ReturnsCachedResult is a regression test verifying
// that same key + same operation_id still produces a cache hit (unchanged behavior).
func TestGetOrCreate_SameKeyAndSameOp_ReturnsCachedResult(t *testing.T) {
	ctx := context.Background()
	userID := "user-aw2"
	key := "idem-key-aw2"
	opID := "confirmTransferSuggestionMatch"

	cachedBody := map[string]interface{}{"id": "sug-2", "status": "confirmed"}
	bodyJSON, _ := json.Marshal(cachedBody)

	t.Run("GAP AW: same key same operation returns cached result without calling fn", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Advisory lock
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Cache hit: key exists with same operation_id
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnRows(pgxmock.NewRows([]string{"response_body", "operation_id"}).
				AddRow(bodyJSON, opID))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}
		fnCalled := false

		cacheHit, err := repo.GetOrCreate(ctx, key, userID, opID, func() (interface{}, error) {
			fnCalled = true
			return nil, nil
		}, &result)

		require.NoError(t, err)
		assert.True(t, cacheHit, "expected cache hit for same key+op")
		assert.False(t, fnCalled, "fn must not be called on cache hit")
		assert.Equal(t, "sug-2", result["id"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestGetOrCreateInTx_SameKeyDifferentOp_Returns409 verifies GAP AW for the transactional path.
func TestGetOrCreateInTx_SameKeyDifferentOp_Returns409(t *testing.T) {
	ctx := context.Background()
	userID := "user-aw-tx"
	key := "idem-key-aw-tx"

	confirmBody, _ := json.Marshal(map[string]interface{}{"id": "sug-tx-1", "status": "confirmed"})

	t.Run("GAP AW: GetOrCreateInTx same key different operation returns ErrIdempotencyKeyConflict", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Begin transaction
		mock.ExpectBegin()
		tx, err := mock.Begin(ctx)
		require.NoError(t, err)

		// Advisory lock inside tx
		mock.ExpectExec("pg_advisory_xact_lock").
			WithArgs(userID, key).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))

		// Cache hit: key exists with a different operation_id
		mock.ExpectQuery("SELECT response_body, operation_id FROM idempotency_keys").
			WithArgs(key, userID).
			WillReturnRows(pgxmock.NewRows([]string{"response_body", "operation_id"}).
				AddRow(confirmBody, "confirmTransferSuggestionMatch"))

		repo := repository.NewIdempotencyRepository(mock)
		var result map[string]interface{}
		declineFnCalled := false

		_, err = repo.GetOrCreateInTx(ctx, tx, key, userID, "declineTransferSuggestionMatch", func() (interface{}, error) {
			declineFnCalled = true
			return nil, nil
		}, &result)

		assert.ErrorIs(t, err, repository.ErrIdempotencyKeyConflict, "expected ErrIdempotencyKeyConflict")
		assert.False(t, declineFnCalled, "decline fn must not be called on key conflict")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
