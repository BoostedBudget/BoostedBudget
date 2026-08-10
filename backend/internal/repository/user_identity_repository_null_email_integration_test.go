//go:build integration

package repository

// Integration tests for Issue #1444 ("Grep for the same shape elsewhere"): user_identities.email is
// a nullable VARCHAR(255)/TEXT column (migration 000008_create_user_identities_table.up.sql:18 — no
// NOT NULL constraint; the column comment notes "may differ across providers"), but
// domain.UserIdentity.Email is a bare (non-pointer) string field. Scanning a row whose email is
// genuinely NULL into that bare field fails with "cannot scan NULL into *string" — the same shape as
// the linked_items.status/institution_id/institution_name bugs fixed elsewhere in this package.
//
// pgxmock cannot reproduce this (it returns the Go zero value with no error for a nullable column
// regardless of what the real driver would do), so this runs against real Postgres. Create() always
// encrypts and writes a Go string (possibly ""), never a SQL NULL, so a raw SQL UPDATE bypassing the
// repository is required to reproduce a genuinely NULL row.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestUserIdentityRepository_NullEmail -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// createNullEmailFixture creates a fresh user + user_identity (with a real email, so encryption is
// exercised exactly like production), then forces email to genuine SQL NULL via a raw UPDATE that
// bypasses the repository (Create() only ever writes a Go string, never a SQL NULL).
func createNullEmailFixture(t *testing.T, ctx context.Context, tx pgx.Tx, repo UserIdentityRepository, userID, suffix string) *domain.UserIdentity {
	t.Helper()

	identity := &domain.UserIdentity{
		UserID:         userID,
		ClerkUserID:    "clerk_nullemail_" + suffix,
		Provider:       "clerk",
		ProviderUserID: "provider_" + suffix,
		Email:          "nullemail-" + suffix + "@example.com",
		IsPrimary:      true,
	}
	require.NoError(t, repo.Create(ctx, identity))

	_, err := tx.Exec(ctx, `UPDATE user_identities SET email = NULL WHERE id = $1`, identity.ID)
	require.NoError(t, err)

	return identity
}

// newNullEmailTestTx begins a transaction, registers a rollback, creates repos and a test user,
// and returns them for use in NULL email integration tests.
func newNullEmailTestTx(t *testing.T, ctx context.Context, suffix string) (pgx.Tx, UserIdentityRepository, *domain.User) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	profileRepo := NewProfileRepository(tx, enc)
	repo := NewUserIdentityRepository(tx, enc)

	user := &domain.User{
		ClerkUserID:        "clerk_nullemailuser_" + suffix,
		Email:              "nullemailuser-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	return tx, repo, user
}

func TestUserIdentityRepository_NullEmail_GetByClerkID(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tx, repo, user := newNullEmailTestTx(t, ctx, suffix)

	fixture := createNullEmailFixture(t, ctx, tx, repo, user.ID, suffix)

	got, err := repo.GetByClerkID(ctx, "clerk_nullemail_"+suffix)
	require.NoError(t, err, "a NULL email row must not fail the scan")
	assert.Equal(t, fixture.ID, got.ID)
	assert.Equal(t, "", got.Email, "NULL email should surface as the empty string")
}

func TestUserIdentityRepository_NullEmail_GetByUserID(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tx, repo, user := newNullEmailTestTx(t, ctx, suffix)

	fixture := createNullEmailFixture(t, ctx, tx, repo, user.ID, suffix)

	identities, err := repo.GetByUserID(ctx, user.ID)
	require.NoError(t, err, "a NULL email row must not fail the scan")
	require.Len(t, identities, 1)
	assert.Equal(t, fixture.ID, identities[0].ID)
	assert.Equal(t, "", identities[0].Email, "NULL email should surface as the empty string")
}
