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
	"github.com/maxnussbaum/BoostedBudget/internal/database"
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
	// Issue #1461: route through database.NewPoolFromURL (not a bare pgxpool.New) so this pool
	// pins session TimeZone=UTC exactly like the production pool does.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
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

// dbNow returns the database server's own current time — mirrors the identically-named helper in
// internal/repository/admin_user_repository_remove_tier_override_integration_test.go (re-declared
// here for this package, same convention as stubEncryptionService above): fencing must be against
// Postgres's clock, never against a Go-side time.Now(), which a containerized Postgres server's
// clock can disagree with by more than any fixed tolerance would safely assume.
func dbNow(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT NOW()").Scan(&now))
	return now
}

// dbAddMonthsQuery computes ts + months entirely in Postgres via make_interval, pinned to UTC
// BEFORE the calendar addition (Issue #1461) — mirrors the identically-named constant in
// admin_user_repository_remove_tier_override_integration_test.go's own dbAddMonthsQuery
// (re-declared here for this package). The same semantics as
// AdminUserRepository.RemoveUserTierOverride's SQL uses for retention_expires_at.
//
// Historically (pre-#1461) this was `$1::timestamptz + make_interval(months => $2)`, with no pin
// of its own — the same unpinned expression production uses, so both sides drifted identically
// with whatever session TimeZone the shared pool happened to have, and this test fence could never
// detect a regression in NewPool's UTC pin. The AT TIME ZONE 'UTC' conversion makes this
// expectation independent of the connection's own session TimeZone.
//
// Go's time.Time.AddDate normalizes month overflow FORWARD (2026-07-31 + 9mo -> 2027-05-01);
// Postgres's make_interval CLAMPS to the target month's last day (-> 2027-04-30) instead.
// Computing the expectation here — instead of via AddDate — keeps this assertion calendar-immune
// by construction: expectation and actual value are both computed by Postgres, so they can never
// disagree regardless of what day of the month the test runs on.
const dbAddMonthsQuery = `SELECT ($1::timestamptz AT TIME ZONE 'UTC') + make_interval(months => $2)`

func dbAddMonths(t *testing.T, pool *pgxpool.Pool, ts time.Time, months int) time.Time {
	t.Helper()
	var result time.Time
	require.NoError(t, pool.QueryRow(context.Background(), dbAddMonthsQuery, ts, months).Scan(&result))
	return result.UTC()
}

// assertBetweenFence asserts ts falls within [before, after] inclusive — mirrors the
// identically-named helper in admin_user_repository_remove_tier_override_integration_test.go,
// re-declared here for this package.
func assertBetweenFence(t *testing.T, before, after, ts time.Time, msg string) {
	t.Helper()
	require.False(t, ts.Before(before), "%s: got %v, expected not before fence-before %v", msg, ts, before)
	require.False(t, ts.After(after), "%s: got %v, expected not after fence-after %v", msg, ts, after)
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
	// Issue #1413 (Codex review, PR #1443 follow-up — Finding 6): pin the exact key set
	// ({subscription_tier, subscription_status, tier}, len(metadata) == 3), matching the standard
	// already established in manual_tier_override_expiry_check_test.go and
	// admin_user_service_test.go. A two-key check (this matcher's previous shape) silently accepted
	// a payload missing "tier": nil — the key whose entire purpose is retroactively cleaning up a
	// poisoned Clerk user — or one carrying extra keys; falsified against a broken
	// ClerkPublicMetadata() (domain/user.go) that returned "tier": "premium" instead of nil before
	// this tightening, which the old two-key matcher did not catch.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_"+suffix, mock.MatchedBy(func(metadata map[string]interface{}) bool {
		tierVal, hasTierKey := metadata["subscription_tier"]
		statusVal, hasStatusKey := metadata["subscription_status"]
		nilTierVal, hasNilTierKey := metadata["tier"]
		return len(metadata) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "cancelled" &&
			hasNilTierKey && nilTierVal == nil
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

	before := dbNow(t, pool)
	workErr := worker.Work(ctx, job)
	require.NoError(t, workErr)
	after := dbNow(t, pool)

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
	// Expectation computed in Postgres (make_interval), not via Go's time.Time.AddDate — the two
	// disagree on month-end dates (Go normalizes overflow forward, Postgres clamps to the target
	// month's last day), so this fence is calendar-immune by construction rather than merely
	// tolerant enough to survive today's run.
	assertBetweenFence(t, dbAddMonths(t, pool, before, jobsIntegrationRetentionMonths), dbAddMonths(t, pool, after, jobsIntegrationRetentionMonths), *retExpires,
		fmt.Sprintf("retention_expires_at must honor THIS JOB's configured retentionMonths (%d), not a hardcoded default", jobsIntegrationRetentionMonths))
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
