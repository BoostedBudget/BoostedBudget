//go:build integration

package repository

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

// Integration tests for Issue #1444 ("Grep for the same shape elsewhere"): mcp_recipes.description
// is a nullable TEXT column (migration 000085_create_mcp_recipes.up.sql:10 — no NOT NULL
// constraint), but domain.MCPRecipe.Description is a bare (non-pointer) string field. Scanning a
// row whose description is genuinely NULL into that bare field fails with "cannot scan NULL into
// *string" — the same shape as the linked_items.status/institution_id/institution_name and
// user_identities.email bugs fixed elsewhere in this package.
//
// pgxmock cannot reproduce this (it returns the Go zero value with no error for a nullable column
// regardless of what the real driver would do), so this runs against real Postgres. Create() always
// writes recipe.Description as a Go string (possibly ""), never a SQL NULL, so a raw SQL UPDATE
// bypassing the repository is required to reproduce a genuinely NULL row.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRecipeRepository_NullDescription -tags integration -v

// newNullDescriptionTestTx begins a transaction, registers a rollback, creates repos and a test user,
// and returns them for use in NULL description integration tests.
func newNullDescriptionTestTx(t *testing.T, ctx context.Context, suffix string) (RecipeRepository, *domain.User, pgx.Tx) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	profileRepo := NewProfileRepository(tx, enc)
	repo := NewRecipeRepository(tx)

	user := &domain.User{
		ClerkUserID:        "clerk_nulldesc_" + suffix,
		Email:              "nulldesc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	return repo, user, tx
}

func TestRecipeRepository_NullDescription_List(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repo, user, tx := newNullDescriptionTestTx(t, ctx, suffix)

	recipe := &domain.MCPRecipe{
		UserID:      user.ID,
		Name:        "recipe-" + suffix,
		Description: "a real description",
		Spec:        []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled:     true,
	}
	require.NoError(t, repo.Create(ctx, recipe))

	_, err := tx.Exec(ctx, `UPDATE mcp_recipes SET description = NULL WHERE id = $1`, recipe.ID)
	require.NoError(t, err)

	got, err := repo.List(ctx, user.ID)
	require.NoError(t, err, "a NULL description row must not fail the scan")
	require.Len(t, got, 1)
	assert.Equal(t, "", got[0].Description, "NULL description should surface as the empty string")
}

func TestRecipeRepository_NullDescription_Get(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repo, user, tx := newNullDescriptionTestTx(t, ctx, suffix)

	recipe := &domain.MCPRecipe{
		UserID:      user.ID,
		Name:        "recipe-" + suffix,
		Description: "a real description",
		Spec:        []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled:     true,
	}
	require.NoError(t, repo.Create(ctx, recipe))

	_, err := tx.Exec(ctx, `UPDATE mcp_recipes SET description = NULL WHERE id = $1`, recipe.ID)
	require.NoError(t, err)

	got, err := repo.Get(ctx, recipe.ID, user.ID)
	require.NoError(t, err, "a NULL description row must not fail the scan")
	require.NotNil(t, got)
	assert.Equal(t, "", got.Description, "NULL description should surface as the empty string")
}
