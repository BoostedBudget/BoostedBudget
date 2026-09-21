//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Integration test for #1699 MCP-3: a duplicate recipe name is a per-user unique-violation
// (mcp_recipes_user_id_name_unique, migration 000085_create_mcp_recipes.up.sql:22). The
// repository must translate it to domain.ErrDuplicateRecipeName so the error mapper can return
// 409 instead of a generic 500. pgxmock cannot raise a real constraint violation, so this runs
// against real Postgres.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRecipeRepository_DuplicateName -tags integration -p 1 -v

func newRecipeDuplicateNameTestTx(t *testing.T, ctx context.Context, suffix string) (RecipeRepository, *domain.User, pgx.Tx) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	profileRepo := NewProfileRepository(tx, enc)
	repo := NewRecipeRepository(tx)

	user := &domain.User{
		ClerkUserID:        "clerk_dupname_" + suffix,
		Email:              "dupname-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	return repo, user, tx
}

func TestRecipeRepository_DuplicateName_MapsToSentinel(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repo, user, _ := newRecipeDuplicateNameTestTx(t, ctx, suffix)

	first := &domain.MCPRecipe{
		UserID:      user.ID,
		Name:        "recipe-" + suffix,
		Description: "first",
		Spec:        []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled:     true,
	}
	require.NoError(t, repo.Create(ctx, first))

	second := &domain.MCPRecipe{
		UserID:      user.ID,
		Name:        "recipe-" + suffix,
		Description: "second",
		Spec:        []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled:     true,
	}
	err := repo.Create(ctx, second)
	require.Error(t, err, "creating a second recipe with the same user+name must fail")
	assert.True(t, errors.Is(err, domain.ErrDuplicateRecipeName), "expected ErrDuplicateRecipeName, got %v", err)
}

func TestRecipeRepository_DuplicateName_UpdateMapsToSentinel(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repo, user, _ := newRecipeDuplicateNameTestTx(t, ctx, suffix)

	first := &domain.MCPRecipe{
		UserID:  user.ID,
		Name:    "recipe-a-" + suffix,
		Spec:    []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled: true,
	}
	require.NoError(t, repo.Create(ctx, first))

	second := &domain.MCPRecipe{
		UserID:  user.ID,
		Name:    "recipe-b-" + suffix,
		Spec:    []domain.MCPRecipeCall{{OperationID: "getAccounts"}},
		Enabled: true,
	}
	require.NoError(t, repo.Create(ctx, second))

	second.Name = first.Name
	err := repo.Update(ctx, second)
	require.Error(t, err, "renaming a recipe onto an existing name must fail")
	assert.True(t, errors.Is(err, domain.ErrDuplicateRecipeName), "expected ErrDuplicateRecipeName, got %v", err)
}
