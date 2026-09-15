//go:build integration

package repository

// Issue #1413 Finding 2 (Low, Codex review, PR #1443 second follow-up round) — making the
// falsification behind profile_repository_finding2_lock_contention_integration_test.go
// reproducible from source.
//
// That file's own doc comment claims each of its four regression tests was verified RED against
// (a) the ORIGINAL bare `COALESCE(plaid_cleanup_pending_at, NOW())` form (no CTE) and (b) the
// NAIVE `COALESCE(plaid_cleanup_pending_at, clock_timestamp())` form (still no CTE) — 3 runs per
// writer per variant, 24 runs total, every one a deterministic failure. Until this file, that
// claim existed ONLY as prose: there was no probe mode, no parameterisation, and no patch checked
// in, so nobody could audit it — and per this round's own review, an unaudited count is exactly
// the kind of thing that gets relayed onward as evidence when it shouldn't be trusted.
//
// This file is that audit trail. For each of the four writers it checks in the EXACT broken SQL —
// reconstructed here as raw strings executed directly via pool.Exec, NEVER by editing
// profile_repository.go — and a test proving each one DOES backdate plaid_cleanup_pending_at to
// before the lock's real release, using the identical real waitUntilBlockedBy barrier and
// runWriterUnderLockContention harness the real regression tests use. Where a broken form's
// WHERE-clause predicate is shared with production code
// (plaidDisconnectRepairEligibleStatusPredicate, noActiveManualOverridePredicate), these probes
// reference those SAME package constants rather than hand-copying a third rewording, so a probe
// can never silently drift out of sync with what an actual revert would contain.
//
// These are gated behind FINDING2_RUN_PROBES=1 and skipped by default: they exist to demonstrate
// a KNOWN-BROKEN pattern against the real database, not to exercise production code, so a PASS
// here is confirmation the bug still reproduces (good — the probe still works), not evidence
// anything is wrong with the shipped writers. They must never run unattended as part of the
// ordinary integration suite. To reproduce this round's falsification (3 runs per writer per
// variant) from a clean checkout:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5432/boostedbudget?sslmode=disable \
//	  FINDING2_RUN_PROBES=1 go test ./internal/repository/... -tags integration \
//	  -run TestFinding2Probe -v -p 1 -count=3
//
// Raise -count for more repetitions; every iteration is expected to fail the "backdated" assertion
// in exactly the same way (i.e. PASS, since the assertion IS "the bug reproduced").
//
// Issue #1442 deleted a fifth writer, CancelIfPastDueWithRetention (zero production callers), along
// with its two dedicated probes here. #1652 deleted CancelIfPastDueOccurrenceWithRetention and its
// two probes, which reconstructed the identical broken SQL shape for its occurrence-scoped successor.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// skipUnlessFinding2ProbesEnabled is the gate documented in this file's own doc comment above.
func skipUnlessFinding2ProbesEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("FINDING2_RUN_PROBES") == "" {
		t.Skip("Finding 2 falsification probe demonstrating a KNOWN-BROKEN pre-ROUND-23 SQL form; set FINDING2_RUN_PROBES=1 to run — see this file's doc comment for why it must not run unattended")
	}
}

// assertMarkerWasBackdated is the exact logical negation of
// assertMarkerReflectsPostWaitTime (profile_repository_finding2_lock_contention_integration_test.go):
// a broken (non-CTE) writer's SET-list is computed once against the pre-lock-wait row version, so
// its marker must land strictly BEFORE the lock's actual release, not at-or-after it.
func assertMarkerWasBackdated(t *testing.T, releaseFence time.Time, marker *time.Time) {
	t.Helper()
	require.NotNil(t, marker, "plaid_cleanup_pending_at must be set")
	assert.True(t, marker.Before(releaseFence),
		"expected this deliberately-broken (no-CTE) form to backdate plaid_cleanup_pending_at to BEFORE the lock's real release (%v) — got %v; if this ever passes, the reverted SQL is no longer distinguishable from the fix and this probe has stopped proving the defect it exists to demonstrate", releaseFence, *marker)
}

// TestFinding2Probe_UpdateSubscriptionFromPaddle_BareNOW_Backdates reconstructs
// UpdateSubscriptionFromPaddle's pre-ROUND-23 bare form (nil-subscriptionID branch): a plain
// UPDATE (no CTE) with COALESCE(plaid_cleanup_pending_at, NOW()).
func TestFinding2Probe_UpdateSubscriptionFromPaddle_BareNOW_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_bnow_" + suffix,
		Email:              "f2p-bnow-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	startedAt := time.Now()
	brokenQuery := `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, NOW()) ELSE plaid_cleanup_pending_at END WHERE id = $5 AND is_deleted = FALSE`

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, "premium", "cancelled", startedAt, (*time.Time)(nil), user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}

// TestFinding2Probe_UpdateSubscriptionFromPaddle_NaiveClockTimestamp_Backdates is the same as
// above but with the naive clock_timestamp()-without-CTE substitution — proves a bare
// function-name swap does not close the gap either.
func TestFinding2Probe_UpdateSubscriptionFromPaddle_NaiveClockTimestamp_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_nct_" + suffix,
		Email:              "f2p-nct-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	startedAt := time.Now()
	brokenQuery := `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END WHERE id = $5 AND is_deleted = FALSE`

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, "premium", "cancelled", startedAt, (*time.Time)(nil), user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}

// TestFinding2Probe_UpdateTrialStatus_BareNOW_Backdates reconstructs UpdateTrialStatus's
// pre-ROUND-23 bare form.
func TestFinding2Probe_UpdateTrialStatus_BareNOW_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_trial_bnow_" + suffix,
		Email:              "f2p-trial-bnow-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	trialStarted := time.Now()
	trialEnds := trialStarted.Add(30 * 24 * time.Hour)
	brokenQuery := `
		UPDATE users
		SET subscription_status = $1,
		    subscription_tier = 'premium',
		    trial_started_at = $2,
		    trial_ends_at = $3,
		    had_trial = $4,
		    updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, NOW())
		WHERE id = $5 AND is_deleted = FALSE
	`

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, "cancelled", trialStarted, trialEnds, true, user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}

// TestFinding2Probe_UpdateTrialStatus_NaiveClockTimestamp_Backdates is the naive
// clock_timestamp()-without-CTE variant of the probe above.
func TestFinding2Probe_UpdateTrialStatus_NaiveClockTimestamp_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_trial_nct_" + suffix,
		Email:              "f2p-trial-nct-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	trialStarted := time.Now()
	trialEnds := trialStarted.Add(30 * 24 * time.Hour)
	brokenQuery := `
		UPDATE users
		SET subscription_status = $1,
		    subscription_tier = 'premium',
		    trial_started_at = $2,
		    trial_ends_at = $3,
		    had_trial = $4,
		    updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
		WHERE id = $5 AND is_deleted = FALSE
	`

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, "cancelled", trialStarted, trialEnds, true, user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}

// TestFinding2Probe_ExpireTrialIfNoOverride_BareNOW_Backdates reconstructs
// ExpireTrialIfNoOverride's pre-ROUND-23 bare form.
func TestFinding2Probe_ExpireTrialIfNoOverride_BareNOW_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_te_bnow_" + suffix,
		Email:              "f2p-te-bnow-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	brokenQuery := `
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, NOW())
		WHERE id = $1 AND subscription_status = 'trial'
		` + noActiveManualOverridePredicate

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}

// TestFinding2Probe_ExpireTrialIfNoOverride_NaiveClockTimestamp_Backdates is the naive
// clock_timestamp()-without-CTE variant of the probe above.
func TestFinding2Probe_ExpireTrialIfNoOverride_NaiveClockTimestamp_Backdates(t *testing.T) {
	skipUnlessFinding2ProbesEnabled(t)
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2p_te_nct_" + suffix,
		Email:              "f2p-te-nct-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	brokenQuery := `
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
		WHERE id = $1 AND subscription_status = 'trial'
		` + noActiveManualOverridePredicate

	_, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		ct, err := pool.Exec(ctx, brokenQuery, user.ID)
		if err == nil && ct.RowsAffected() == 0 {
			return fmt.Errorf("broken probe UPDATE matched zero rows")
		}
		return err
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerWasBackdated(t, releaseFence, marker)
}
