//go:build integration

package services

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestBudgetServiceCreateBudgetPersistsUnrelatedGroupMemberKinds(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := pool.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`, userID, "acceptance|"+userID, userID+"@example.invalid")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	customRepo := repository.NewCustomCategoryRepository(pool)
	custom := &domain.CustomCategory{UserID: userID, Name: "Owned custom selector"}
	require.NoError(t, customRepo.Create(ctx, custom))
	food := "FOOD_AND_DRINK"
	name := "Mixed selectors"
	members := []domain.BudgetGroupMember{
		{CategoryKind: "pfc", PFCPrimary: &food},
		{CategoryKind: "custom", CustomCategoryID: &custom.ID},
		{CategoryKind: "uncategorized"},
	}
	service := NewBudgetService(repository.NewBudgetRepository(pool), customRepo, nil, slog.Default())

	created, err := service.CreateBudget(ctx, userID, &domain.CreateBudgetRequest{
		Name: "Grouped budget acceptance", Period: domain.BudgetPeriodMonthly,
		StartDate: domain.FlexibleDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		Amount:    500, CurrencyCode: "USD",
		Categories: []domain.CreateBudgetCategoryRequest{{
			CategoryKind: "group", GroupName: &name, Members: members, AllocatedAmount: 300,
		}},
	})
	require.NoError(t, err)
	require.True(t, created.IsActive)
	require.Len(t, created.Categories, 1)

	persisted, err := repository.NewBudgetRepository(pool).GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, persisted.IsActive)
	require.Len(t, persisted.Categories, 1)
	group := persisted.Categories[0]
	require.Equal(t, "group", group.CategoryKind)
	require.NotNil(t, group.GroupName)
	require.Equal(t, name, *group.GroupName)
	require.ElementsMatch(t, members, group.Members)

	var memberCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM budget_group_members WHERE budget_category_id=$1 AND deleted_at IS NULL`, group.ID).Scan(&memberCount))
	require.Equal(t, 3, memberCount, "normalization must neither reject unrelated selectors nor add membership")
}
