//go:build integration

package repository

// Integration tests for the #1683 purpose columns and the locked privacy write path. They must run
// against real Postgres: pgxmock replays canned rows and cannot prove a column actually persists,
// nor that SELECT ... FOR UPDATE serializes two writers.
//
// Run with:
//
//	DATABASE_URL="postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable" \
//	  go test -tags integration -p 1 -count=1 ./internal/repository/... -run TestPrivacyRepository_Integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createPrivacyFixtureUser inserts a bare user row and returns its id. Privacy settings cascade
// from users, so deleting the user in cleanup also removes the privacy row.
func createPrivacyFixtureUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) string {
	t.Helper()
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	return createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
}

// TestPrivacyRepository_Integration_UpdateTxRoundTripsPurposeColumns persists the two #1683
// purpose columns through the transaction-scoped write and reads them back through the plain read.
func TestPrivacyRepository_Integration_UpdateTxRoundTripsPurposeColumns(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewPrivacyRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createPrivacyFixtureUser(t, ctx, pool, suffix)

	settings := domain.NewPrivacySettings(userID)
	require.NoError(t, repo.Create(ctx, settings))
	require.True(t, settings.IdentifiedDiagnosticsEnabled, "opt-out default must be true on insert")
	require.False(t, settings.GDPRInteractionCaptureConsent, "opt-in default must be false on insert")

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	locked, err := repo.GetByUserIDForUpdate(ctx, tx, userID)
	require.NoError(t, err)

	locked.GDPRAnalyticsConsent = true
	locked.GDPRInteractionCaptureConsent = true
	locked.IdentifiedDiagnosticsEnabled = false

	require.NoError(t, repo.UpdateTx(ctx, tx, locked))
	require.NoError(t, tx.Commit(ctx))

	read, err := repo.GetByUserID(ctx, userID)
	require.NoError(t, err)
	assert.True(t, read.GDPRInteractionCaptureConsent, "interaction capture must round-trip through the DB")
	assert.False(t, read.IdentifiedDiagnosticsEnabled, "identified diagnostics must round-trip through the DB")
	assert.True(t, read.GDPRAnalyticsConsent)
}

// TestPrivacyRepository_Integration_GetByUserIDForUpdateHoldsRowLock proves the FOR UPDATE lock
// serializes a concurrent read-modify-write: the second writer's locked read cannot return while
// the first transaction holds the row, and once it does return it observes the committed update.
func TestPrivacyRepository_Integration_GetByUserIDForUpdateHoldsRowLock(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewPrivacyRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createPrivacyFixtureUser(t, ctx, pool, suffix)

	settings := domain.NewPrivacySettings(userID)
	require.NoError(t, repo.Create(ctx, settings))

	firstLocked := make(chan struct{})
	secondAttempted := make(chan struct{})
	secondReturned := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	var secondRead *domain.PrivacySettings

	// First writer: take the lock, then hold it until the second writer has issued its FOR UPDATE.
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			firstDone <- err
			return
		}
		//nolint:errcheck // no-op after Commit; best-effort cleanup on the error paths.
		defer tx.Rollback(ctx)

		locked, err := repo.GetByUserIDForUpdate(ctx, tx, userID)
		if err != nil {
			firstDone <- err
			return
		}
		close(firstLocked)
		<-secondAttempted

		// The second writer has now issued its FOR UPDATE while this transaction holds the lock.
		// Without FOR UPDATE it would have returned already — fail before committing rather than
		// letting the test pass on a serialization that never happened.
		select {
		case <-secondReturned:
			firstDone <- errors.New("second locked read returned while the first transaction still held the row lock")
			return
		case <-time.After(300 * time.Millisecond):
		}

		locked.IdentifiedDiagnosticsEnabled = false
		if err := repo.UpdateTx(ctx, tx, locked); err != nil {
			firstDone <- err
			return
		}
		firstDone <- tx.Commit(ctx)
	}()

	<-firstLocked

	secondTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	//nolint:errcheck // no-op after Commit; best-effort cleanup.
	defer secondTx.Rollback(ctx)

	close(secondAttempted)
	start := time.Now()
	read, err := repo.GetByUserIDForUpdate(ctx, secondTx, userID)
	elapsed := time.Since(start)
	close(secondReturned)
	secondRead = read
	secondDone <- err

	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	require.NotNil(t, secondRead)

	// The second read could only complete after the first committed, so it must observe the
	// committed value and it must have waited for the lock.
	assert.False(t, secondRead.IdentifiedDiagnosticsEnabled,
		"the second locked read must observe the first writer's committed update")
	assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond,
		"the second FOR UPDATE must block while the first transaction holds the row lock")

	require.NoError(t, secondTx.Commit(ctx))
}
