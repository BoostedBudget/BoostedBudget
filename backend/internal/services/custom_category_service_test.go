package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockCustomCategoryRepository is a mock implementation of repository.CustomCategoryRepository
type MockCustomCategoryRepository struct {
	mock.Mock
}

func (m *MockCustomCategoryRepository) Create(ctx context.Context, category *domain.CustomCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) Update(ctx context.Context, category *domain.CustomCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

// UpdateInTx is a mock implementation (Issue #1630 B3b).
func (m *MockCustomCategoryRepository) UpdateInTx(ctx context.Context, tx repository.Transaction, category *domain.CustomCategory) error {
	args := m.Called(ctx, tx, category)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	args := m.Called(ctx, userID, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetLiveNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	args := m.Called(ctx, userID, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error) {
	args := m.Called(ctx, parentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) HasChildren(ctx context.Context, id string) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	args := m.Called(ctx, userID, categoryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategoryUsage), args.Error(1)
}

func (m *MockCustomCategoryRepository) DeleteWithReassignment(ctx context.Context, userID, categoryID string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, userID, categoryID, auditLog)
	return args.Error(0)
}

// TestCustomCategoryService_CreateCategory tests successful category creation
func TestCustomCategoryService_CreateCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateCustomCategoryRequest{
		Name: "Groceries",
	}

	// Setup mocks - categories are unlimited, so we just need Create
	mockCategoryRepo.On("Create", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.CreateCategory(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, "Groceries", category.Name)
	assert.Equal(t, userID, category.UserID)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_CreateCategory_WithCustomParent tests nesting under a
// valid, owned, top-level custom parent (Issue #1203).
func TestCustomCategoryService_CreateCategory_WithCustomParent(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parentID := "11111111-1111-1111-1111-111111111111"

	req := &domain.CreateCustomCategoryRequest{Name: "Fast Food", ParentID: &parentID}

	parent := &domain.CustomCategory{ID: parentID, UserID: userID, Name: "Food"}
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(parent, nil)
	mockCategoryRepo.On("Create", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	category, err := service.CreateCategory(ctx, userID, req)

	assert.NoError(t, err)
	require.NotNil(t, category)
	require.NotNil(t, category.ParentID)
	assert.Equal(t, parentID, *category.ParentID)
	require.NotNil(t, category.ParentName)
	assert.Equal(t, "Food", *category.ParentName)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_CreateCategory_NormalizesEmptyPfcParent verifies that an
// empty-string parent_pfc_primary (sent by the form when nesting under a custom parent)
// is normalized to nil so it doesn't violate the mutual-exclusivity CHECK (Issue #1203).
func TestCustomCategoryService_CreateCategory_NormalizesEmptyPfcParent(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parentID := "11111111-1111-1111-1111-111111111111"
	emptyPfc := ""

	req := &domain.CreateCustomCategoryRequest{
		Name:             "Fast Food",
		ParentID:         &parentID,
		ParentPFCPrimary: &emptyPfc, // form sends "" for the unused dimension
	}

	mockCategoryRepo.On("GetByID", ctx, parentID).
		Return(&domain.CustomCategory{ID: parentID, UserID: userID, Name: "Food"}, nil)
	mockCategoryRepo.On("Create", ctx, mock.MatchedBy(func(c *domain.CustomCategory) bool {
		// parent_pfc_primary must be nil (not an empty string) so the CHECK passes.
		return c.ParentPFCPrimary == nil && c.ParentID != nil && *c.ParentID == parentID
	})).Return(nil)

	category, err := service.CreateCategory(ctx, userID, req)

	require.NoError(t, err)
	require.NotNil(t, category)
	assert.Nil(t, category.ParentPFCPrimary)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_CreateCategory_RejectsParentWithParent enforces the
// 2-level limit: a parent cannot itself have a parent (Issue #1203).
func TestCustomCategoryService_CreateCategory_RejectsParentWithParent(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parentID := "11111111-1111-1111-1111-111111111111"
	grandparentID := "22222222-2222-2222-2222-222222222222"

	req := &domain.CreateCustomCategoryRequest{Name: "Espresso", ParentID: &parentID}

	parentThatHasParent := &domain.CustomCategory{ID: parentID, UserID: userID, Name: "Fast Food", ParentID: &grandparentID}
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(parentThatHasParent, nil)

	category, err := service.CreateCategory(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryParentHasParent)
	assert.Nil(t, category)
	mockCategoryRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_CreateCategory_RejectsParentNotOwned prevents nesting
// under another user's category (Issue #1203).
func TestCustomCategoryService_CreateCategory_RejectsParentNotOwned(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parentID := "11111111-1111-1111-1111-111111111111"

	req := &domain.CreateCustomCategoryRequest{Name: "Coffee", ParentID: &parentID}

	otherUsersParent := &domain.CustomCategory{ID: parentID, UserID: "other-user", Name: "Food"}
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(otherUsersParent, nil)

	category, err := service.CreateCategory(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryInvalidParent)
	assert.Nil(t, category)
	mockCategoryRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestCustomCategoryService_UpdateCategory_RejectsNestingWhenHasChildren prevents
// nesting a category that itself has children (2-level limit, Issue #1203).
func TestCustomCategoryService_UpdateCategory_RejectsNestingWhenHasChildren(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "33333333-3333-3333-3333-333333333333"
	parentID := "11111111-1111-1111-1111-111111111111"

	existing := &domain.CustomCategory{ID: categoryID, UserID: userID, Name: "Food"}
	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existing, nil)
	mockCategoryRepo.On("HasChildren", ctx, categoryID).Return(true, nil)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &parentID}
	category, err := service.UpdateCategory(ctx, userID, categoryID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryHasChildren)
	assert.Nil(t, category)
	mockCategoryRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestCustomCategoryService_CreateCategory_ManyCategories tests creating categories with many existing (unlimited)
func TestCustomCategoryService_CreateCategory_ManyCategories(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateCustomCategoryRequest{
		Name: "unlimited-category",
	}

	// Categories are unlimited for all tiers
	mockCategoryRepo.On("Create", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.CreateCategory(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetUserCategories tests retrieving all user categories
func TestCustomCategoryService_GetUserCategories(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	expectedCategories := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "Groceries"},
		{ID: "cat-2", UserID: userID, Name: "Entertainment"},
	}

	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(expectedCategories, nil)

	// Execute
	categories, err := service.GetUserCategories(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, categories, 2)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory tests successful category update
func TestCustomCategoryService_UpdateCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Updated Name"
	newColor := "#FF0000"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Old Name",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name:  &newName,
		Color: &newColor,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.UpdateCategory(ctx, userID, categoryID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, newName, category.Name)
	assert.Equal(t, &newColor, category.Color)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_WrongOwner tests update attempt by wrong user
func TestCustomCategoryService_UpdateCategory_WrongOwner(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	wrongUserID := "user-456"
	categoryID := "cat-789"
	newName := "hacker-name"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID, // Different from wrongUserID
		Name:   "Original",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)

	// Execute - attempt to update with wrong user ID
	category, err := service.UpdateCategory(ctx, wrongUserID, categoryID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, category)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_DeleteCategory tests successful category deletion
func TestCustomCategoryService_DeleteCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "to-delete",
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	// nil audit repo: SetAuditLogRepository is not wired in this test, and the delete must still
	// work without one (#1569 Task 9).
	mockCategoryRepo.On("DeleteWithReassignment", ctx, userID, categoryID, repository.AuditLogRepository(nil)).Return(nil)

	// Execute
	err := service.DeleteCategory(ctx, userID, categoryID)

	// Assert
	assert.NoError(t, err)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetCategoryUsage verifies ownership check + live counts (Issue #1203).
func TestCustomCategoryService_GetCategoryUsage(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"

	mockCategoryRepo.On("GetByID", ctx, categoryID).
		Return(&domain.CustomCategory{ID: categoryID, UserID: userID, Name: "Food"}, nil)
	mockCategoryRepo.On("GetCategoryUsage", ctx, userID, categoryID).
		Return(&domain.CategoryUsage{TransactionCount: 3, BudgetCount: 1, RuleCount: 2, ChildCount: 1}, nil)

	usage, err := service.GetCategoryUsage(ctx, userID, categoryID)

	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TransactionCount)
	assert.Equal(t, 1, usage.ChildCount)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetCategoryUsage_NotOwned hides another user's category.
func TestCustomCategoryService_GetCategoryUsage_NotOwned(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	categoryID := "cat-456"
	mockCategoryRepo.On("GetByID", ctx, categoryID).
		Return(&domain.CustomCategory{ID: categoryID, UserID: "other-user", Name: "Food"}, nil)

	usage, err := service.GetCategoryUsage(ctx, "user-123", categoryID)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, usage)
	mockCategoryRepo.AssertNotCalled(t, "GetCategoryUsage", mock.Anything, mock.Anything, mock.Anything)
}

// TestCustomCategoryService_GetUserCategories_PopulatesPFCDisplay asserts that GetUserCategories
// enriches each category with ParentPFCPrimaryDisplay before returning.
func TestCustomCategoryService_GetUserCategories_PopulatesPFCDisplay(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parent := "FOOD_AND_DRINK"

	categories := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "My Food", ParentPFCPrimary: &parent},
	}

	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(categories, nil)

	result, err := service.GetUserCategories(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].ParentPFCPrimaryDisplay, "ParentPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result[0].ParentPFCPrimaryDisplay)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetCategoryByID_PopulatesPFCDisplay asserts that GetCategoryByID
// enriches the category with ParentPFCPrimaryDisplay before returning.
func TestCustomCategoryService_GetCategoryByID_PopulatesPFCDisplay(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	categoryID := "cat-pfc"
	parent := "TRANSPORTATION"

	category := &domain.CustomCategory{
		ID:               categoryID,
		UserID:           "user-123",
		Name:             "My Transport",
		ParentPFCPrimary: &parent,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(category, nil)

	result, err := service.GetCategoryByID(ctx, categoryID)

	require.NoError(t, err)
	require.NotNil(t, result.ParentPFCPrimaryDisplay, "ParentPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Transportation", *result.ParentPFCPrimaryDisplay)
	mockCategoryRepo.AssertExpectations(t)
}

// ===========================================================================
// Issue #1123: Effective category name cascade rename tests
// ===========================================================================

// TestCustomCategoryService_UpdateCategory_EnqueuesRefreshJobOnRename asserts that
// when the category name changes, an effectiveCategoryRefreshJobArgs job is enqueued.
func TestCustomCategoryService_UpdateCategory_EnqueuesRefreshJobOnRename(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	oldName := "Fast Food"
	newName := "Dining Out"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   oldName,
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)
	mockQueue.On("Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			args, ok := job.(effectiveCategoryRefreshJobArgs)
			return ok &&
				args.UserID == userID &&
				args.CustomCategoryID == categoryID
		}),
		mock.Anything,
	).Return(&queue.JobResult{ID: 1, Kind: "effective_category_refresh"}, nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_NoJobWhenNameUnchanged asserts that
// no job is enqueued when the update request does not change the name.
func TestCustomCategoryService_UpdateCategory_NoJobWhenNameUnchanged(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	sameName := "Groceries"
	newColor := "#00FF00"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   sameName,
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name:  &sameName, // Same name — no rename
		Color: &newColor,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, sameName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	// Enqueue must NOT have been called.
	mockQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestCustomCategoryService_UpdateCategory_NoJobWhenQueueNotSet asserts that
// when SetJobQueue has not been called, a rename still succeeds without panicking.
func TestCustomCategoryService_UpdateCategory_NoJobWhenQueueNotSet(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	// No SetJobQueue call — jobQueue is nil.
	svc := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Renamed Category"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Old Name",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_EnqueueErrorDoesNotFail asserts that
// when enqueuing fails, the UpdateCategory call still succeeds (best-effort enqueue).
func TestCustomCategoryService_UpdateCategory_EnqueueErrorDoesNotFail(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Renamed"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Original",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)
	mockQueue.On("Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			_, ok := job.(effectiveCategoryRefreshJobArgs)
			return ok
		}),
		mock.Anything,
	).Return(nil, assert.AnError)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	// Rename persisted — enqueue failure does not propagate.
	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestEffectiveCategoryRefreshJobArgs_Kind(t *testing.T) {
	args := effectiveCategoryRefreshJobArgs{}
	assert.Equal(t, "effective_category_refresh", args.Kind(), "Kind() must return the expected job type identifier")
}

func TestBudgetHierarchyResyncJobArgs_Kind(t *testing.T) {
	args := budgetHierarchyResyncJobArgs{}
	assert.Equal(t, "budget_hierarchy_resync", args.Kind(), "Kind() must return the expected job type identifier")
}

// MockBudgetCategoryRepository is a mock implementation of services.BudgetCategoryRepository
// (Issue #1630 Task B1).
type MockBudgetCategoryRepository struct {
	mock.Mock
}

func (m *MockBudgetCategoryRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

func (m *MockBudgetCategoryRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

// BeginTx is a mock implementation (Issue #1630 B3b).
func (m *MockBudgetCategoryRepository) BeginTx(ctx context.Context) (repository.Transaction, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(repository.Transaction), args.Error(1)
}

// GetCategoriesByBudgetIDLockedInTx is a mock implementation (Issue #1630 B3b).
func (m *MockBudgetCategoryRepository) GetCategoriesByBudgetIDLockedInTx(ctx context.Context, tx repository.Transaction, budgetID string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, tx, budgetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

// TestCustomCategoryService_UpdateCategory_ReparentFailsClosedWithoutBudgetCategoryRepo asserts
// that a reparent is REFUSED — not silently allowed — when SetBudgetCategoryRepository was never
// called. This is money-integrity enforcement (Issue #1630 Task B1), not observability: contrast
// SetAuditLogRepository, whose absence is a safe no-op (fail-open by design).
func TestCustomCategoryService_UpdateCategory_ReparentFailsClosedWithoutBudgetCategoryRepo(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	svc := NewCustomCategoryService(mockCategoryRepo)
	// Deliberately no SetBudgetCategoryRepository call.

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	parentID := "11111111-1111-1111-1111-111111111111"

	existingCategory := &domain.CustomCategory{ID: categoryID, UserID: userID, Name: "Fast Food"}
	parent := &domain.CustomCategory{ID: parentID, UserID: userID, Name: "Food"}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(parent, nil)
	mockCategoryRepo.On("HasChildren", ctx, categoryID).Return(false, nil)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &parentID}
	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	assert.Error(t, err)
	assert.Nil(t, category)
	assert.Contains(t, err.Error(), "budget category repository not configured")
	mockCategoryRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestCustomCategoryService_UpdateCategory_ReparentRefusesEscapingBudgetedPair asserts that a
// reparent is refused when it would place a BUDGETED row into a not-cleanly-contained nested
// pair — checking the PROPOSED hierarchy (the parent this request is setting), not the stale one
// GetCategoriesByBudgetID's join still reflects.
func TestCustomCategoryService_UpdateCategory_ReparentRefusesEscapingBudgetedPair(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockBudgetCategoryRepo := new(MockBudgetCategoryRepository)
	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetBudgetCategoryRepository(mockBudgetCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-child"
	parentID := "cat-parent"
	budgetID := "budget-1"

	existingCategory := &domain.CustomCategory{ID: categoryID, UserID: userID, Name: "Fast Food"}
	parent := &domain.CustomCategory{ID: parentID, UserID: userID, Name: "Food"}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(parent, nil)
	mockCategoryRepo.On("HasChildren", ctx, categoryID).Return(false, nil)
	mockCategoryRepo.On("GetChildren", ctx, categoryID).Return([]*domain.CustomCategory{}, nil)

	mockBudgetCategoryRepo.On("GetActiveByUserID", ctx, userID).
		Return([]*domain.Budget{{ID: budgetID, UserID: userID}}, nil)

	// Issue #1630 B3b: the guard and the (would-be) write now share ONE transaction.
	tx := &fakeTx{}
	mockBudgetCategoryRepo.On("BeginTx", ctx).Return(repository.Transaction(tx), nil)
	// Parent's match_logic is 'and' — NOT a containing shape (isContainingMatchLogic requires
	// category_only/or) — so nesting the child under it escapes clean containment.
	mockBudgetCategoryRepo.On("GetCategoriesByBudgetIDLockedInTx", ctx, repository.Transaction(tx), budgetID).Return([]domain.BudgetCategory{
		{
			ID: "bc-parent", BudgetID: budgetID, CategoryKind: "custom",
			CustomCategoryID: &parentID, MatchLogic: domain.MatchLogicAnd, Tags: []string{"tag"},
		},
		{
			ID: "bc-child", BudgetID: budgetID, CategoryKind: "custom",
			CustomCategoryID: &categoryID, MatchLogic: domain.MatchLogicCategoryOnly,
		},
	}, nil)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &parentID}
	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)
	assert.Nil(t, category)
	mockCategoryRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertNotCalled(t, "UpdateInTx", mock.Anything, mock.Anything, mock.Anything)
	mockBudgetCategoryRepo.AssertExpectations(t)
	// The guard refused before the write — the transaction must be ROLLED BACK, never committed.
	assert.True(t, tx.rolledBack, "expected the shared transaction to be rolled back on guard refusal")
	assert.False(t, tx.committed, "expected the shared transaction NOT to be committed on guard refusal")
}

// TestCustomCategoryService_UpdateCategory_ReparentEnqueuesResyncJob asserts that a LEGITIMATE
// reparent (both sides cleanly contained) persists, enqueues the NEW budget_hierarchy_resync job
// carrying the PRE-reparent parent identity, and does NOT enqueue effective_category_refresh
// (name unchanged).
func TestCustomCategoryService_UpdateCategory_ReparentEnqueuesResyncJob(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockBudgetCategoryRepo := new(MockBudgetCategoryRepository)
	mockQueue := &MockJobQueue{}
	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetBudgetCategoryRepository(mockBudgetCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-child"
	parentID := "cat-parent"
	oldParentPFC := "FOOD_AND_DRINK"

	existingCategory := &domain.CustomCategory{
		ID: categoryID, UserID: userID, Name: "Fast Food",
		ParentPFCPrimary: &oldParentPFC, // pre-reparent: nested under a PFC primary
	}
	parent := &domain.CustomCategory{ID: parentID, UserID: userID, Name: "Food"}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("GetByID", ctx, parentID).Return(parent, nil)
	mockCategoryRepo.On("HasChildren", ctx, categoryID).Return(false, nil)
	mockCategoryRepo.On("GetChildren", ctx, categoryID).Return([]*domain.CustomCategory{}, nil)
	mockCategoryRepo.On("UpdateInTx", ctx, mock.Anything, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// No budgets reference this category at all — nothing for the guard to refuse.
	mockBudgetCategoryRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.Budget{}, nil)

	// Issue #1630 B3b: the guard and the write now share ONE transaction — even with zero
	// budgets to lock, reparentCategoryChecked still opens it to persist the write and commit.
	tx := &fakeTx{}
	mockBudgetCategoryRepo.On("BeginTx", ctx).Return(repository.Transaction(tx), nil)

	mockQueue.On("Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			args, ok := job.(budgetHierarchyResyncJobArgs)
			return ok &&
				args.UserID == userID &&
				args.CustomCategoryID == categoryID &&
				args.OldParentCustomCategoryID == nil &&
				args.OldParentPFCPrimary != nil && *args.OldParentPFCPrimary == oldParentPFC
		}),
		mock.Anything,
	).Return(&queue.JobResult{ID: 1, Kind: "budget_hierarchy_resync"}, nil)

	req := &domain.UpdateCustomCategoryRequest{ParentID: &parentID}
	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	require.NotNil(t, category)
	require.NotNil(t, category.ParentID)
	assert.Equal(t, parentID, *category.ParentID)
	mockCategoryRepo.AssertExpectations(t)
	mockBudgetCategoryRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
	assert.True(t, tx.committed, "expected the shared transaction to be committed on a legitimate reparent")
	// effective_category_refresh must NOT be enqueued — the name didn't change.
	mockQueue.AssertNotCalled(t, "Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			_, ok := job.(effectiveCategoryRefreshJobArgs)
			return ok
		}),
		mock.Anything,
	)
}
