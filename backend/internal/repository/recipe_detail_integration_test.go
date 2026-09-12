//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRecipeRepository_DetailJSONBRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, user, tx := newNullDescriptionTestTx(t, ctx, fmt.Sprintf("detail_%d", time.Now().UnixNano()))
	raw := `[{"operation_id":"getAccounts","params":{"include_hidden":true},"body":{"nested":{"value":2}},"path_params":{"id":"synthetic"}},{"operation_id":"getAccounts","detail":"compact"},{"operation_id":"getTransactions","detail":"full"}]`
	var spec []domain.MCPRecipeCall
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	recipe := &domain.MCPRecipe{UserID: user.ID, Name: "Synthetic detail roundtrip", Spec: spec, Enabled: true}
	require.NoError(t, repo.Create(ctx, recipe))
	check := func(got *domain.MCPRecipe) {
		t.Helper()
		encoded, err := json.Marshal(got.Spec)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(encoded))
	}
	got, err := repo.Get(ctx, recipe.ID, user.ID)
	require.NoError(t, err)
	check(got)
	listed, err := repo.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	check(&listed[0])
	// Historical JSON null is absent semantically, and remains absent when rewritten.
	_, err = tx.Exec(ctx, `UPDATE mcp_recipes SET spec=jsonb_set(spec,'{0,detail}','null'::jsonb) WHERE id=$1`, recipe.ID)
	require.NoError(t, err)
	got, err = repo.Get(ctx, recipe.ID, user.ID)
	require.NoError(t, err)
	check(got)
	listed, err = repo.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	check(&listed[0])
	got.Name = "Renamed synthetic review"
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.Get(ctx, recipe.ID, user.ID)
	require.NoError(t, err)
	check(got)
	listed, err = repo.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	check(&listed[0])
	var stored string
	require.NoError(t, tx.QueryRow(ctx, "SELECT spec::text FROM mcp_recipes WHERE id=$1", recipe.ID).Scan(&stored))
	require.JSONEq(t, raw, stored)
}
