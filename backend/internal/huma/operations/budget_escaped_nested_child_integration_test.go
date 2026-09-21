//go:build integration

package operations

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEscapedNestedChildStillReturns400 pins that the #1685 barrier taxonomy did not
// weaken the pre-existing write-time hierarchy guard. POSTing a custom category whose
// match_logic escapes its containing parent's envelope must still return the same 400
// (domain.ErrEscapedNestedChild wraps domain.ErrInvalidBudget, which the mapper maps to
// 400) — not a 500, and not a silently-accepted write.
//
// The shape is the one the #1630 review fix established: a pfc root with a CONTAINING
// match_logic (category_only) and a custom category nested under it via
// parent_pfc_primary whose own match_logic is `or` (not contained).
func TestEscapedNestedChildStillReturns400(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID := seedActualsSyncBudget(t, tx, userID, 0)
	// The containing pfc root: category_only.
	seedActualsSyncCategory(t, tx, budgetID, "FOOD_AND_DRINK")

	// A custom category nested under that root via parent_pfc_primary.
	customID := uuid.New().String()
	_, err = tx.Exec(ctx, `INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary) VALUES ($1, $2, 'Nested', 'FOOD_AND_DRINK')`, customID, userID)
	require.NoError(t, err, "seed nested custom category")

	handler := newActualsSyncHandler(t, tx)

	out, err := handler.AddCategory(ctx, &AddCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
		Body: domain.CreateBudgetCategoryRequest{
			CategoryKind:     "custom",
			CustomCategoryID: &customID,
			MatchLogic:       domain.MatchLogicOr,
			AllocatedAmount:  25,
		},
	})
	require.Nil(t, out, "an escaping nested child must not be created")
	statusErr := budgetStatusErr(t, err)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
		"an escaping nested child must still return the pre-existing 400")
	assert.Contains(t, statusErr.Error(), "escapes its hierarchical parent's containment envelope",
		"the 400 must name the containment violation")

	var persisted int
	require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM budget_categories WHERE budget_id=$1 AND custom_category_id=$2 AND deleted_at IS NULL`, budgetID, customID).Scan(&persisted))
	assert.Equal(t, 0, persisted, "the refused escaping candidate must not have persisted")
}