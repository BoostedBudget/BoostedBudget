package services

import (
	"context"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestBudgetServiceAddsGroupRequest(t *testing.T) {
	repo := new(MockBudgetRepository)
	service := NewBudgetService(repo, new(MockCustomCategoryRepository), new(MockTransactionRepositoryForBudget), testLogger())
	ctx := context.Background()
	repo.On("GetByID", ctx, "budget-123").Return(createTestBudgetForService("user-123"), nil)
	repo.On("AddCategoryChecked", ctx, "budget-123", mock.Anything).Return(nil, nil)
	name := "Everyday"
	c, err := service.AddCategory(ctx, "user-123", "budget-123", &domain.CreateBudgetCategoryRequest{CategoryKind: "group", GroupName: &name, Members: []domain.BudgetGroupMember{{CategoryKind: "uncategorized"}}, AllocatedAmount: 100})
	require.NoError(t, err)
	require.Equal(t, &name, c.GroupName)
	require.Len(t, c.Members, 1)
}

func TestBudgetServiceEditsGroupAfterFinalMemberRetired(t *testing.T) {
	ctx := context.Background()
	repo := new(MockBudgetRepository)
	svc := NewBudgetService(repo, new(MockCustomCategoryRepository), new(MockTransactionRepositoryForBudget), testLogger())
	name, newName, amount := "Before", "After", 250.0
	budget := createTestBudgetForService("user-123")
	group := domain.BudgetCategory{ID: "group", BudgetID: "budget-123", CategoryKind: "group", GroupName: &name}
	budget.Categories = []domain.BudgetCategory{group}
	repo.On("GetByID", ctx, "budget-123").Return(budget, nil)
	repo.On("UpdateCategoryChecked", ctx, "budget-123", "group", mock.Anything).Return(&group, nil)
	updated, err := svc.UpdateCategory(ctx, "user-123", "budget-123", "group", &domain.UpdateBudgetCategoryRequest{GroupName: &newName, AllocatedAmount: &amount})
	require.NoError(t, err)
	require.Equal(t, "group", updated.ID)
	require.Equal(t, &newName, updated.GroupName)
	require.Equal(t, amount, updated.AllocatedAmount)
	require.Empty(t, updated.Members)
}

type budgetConfigurationLockTx struct {
	fakeTx
	locked bool
}

func (tx *budgetConfigurationLockTx) Exec(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		tx.locked = true
	}
	return pgconn.CommandTag{}, nil
}

func TestReparentReadsHierarchyAndBudgetUniverseAfterSerialization(t *testing.T) {
	ctx := context.Background()
	parent, child, food := "parent", "child", "FOOD_AND_DRINK"
	cr := new(MockCustomCategoryRepository)
	br := new(MockBudgetCategoryRepository)
	svc := NewCustomCategoryService(cr).(*customCategoryService)
	svc.SetBudgetCategoryRepository(br)
	tx := &budgetConfigurationLockTx{}
	br.On("BeginTx", ctx).Return(repository.Transaction(tx), nil)
	cr.On("GetChildren", ctx, child).Run(func(mock.Arguments) {
		require.True(t, tx.locked, "children must be resolved after concurrent hierarchy writes finish")
	}).Return([]*domain.CustomCategory{}, nil)
	cr.On("GetByID", ctx, parent).Run(func(mock.Arguments) {
		require.True(t, tx.locked, "proposed parent ancestry must be refreshed after the serialization lock")
	}).Return(&domain.CustomCategory{ID: parent, ParentPFCPrimary: &food}, nil)
	br.On("GetByUserID", ctx, "user").Run(func(mock.Arguments) {
		require.True(t, tx.locked, "budget enumeration must include a budget committed while reparent waited")
	}).Return([]*domain.Budget{{ID: "new-budget"}}, nil)
	br.On("GetCategoriesByBudgetIDLockedInTx", ctx, repository.Transaction(tx), "new-budget").Return([]domain.BudgetCategory{
		{CategoryKind: "group", Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}},
		{CategoryKind: "group", Members: []domain.BudgetGroupMember{{CategoryKind: "custom", CustomCategoryID: &child}}},
	}, nil)
	require.ErrorIs(t, svc.reparentCategoryChecked(ctx, "user", &domain.CustomCategory{ID: child, ParentID: &parent}), domain.ErrInvalidBudget)
}

func TestCustomReparentRefusesGroupOverlap(t *testing.T) {
	ctx := context.Background()
	food, id := "FOOD_AND_DRINK", "custom"
	cr := new(MockCustomCategoryRepository)
	br := new(MockBudgetCategoryRepository)
	svc := NewCustomCategoryService(cr)
	svc.SetBudgetCategoryRepository(br)
	tx := &fakeTx{}
	cr.On("GetChildren", ctx, id).Return([]*domain.CustomCategory{}, nil)
	br.On("GetByUserID", ctx, "user").Return([]*domain.Budget{{ID: "budget"}}, nil)
	br.On("BeginTx", ctx).Return(repository.Transaction(tx), nil)
	br.On("GetCategoriesByBudgetIDLockedInTx", ctx, repository.Transaction(tx), "budget").Return([]domain.BudgetCategory{
		{CategoryKind: "group", Members: []domain.BudgetGroupMember{{CategoryKind: "pfc", PFCPrimary: &food}}},
		{CategoryKind: "group", Members: []domain.BudgetGroupMember{{CategoryKind: "custom", CustomCategoryID: &id}}},
	}, nil)
	// If the guard is absent this writer succeeds, so the error assertion is the red signal.
	cr.On("UpdateInTx", ctx, repository.Transaction(tx), mock.Anything).Return(nil)
	err := svc.(*customCategoryService).reparentCategoryChecked(ctx, "user", &domain.CustomCategory{ID: id, ParentPFCPrimary: &food})
	require.ErrorIs(t, err, domain.ErrInvalidBudget)
	require.False(t, tx.committed)
}
