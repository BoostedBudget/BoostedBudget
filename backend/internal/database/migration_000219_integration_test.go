//go:build integration

package database

// Integration tests for migration 000219 — the one-time repair of legacy manual_tier_override /
// manual_tier_ends_at rows for users RevenueCat has confirmed inactive (#1652 todo 4, Part B).
//
// The migration reads "RevenueCat confirms inactive" from RC's own stored projection
// (revenuecat_customers.grants + access_updated_at), never from the legacy column. These tests seed
// each distinguishable state via raw SQL, apply the real up SQL inside a rolled-back transaction,
// and assert exactly which rows were repaired.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000219 -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMig211User inserts a minimal users row carrying a legacy manual tier override, returning its
// UUID. is_deleted is settable so the soft-deleted exclusion can be exercised.
func seedMig211User(t *testing.T, tx pgx.Tx, suffix string, isDeleted bool) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (
			id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
			manual_tier_override, manual_tier_ends_at, is_deleted
		)
		VALUES ($1, $2, $3, 'premium', 'inactive', 'iad', 'household_3', clock_timestamp() + interval '30 days', $4)
	`, userID, "test|mig211-"+suffix+"-"+userID, "mig211-"+suffix+"-"+userID+"@example.com", isDeleted)
	require.NoError(t, err)
	return userID
}

// seedMig211Customer inserts a revenuecat_customers row with the given grants JSON and
// access_updated_at (nil = never refreshed).
func seedMig211Customer(t *testing.T, tx pgx.Tx, userID, grants string, accessUpdatedAt *time.Time) {
	t.Helper()
	_, err := tx.Exec(context.Background(),
		`INSERT INTO revenuecat_customers (user_id, grants, access_updated_at) VALUES ($1, $2::jsonb, $3)`,
		userID, grants, accessUpdatedAt)
	require.NoError(t, err)
}

// readMig211Override returns the user's manual_tier_override and manual_tier_ends_at.
func readMig211Override(t *testing.T, tx pgx.Tx, userID string) (*string, *time.Time) {
	t.Helper()
	var override *string
	var endsAt *time.Time
	require.NoError(t, tx.QueryRow(context.Background(),
		`SELECT manual_tier_override, manual_tier_ends_at FROM users WHERE id=$1`, userID).Scan(&override, &endsAt))
	return override, endsAt
}

func TestMigration000219_BackfillLegacyManualTierOverride(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	refreshed := time.Now().Add(-time.Hour)
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	// RC-inactive: refreshed, no grants at all.
	inactiveEmpty := seedMig211User(t, tx, "inactive_empty", false)
	seedMig211Customer(t, tx, inactiveEmpty, `[]`, &refreshed)

	// RC-inactive: refreshed, only an expired comp grant.
	inactiveExpired := seedMig211User(t, tx, "inactive_expired", false)
	seedMig211Customer(t, tx, inactiveExpired, `[{"key":"comp_premium","expires_at":"`+past+`"}]`, &refreshed)

	// RC-active: refreshed, a live paid grant — must be left untouched.
	active := seedMig211User(t, tx, "active", false)
	seedMig211Customer(t, tx, active, `[{"key":"paid_premium","expires_at":"`+future+`"}]`, &refreshed)

	// RC-active: refreshed, a permanent (no-expiry) comp grant — must be left untouched.
	activePermanent := seedMig211User(t, tx, "active_permanent", false)
	seedMig211Customer(t, tx, activePermanent, `[{"key":"comp_household_2","expires_at":null}]`, &refreshed)

	// Never refreshed: a customer row exists but RC has not read the provider — not "confirmed".
	neverRefreshed := seedMig211User(t, tx, "never_refreshed", false)
	seedMig211Customer(t, tx, neverRefreshed, `[]`, nil)

	// No RC customer row at all — RC has no opinion.
	noRC := seedMig211User(t, tx, "no_rc", false)

	// RC-inactive but soft-deleted — excluded.
	deleted := seedMig211User(t, tx, "deleted", true)
	seedMig211Customer(t, tx, deleted, `[]`, &refreshed)

	upSQL := readMigrationFile(t, "000219_backfill_legacy_manual_tier_override.up.sql")
	_, err = tx.Exec(ctx, upSQL)
	require.NoError(t, err, "migration 000219 failed")

	for _, tc := range []struct {
		name     string
		userID   string
		repaired bool
	}{
		{"RC-inactive with no grants", inactiveEmpty, true},
		{"RC-inactive with only an expired comp grant", inactiveExpired, true},
		{"RC-active with a live paid grant", active, false},
		{"RC-active with a permanent comp grant", activePermanent, false},
		{"customer row never refreshed", neverRefreshed, false},
		{"no RC customer row", noRC, false},
		{"RC-inactive but soft-deleted", deleted, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			override, endsAt := readMig211Override(t, tx, tc.userID)
			if tc.repaired {
				assert.Nil(t, override, "an RC-inactive user's legacy override must be nulled")
				assert.Nil(t, endsAt, "an RC-inactive user's legacy ends_at must be nulled")
			} else {
				require.NotNil(t, override, "a non-RC-inactive user's legacy override must be preserved")
				assert.Equal(t, "household_3", *override)
				assert.NotNil(t, endsAt)
			}
		})
	}

	// Idempotency: re-applying the up migration is a no-op and must not error.
	_, err = tx.Exec(ctx, upSQL)
	require.NoError(t, err, "migration 000219 must be idempotent")

	// The down migration is a bounded no-op and must not error.
	downSQL := readMigrationFile(t, "000219_backfill_legacy_manual_tier_override.down.sql")
	_, err = tx.Exec(ctx, downSQL)
	require.NoError(t, err, "migration 000219 down must be a safe no-op")
}