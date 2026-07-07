package repository_test

// household_schema_integration_test.go — DB-backed constraint verification for
// the 000106–000111 migrations.
//
// These tests require a live DATABASE_URL with migrations applied up to 000111.
// They are skipped automatically when DATABASE_URL is unset (CI/CD environments
// that haven't run make migrate-up yet).
//
// Per project rule: pgxmock never catches constraint/type errors — add
// _integration_test.go for real SQL behavior (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//   DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestHouseholdSchema -v
//
// Issue #1365 — Phase 1: schema + domain foundation.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setupHouseholdSchemaDB creates an isolated DB pool for schema tests and
// returns a teardown function that removes all rows inserted by the test.
// Each test function is responsible for cleaning up its own rows (or using
// subtests with per-subtest cleanup) to keep tests independent.
func setupHouseholdSchemaDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// insertTestUser inserts a minimal users row for FK purposes and returns its UUID.
// Cleans up on t.Cleanup.
func insertTestUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO users (clerk_user_id, email, email_blind_idx, subscription_tier, subscription_status)
		VALUES (gen_random_uuid()::text, 'enc_test', 'blind_test_'||gen_random_uuid()::text, 'premium', 'active')
		RETURNING id::text
	`).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestUser: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTestHousehold inserts a households row and returns its UUID.
// Cleans up on t.Cleanup.
func insertTestHousehold(t *testing.T, pool *pgxpool.Pool, ownerID string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO households (owner_user_id, name)
		VALUES ($1, 'Test Household')
		RETURNING id::text
	`, ownerID).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestHousehold: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTestUserAccount inserts a minimal user_accounts row and returns its UUID.
func insertTestUserAccount(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, userID).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestUserAccount: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// ─── TestHouseholdSchema_Households ──────────────────────────────────────────

func TestHouseholdSchema_Households(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	t.Run("one household per owner", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `INSERT INTO households (owner_user_id, name) VALUES ($1, 'Second')`, owner)
		if err == nil {
			t.Fatal("expected unique violation for second household, got nil")
		}
	})

	t.Run("blank name rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		_, err := pool.Exec(ctx, `INSERT INTO households (owner_user_id, name) VALUES ($1, '   ')`, owner)
		if err == nil {
			t.Fatal("expected check violation for blank name, got nil")
		}
	})

	t.Run("name over 255 chars rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		longName := make([]byte, 256)
		for i := range longName {
			longName[i] = 'x'
		}
		_, err := pool.Exec(ctx, `INSERT INTO households (owner_user_id, name) VALUES ($1, $2)`, owner, string(longName))
		if err == nil {
			t.Fatal("expected check violation for name > 255 chars, got nil")
		}
	})

	t.Run("fk cascade: deleting household removes members", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hhID := insertTestHousehold(t, pool, owner)

		// Insert a member row (no conflict with other subtests because household is fresh)
		_, err := pool.Exec(ctx, `
			INSERT INTO household_members (household_id, user_id, role, status)
			VALUES ($1, $2, 'owner', 'active')
		`, hhID, owner)
		if err != nil {
			t.Fatalf("insert member: %v", err)
		}

		// Delete household — member should cascade
		_, err = pool.Exec(ctx, `DELETE FROM households WHERE id = $1`, hhID)
		if err != nil {
			t.Fatalf("delete household: %v", err)
		}

		var count int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE household_id = $1`, hhID).Scan(&count) //nolint:errcheck
		if count != 0 {
			t.Fatalf("expected 0 members after cascade, got %d", count)
		}
	})
}

// ─── TestHouseholdSchema_Members ─────────────────────────────────────────────

func TestHouseholdSchema_Members(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	t.Run("one active household per user", func(t *testing.T) {
		owner1 := insertTestUser(t, pool)
		owner2 := insertTestUser(t, pool)
		member := insertTestUser(t, pool)

		hh1 := insertTestHousehold(t, pool, owner1)
		hh2 := insertTestHousehold(t, pool, owner2)

		// First active member row — fine
		_, err := pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')`, hh1, member)
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM household_members WHERE household_id = $1 AND user_id = $2`, hh1, member) //nolint:errcheck
		})

		// Second active row for same user — must fail
		_, err = pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')`, hh2, member)
		if err == nil {
			t.Fatal("expected unique violation for second active membership, got nil")
		}
	})

	t.Run("one active owner per household", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		other := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`, hh, owner)
		if err != nil {
			t.Fatalf("insert first owner: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM household_members WHERE household_id = $1`, hh) //nolint:errcheck
		})

		_, err = pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`, hh, other)
		if err == nil {
			t.Fatal("expected unique violation for second active owner, got nil")
		}
	})

	t.Run("invalid role rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		_, err := pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'admin', 'active')`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for invalid role, got nil")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		_, err := pool.Exec(ctx, `INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'banned')`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for invalid status, got nil")
		}
	})

	t.Run("active with removed_at rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		_, err := pool.Exec(ctx, `
			INSERT INTO household_members (household_id, user_id, role, status, removed_at)
			VALUES ($1, $2, 'member', 'active', now())
		`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for active+removed_at, got nil")
		}
	})

	t.Run("removed without removed_at rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		_, err := pool.Exec(ctx, `
			INSERT INTO household_members (household_id, user_id, role, status, removed_at)
			VALUES ($1, $2, 'member', 'removed', NULL)
		`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for removed+null removed_at, got nil")
		}
	})
}

// ─── TestHouseholdSchema_SubscriptionEntitlements ────────────────────────────

func TestHouseholdSchema_SubscriptionEntitlements(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	t.Run("own_subscription with household_id rejected", func(t *testing.T) {
		user := insertTestUser(t, pool)
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id)
			VALUES ($1, 'own_subscription', 'premium', 'active', $2)
		`, user, hh)
		if err == nil {
			t.Fatal("expected check violation for own_subscription with household_id, got nil")
		}
	})

	t.Run("household without payer_user_id rejected", func(t *testing.T) {
		user := insertTestUser(t, pool)
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id)
			VALUES ($1, 'household', 'household_2', 'active', $2)
		`, user, hh)
		if err == nil {
			t.Fatal("expected check violation for household without payer_user_id, got nil")
		}
	})

	t.Run("effective_until before effective_from rejected", func(t *testing.T) {
		user := insertTestUser(t, pool)
		now := time.Now()
		earlier := now.Add(-time.Hour)

		_, err := pool.Exec(ctx, `
			INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
			VALUES ($1, 'own_subscription', 'premium', 'active', $2, $3)
		`, user, now, earlier)
		if err == nil {
			t.Fatal("expected check violation for effective_until <= effective_from, got nil")
		}
	})

	t.Run("invalid source rejected", func(t *testing.T) {
		user := insertTestUser(t, pool)
		_, err := pool.Exec(ctx, `
			INSERT INTO subscription_entitlements (user_id, source, tier, status)
			VALUES ($1, 'employer', 'premium', 'active')
		`, user)
		if err == nil {
			t.Fatal("expected check violation for invalid source, got nil")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		user := insertTestUser(t, pool)
		_, err := pool.Exec(ctx, `
			INSERT INTO subscription_entitlements (user_id, source, tier, status)
			VALUES ($1, 'own_subscription', 'premium', 'suspended')
		`, user)
		if err == nil {
			t.Fatal("expected check violation for invalid status, got nil")
		}
	})
}

// ─── TestHouseholdSchema_Invites ─────────────────────────────────────────────

func TestHouseholdSchema_Invites(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	t.Run("accepted without accepted_at rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_invites
				(household_id, inviter_user_id, invitee_email, invitee_email_blind_idx,
				 token_hash, status, expires_at, accepted_at)
			VALUES ($1, $2, 'enc_email', 'blind_email', 'tok1', 'accepted', now() + interval '1 day', NULL)
		`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for accepted with null accepted_at, got nil")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_invites
				(household_id, inviter_user_id, invitee_email, invitee_email_blind_idx,
				 token_hash, status, expires_at)
			VALUES ($1, $2, 'enc_email', 'blind_email', 'tok2', 'sent', now() + interval '1 day')
		`, hh, owner)
		if err == nil {
			t.Fatal("expected check violation for invalid status, got nil")
		}
	})

	t.Run("unique pending invite per household+email", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_invites
				(household_id, inviter_user_id, invitee_email, invitee_email_blind_idx,
				 token_hash, status, expires_at)
			VALUES ($1, $2, 'enc_email', 'dup_blind_email', 'tok3', 'pending', now() + interval '1 day')
		`, hh, owner)
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM household_invites WHERE token_hash IN ('tok3','tok4')`) //nolint:errcheck
		})

		_, err = pool.Exec(ctx, `
			INSERT INTO household_invites
				(household_id, inviter_user_id, invitee_email, invitee_email_blind_idx,
				 token_hash, status, expires_at)
			VALUES ($1, $2, 'enc_email', 'dup_blind_email', 'tok4', 'pending', now() + interval '1 day')
		`, hh, owner)
		if err == nil {
			t.Fatal("expected unique violation for duplicate pending invite, got nil")
		}
	})
}

// ─── TestHouseholdSchema_AccountShares ───────────────────────────────────────

func TestHouseholdSchema_AccountShares(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	t.Run("revoked_at before shared_at rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		ua := insertTestUserAccount(t, pool, owner)
		sharedAt := time.Now()
		before := sharedAt.Add(-time.Second)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_account_shares
				(household_id, user_account_id, visibility, shared_at, revoked_at)
			VALUES ($1, $2, 'shared', $3, $4)
		`, hh, ua, sharedAt, before)
		if err == nil {
			t.Fatal("expected check violation for revoked_at < shared_at, got nil")
		}
	})

	t.Run("one active share per household+account", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		ua := insertTestUserAccount(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility)
			VALUES ($1, $2, 'shared')
		`, hh, ua)
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
		})

		_, err = pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility)
			VALUES ($1, $2, 'shared')
		`, hh, ua)
		if err == nil {
			t.Fatal("expected unique violation for duplicate active share, got nil")
		}
	})

	t.Run("re-share allowed after revocation", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		ua := insertTestUserAccount(t, pool, owner)

		// First share
		_, err := pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility)
			VALUES ($1, $2, 'shared')
		`, hh, ua)
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`, hh, ua) //nolint:errcheck
		})

		// Revoke it
		_, err = pool.Exec(ctx, `
			UPDATE household_account_shares SET revoked_at = now()
			WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NULL
		`, hh, ua)
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}

		// Re-share — must succeed (revoked_at IS NULL partial unique no longer applies)
		_, err = pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility)
			VALUES ($1, $2, 'shared')
		`, hh, ua)
		if err != nil {
			t.Fatalf("re-share after revocation: %v", err)
		}
	})

	t.Run("invalid visibility rejected", func(t *testing.T) {
		owner := insertTestUser(t, pool)
		hh := insertTestHousehold(t, pool, owner)
		ua := insertTestUserAccount(t, pool, owner)

		_, err := pool.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility)
			VALUES ($1, $2, 'public')
		`, hh, ua)
		if err == nil {
			t.Fatal("expected check violation for invalid visibility, got nil")
		}
	})
}
