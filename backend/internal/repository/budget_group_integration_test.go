//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBudgetGroupPersistence(t *testing.T) {
	ctx := context.Background()
	tx, err := connectTestDB(t).Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	user, budget := uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO budgets (id,user_id,name,period,start_date,amount,series_id) VALUES ($1,$2,'Grouped','monthly',CURRENT_DATE,1000,$1)`, budget, user)
	require.NoError(t, err)
	repo := NewBudgetRepository(tx)
	name, food, detail := "Everyday", "FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE"
	_, err = repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{ID: uuid.NewString(), BudgetID: budget, CategoryKind: "group", GroupName: &name})
	require.ErrorIs(t, err, domain.ErrInvalidBudget)
	newBudget, err := repo.GetByID(ctx, budget)
	require.NoError(t, err)
	newBudget.ID = uuid.NewString()
	newBudget.Name = "Empty group creation"
	newBudget.SeriesID = newBudget.ID
	err = repo.CreateWithCategories(ctx, newBudget, []*domain.BudgetCategory{{ID: uuid.NewString(), BudgetID: newBudget.ID, CategoryKind: "group", GroupName: &name}})
	require.ErrorIs(t, err, domain.ErrInvalidBudget)
	group := &domain.BudgetCategory{BudgetID: budget, CategoryKind: "group", GroupName: &name, AllocatedAmount: 300, Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}, {CategoryKind: "pfc", PFCPrimary: &food, PFCDetailed: &detail}}}
	created, err := repo.AddCategoryChecked(ctx, budget, group)
	require.NoError(t, err)
	require.Len(t, created.Members, 1)
	loaded, err := repo.GetCategoriesByBudgetID(ctx, budget)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, name, *loaded[0].GroupName)
	require.Len(t, loaded[0].Members, 1)
	_, err = repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "group", GroupName: &name, Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food, PFCDetailed: &detail}}})
	require.ErrorIs(t, err, domain.ErrInvalidBudget)
	_, err = repo.UpdateCategoryChecked(ctx, budget, created.ID, func(c *domain.BudgetCategory) error {
		c.Members = []domain.BudgetGroupMember{{CategoryKind: "uncategorized"}}
		return nil
	})
	require.NoError(t, err)
	var retired, live int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL),count(*) FILTER (WHERE deleted_at IS NULL) FROM budget_group_members WHERE budget_category_id=$1`, created.ID).Scan(&retired, &live))
	require.Equal(t, 1, retired)
	require.Equal(t, 1, live)
	other := &domain.BudgetCategory{BudgetID: budget, CategoryKind: "remainder"}
	_, err = repo.AddCategoryChecked(ctx, budget, other)
	require.NoError(t, err)
	_, err = repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "remainder"})
	require.ErrorIs(t, err, domain.ErrInvalidBudget)
	byID, err := repo.GetCategoryByIDAndUser(ctx, created.ID, user)
	require.NoError(t, err)
	require.Len(t, byID.Members, 1)
	batch, err := repo.GetCategoriesByBudgetIDs(ctx, []string{budget})
	require.NoError(t, err)
	require.Equal(t, loaded[0].GroupName, batch[budget][0].GroupName)
	custom := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Custom member')`, custom, user)
	require.NoError(t, err)
	customGroup, err := repo.AddCategoryChecked(ctx, budget, &domain.BudgetCategory{BudgetID: budget, CategoryKind: "group", GroupName: &name, Members: []domain.BudgetGroupMember{{CategoryKind: "custom", CustomCategoryID: &custom}}})
	require.NoError(t, err)
	// A metadata-only save from a stale client must not restore old ancestry.
	require.NoError(t, NewCustomCategoryRepository(tx).Update(ctx, &domain.CustomCategory{ID: custom, UserID: user, Name: "Renamed", ParentPFCPrimary: &food}))
	var persistedParent *string
	require.NoError(t, tx.QueryRow(ctx, `SELECT parent_pfc_primary FROM custom_categories WHERE id=$1 AND deleted_at IS NULL`, custom).Scan(&persistedParent))
	require.Nil(t, persistedParent)
	require.NoError(t, NewCustomCategoryRepository(tx).DeleteWithReassignment(ctx, user, custom, nil))
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL),count(*) FILTER (WHERE deleted_at IS NULL) FROM budget_group_members WHERE budget_category_id=$1`, customGroup.ID).Scan(&retired, &live))
	require.Equal(t, 1, retired)
	require.Zero(t, live)
	retiredGroupID := customGroup.ID
	newName := "Still editable"
	customGroup, err = repo.UpdateCategoryChecked(ctx, budget, retiredGroupID, func(c *domain.BudgetCategory) error {
		c.GroupName = &newName
		c.AllocatedAmount = 250
		return c.Validate()
	})
	require.NoError(t, err)
	require.Equal(t, retiredGroupID, customGroup.ID)
	require.Empty(t, customGroup.Members)
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL),count(*) FILTER (WHERE deleted_at IS NULL) FROM budget_group_members WHERE budget_category_id=$1`, retiredGroupID).Scan(&retired, &live))
	require.Equal(t, 1, retired)
	require.Zero(t, live)
	// A rollback must refuse retained group data, even when its final custom
	// member has been retired by the category deletion above.
	down, err := os.ReadFile("../../migrations/000205_budget_groups.down.sql")
	require.NoError(t, err)
	rollback, err := tx.Begin(ctx)
	require.NoError(t, err)
	_, err = rollback.Exec(ctx, string(down))
	require.ErrorContains(t, err, "Cannot roll back grouped budgets")
	require.NoError(t, rollback.Rollback(ctx))
	require.NoError(t, repo.DeleteCategory(ctx, created.ID, budget, nil))
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM budget_group_members WHERE budget_category_id=$1 AND deleted_at IS NULL`, created.ID).Scan(&live))
	require.Zero(t, live)
	_, err = tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, user)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM budget_group_members WHERE budget_category_id=ANY($1)`, []string{created.ID, customGroup.ID}).Scan(&live))
	require.Zero(t, live)
}

func TestBudgetConfigurationCreationSerializesWithReparent(t *testing.T) {
	ctx := context.Background()
	pool := connectTestDB(t)
	user := uuid.NewString()
	_, err := pool.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, user) })
	locked, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer locked.Rollback(ctx) //nolint:errcheck
	require.NoError(t, LockBudgetConfiguration(ctx, locked, user))
	for _, table := range []string{"budgets", "custom_categories"} {
		t.Run(table, func(t *testing.T) {
			writer, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer writer.Rollback(ctx) //nolint:errcheck
			_, err = writer.Exec(ctx, `SET LOCAL lock_timeout='100ms'`)
			require.NoError(t, err)
			id := uuid.NewString()
			if table == "budgets" {
				_, err = writer.Exec(ctx, `INSERT INTO budgets (id,user_id,name,period,start_date,amount,series_id) VALUES ($1,$2,'Concurrent','monthly',CURRENT_DATE,1000,$1)`, id, user)
			} else {
				_, err = writer.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Concurrent')`, id, user)
			}
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, "creation must wait until reparent's validated configuration commits")
			require.Equal(t, "55P03", pgErr.Code)
		})
	}
}
