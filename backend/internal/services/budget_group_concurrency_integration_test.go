//go:build integration

package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestGroupReparentRechecksConcurrentConfiguration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	for _, scenario := range []string{"changed_parent_ancestry", "new_budget"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			admin, err := pgxpool.New(ctx, dsn)
			require.NoError(t, err)
			defer admin.Close()
			user, parent, child, budget := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			_, err = admin.Exec(ctx, `INSERT INTO users(id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES($1,$2,$3,'premium','active','iad')`, user, user, user+"@example.com")
			require.NoError(t, err)
			defer func() { _, _ = admin.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user) }()
			_, err = admin.Exec(ctx, `INSERT INTO custom_categories(id,user_id,name,parent_pfc_primary) VALUES($1,$2,'Parent','GENERAL_MERCHANDISE'),($3,$2,'Child',NULL)`, parent, user, child)
			require.NoError(t, err)
			food, name := "FOOD_AND_DRINK", "Shared"
			createBudget := func(q repository.DB) {
				b := &domain.Budget{ID: budget, UserID: user, Name: "Concurrent", Period: domain.BudgetPeriodMonthly, StartDate: time.Now(), Amount: 1000, CurrencyCode: "USD", IsActive: true, ExpectedIncome: 1000, SeriesID: budget}
				require.NoError(t, repository.NewBudgetRepository(q).CreateWithCategories(ctx, b, []*domain.BudgetCategory{
					{BudgetID: budget, CategoryKind: "group", GroupName: &name, Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}},
					{BudgetID: budget, CategoryKind: "group", GroupName: &name, Members: []domain.BudgetGroupMember{{CategoryKind: "custom", CustomCategoryID: &child}}},
				}))
			}
			if scenario == "changed_parent_ancestry" {
				createBudget(admin)
			}
			blocker, err := admin.Begin(ctx)
			require.NoError(t, err)
			defer blocker.Rollback(context.Background()) //nolint:errcheck
			require.NoError(t, repository.LockBudgetConfiguration(ctx, blocker, user))
			if scenario == "changed_parent_ancestry" {
				_, err = blocker.Exec(ctx, `SELECT id FROM budgets WHERE id=$1 FOR UPDATE`, budget)
				require.NoError(t, err)
			}
			config, err := pgxpool.ParseConfig(dsn)
			require.NoError(t, err)
			config.MaxConns = 1 // Every reparent read must use its existing transaction.
			config.ConnConfig.RuntimeParams["application_name"] = user
			serviceDB, err := pgxpool.NewWithConfig(ctx, config)
			require.NoError(t, err)
			defer serviceDB.Close()
			svc := NewCustomCategoryService(repository.NewCustomCategoryRepository(serviceDB)).(*customCategoryService)
			svc.SetBudgetCategoryRepository(repository.NewBudgetRepository(serviceDB))
			proposed := &domain.CustomCategory{ID: child, UserID: user, Name: "Child", ParentPFCPrimary: &food}
			if scenario == "changed_parent_ancestry" {
				proposed.ParentID = &parent
				proposed.ParentPFCPrimary = nil
			}
			result := make(chan error, 1)
			go func() { result <- svc.reparentCategoryChecked(ctx, user, proposed) }()
			// Observe the real lock wait instead of assuming a scheduling delay.
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				require.NoError(t, admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, user).Scan(&waiting))
				if waiting {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("reparent escaped serialization before concurrent change: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			if scenario == "changed_parent_ancestry" {
				_, err = blocker.Exec(ctx, `UPDATE custom_categories SET parent_pfc_primary=$2 WHERE id=$1`, parent, food)
				require.NoError(t, err)
			} else {
				createBudget(blocker)
			}
			require.NoError(t, blocker.Commit(ctx))
			select {
			case err := <-result:
				require.ErrorIs(t, err, domain.ErrInvalidBudget)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
