//go:build integration

package jobs

// Integration test for Issue #1413 Task 9: ManualTierOverrideExpiryCheckWorker's end-to-end
// pipeline against a live database.
//
// The mocked unit tests in manual_tier_override_expiry_check_test.go cannot observe whether
// retention actually gets re-armed or the notice flags get nulled — repository.RemoveTierOverrideRow
// carries no retention fields at all, because that logic lives entirely inside
// AdminUserRepository.RemoveUserTierOverride's SQL, already exhaustively falsified against a real
// database in Task 7's admin_user_repository_remove_tier_override_integration_test.go. Re-running
// that same matrix here (active/never-ran/read-only variants) would duplicate Task 7's coverage
// through a different call path for no new signal.
//
// What THIS test proves instead is the WIRING unique to this job: that
// GetUsersWithExpiredManualTierOverride finds a real expired-override row, the worker's guard
// lets it through, RemoveUserTierOverride is invoked with this job's own retentionMonths and a
// NULL (system) actor, and — end to end, through real SQL — the override is cleared, retention
// is re-armed with the CORRECT (non-default) retention window, both notice flags are nulled, the
// audit row lands with a NULL actor, and Clerk sync is attempted with the reverted values.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... -run TestManualTierOverrideExpiryCheckWorker_Integration -tags integration -v

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// jobsIntegrationRetentionMonths is deliberately NOT the config default (6) so an implementation
// that hardcodes 6 anywhere in the path (instead of honoring the worker's configured
// retentionMonths) would produce a detectably wrong retention_expires_at.
const jobsIntegrationRetentionMonths = 9

// stubEncryptionService is a minimal EncryptionService double for these tests — mirrors
// internal/repository's own test_helpers_test.go stub (unexported there, so re-declared here for
// this package).
type stubEncryptionService struct{}

func (s *stubEncryptionService) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *stubEncryptionService) Decrypt(ciphertext string) (string, error) {
	const prefix, suffix = "TENC[", "]"
	if len(ciphertext) < len(prefix)+len(suffix) || ciphertext[:len(prefix)] != prefix || ciphertext[len(ciphertext)-len(suffix):] != suffix {
		return ciphertext, nil // passthrough for already-plain values
	}
	return ciphertext[len(prefix) : len(ciphertext)-len(suffix)], nil
}

func connectJobsTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

func newJobsTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	key := []byte("12345678901234567890123456789012") // 32-byte AES key
	enc, err := appcrypto.NewFieldEncryptor(&stubEncryptionService{}, key)
	require.NoError(t, err)
	return enc
}

// TestManualTierOverrideExpiryCheckWorker_Integration_ExpiresAndRearmsRetention wires the REAL
// ProfileRepository + AdminUserRepository (no mocks on either side of the DB boundary) against a
// user with an expired override, a running retention clock, and a read-only reverted status
// ('cancelled') — the case that requires re-arming. Only ClerkService is mocked (no live Clerk
// sandbox in this test run), asserted to be called with the reverted (post-clear) values.
func TestManualTierOverrideExpiryCheckWorker_Integration_ExpiresAndRearmsRetention(t *testing.T) {
	pool := connectJobsTestDB(t)
	enc := newJobsTestFieldEncryptor(t)
	ctx := context.Background()

	profileRepo := repository.NewProfileRepository(pool, enc)
	adminRepo := repository.NewAdminUserRepository(pool, enc)

	suffix := fmt.Sprintf("mte_job_%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled", // read-only reverted status: retention must re-arm
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	// A retention clock that was already running (and already had notices sent — proves they
	// get NULLED on re-arm, not just left alone) but is now stale (expires in the past — the
	// exact hazard Task 9 exists to fix: comp someone longer than the retention window and the
	// clock silently lapses with no 30d/7d warning).
	longAgo := time.Now().Add(-200 * 24 * time.Hour)
	staleExpiry := time.Now().Add(-1 * time.Hour)
	noticeSentAt := time.Now().Add(-100 * 24 * time.Hour)
	overrideEndsAt := time.Now().Add(-1 * time.Hour) // expired
	household := "household_2"

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET manual_tier_override = $1,
		    manual_tier_ends_at = $2,
		    retention_started_at = $3,
		    retention_expires_at = $4,
		    retention_notice_30d_sent_at = $5,
		    retention_notice_7d_sent_at = $5
		WHERE id = $6`,
		household, overrideEndsAt, longAgo, staleExpiry, noticeSentAt, user.ID,
	)
	require.NoError(t, err)

	mockClerk := new(mockClerkService)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_"+suffix, mock.MatchedBy(func(metadata map[string]interface{}) bool {
		return metadata["subscription_tier"] == "premium" && metadata["subscription_status"] == "cancelled"
	})).Return(nil)

	worker := NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo:     profileRepo,
		Remover:         adminRepo,
		ClerkService:    mockClerk,
		RetentionMonths: jobsIntegrationRetentionMonths,
		Logger:          slog.Default(),
	})
	job := &river.Job[ManualTierOverrideExpiryCheckArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, Kind: ManualTierOverrideExpiryCheckArgs{}.Kind(), Queue: "low"},
		Args:   ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"},
	}

	workErr := worker.Work(ctx, job)
	require.NoError(t, workErr)

	var override *string
	var endsAt, retStarted, retExpires, notice30, notice7 *time.Time
	err = pool.QueryRow(ctx, `
		SELECT manual_tier_override, manual_tier_ends_at,
		       retention_started_at, retention_expires_at,
		       retention_notice_30d_sent_at, retention_notice_7d_sent_at
		FROM users WHERE id = $1`, user.ID,
	).Scan(&override, &endsAt, &retStarted, &retExpires, &notice30, &notice7)
	require.NoError(t, err)

	require.Nil(t, override, "manual_tier_override must be cleared")
	require.Nil(t, endsAt, "manual_tier_ends_at must be cleared")
	require.NotNil(t, retStarted, "retention_started_at must be re-armed (restarted), not left stale")
	require.NotNil(t, retExpires, "retention_expires_at must be re-armed")
	require.WithinDuration(t, time.Now().AddDate(0, jobsIntegrationRetentionMonths, 0), *retExpires, 2*time.Minute,
		"retention_expires_at must honor THIS JOB's configured retentionMonths (%d), not a hardcoded default", jobsIntegrationRetentionMonths)
	require.Nil(t, notice30, "retention_notice_30d_sent_at must be nulled on re-arm")
	require.Nil(t, notice7, "retention_notice_7d_sent_at must be nulled on re-arm")

	mockClerk.AssertExpectations(t)

	// Documents the known, deliberate audit-action limitation (see ManualTierOverrideRemover's
	// doc comment): reusing RemoveUserTierOverride means every automatic expiry audits as
	// 'remove_manual_tier', not an expiry-specific action — distinguishable from an
	// admin-initiated removal only by its NULL actor.
	var actorID *string
	var action string
	err = pool.QueryRow(ctx, `
		SELECT user_id::text, action FROM audit_logs
		WHERE resource_id = $1 AND resource_type = 'user'
		ORDER BY created_at DESC LIMIT 1`, user.ID,
	).Scan(&actorID, &action)
	require.NoError(t, err)
	require.Nil(t, actorID, "system-initiated expiry must audit with a NULL actor")
	require.Equal(t, "remove_manual_tier", action,
		"documents the known limitation: reusing RemoveUserTierOverride as-is means the audit action is NOT expiry-specific")
}

// TestManualTierOverrideExpiryCheckWorker_Integration_SecondPassIsNoOp proves the job is
// idempotent: running Work() again immediately after a successful expiry (candidate already
// cleared) does nothing and returns no error — the repository query naturally excludes the
// now-cleared user, so the batch is simply empty on the second pass.
func TestManualTierOverrideExpiryCheckWorker_Integration_SecondPassIsNoOp(t *testing.T) {
	pool := connectJobsTestDB(t)
	enc := newJobsTestFieldEncryptor(t)
	ctx := context.Background()

	profileRepo := repository.NewProfileRepository(pool, enc)
	adminRepo := repository.NewAdminUserRepository(pool, enc)

	suffix := fmt.Sprintf("mte_job_idem_%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	household := "household_2"
	overrideEndsAt := time.Now().Add(-1 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		household, overrideEndsAt, user.ID)
	require.NoError(t, err)

	mockClerk := new(mockClerkService)
	mockClerk.On("UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	worker := NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo:     profileRepo,
		Remover:         adminRepo,
		ClerkService:    mockClerk,
		RetentionMonths: jobsIntegrationRetentionMonths,
		Logger:          slog.Default(),
	})
	job := &river.Job[ManualTierOverrideExpiryCheckArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, Kind: ManualTierOverrideExpiryCheckArgs{}.Kind(), Queue: "low"},
		Args:   ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"},
	}

	require.NoError(t, worker.Work(ctx, job))
	// Second pass: the candidate query must no longer return this user (override already
	// cleared) — the run must be a clean no-op, not an error.
	require.NoError(t, worker.Work(ctx, job))
}
