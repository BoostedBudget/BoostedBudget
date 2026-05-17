package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCustomCategoryService is a mock implementation of services.CustomCategoryService
type mockCustomCategoryService struct {
	categories       []*domain.CustomCategory
	category         *domain.CustomCategory
	createErr        error
	getErr           error
	updateErr        error
	deleteErr        error
	getOrCreateCat   *domain.CustomCategory
	getOrCreateErr   error
}

func (m *mockCustomCategoryService) GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.categories, nil
}

func (m *mockCustomCategoryService) GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.category, nil
}

func (m *mockCustomCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.category, nil
}

func (m *mockCustomCategoryService) UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.category, nil
}

func (m *mockCustomCategoryService) DeleteCategory(ctx context.Context, userID, categoryID string) error {
	return m.deleteErr
}

func (m *mockCustomCategoryService) GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error) {
	if m.getOrCreateErr != nil {
		return nil, m.getOrCreateErr
	}
	return m.getOrCreateCat, nil
}

// SetJobQueue is a no-op for the mock (Issue #1123: satisfies updated interface).
func (m *mockCustomCategoryService) SetJobQueue(_ queue.JobQueue) {}

// Helper to create a test category
func createTestCategory(userID, name string) *domain.CustomCategory {
	now := time.Now()
	color := "#FF5733"
	icon := "shopping"
	return &domain.CustomCategory{
		ID:         "cat-123",
		UserID:     userID,
		Name:       name,
		Color:      &color,
		Icon:       &icon,
		UsageCount: 0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// TestGetUserCategories_Success tests successful retrieval of user categories.
func TestGetUserCategories_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCustomCategoryService{
		categories: []*domain.CustomCategory{
			createTestCategory(userID, "Groceries"),
			createTestCategory(userID, "Entertainment"),
		},
	}

	handler := NewCategoryHandler(mockService)

	input := &GetUserCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetUserCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "Groceries", output.Body[0].Name)
	assert.Equal(t, "Entertainment", output.Body[1].Name)
}

// TestGetUserCategories_EmptyList tests that empty array is returned instead of null.
func TestGetUserCategories_EmptyList(t *testing.T) {
	mockService := &mockCustomCategoryService{
		categories: nil,
	}

	handler := NewCategoryHandler(mockService)

	input := &GetUserCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetCategoryByID_Success tests successful retrieval of a category by ID.
func TestGetCategoryByID_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCustomCategoryService{
		category: createTestCategory(userID, "Groceries"),
	}

	handler := NewCategoryHandler(mockService)

	input := &GetCategoryByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "cat-123",
	}

	output, err := handler.GetCategoryByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Groceries", output.Body.Name)
	assert.Equal(t, "#FF5733", *output.Body.Color)
}

// TestGetCategoryByID_NotFound tests 404 when category doesn't exist.
func TestGetCategoryByID_NotFound(t *testing.T) {
	mockService := &mockCustomCategoryService{
		getErr: domain.ErrCategoryNotFound,
	}

	handler := NewCategoryHandler(mockService)

	input := &GetCategoryByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-cat",
	}

	output, err := handler.GetCategoryByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetCategoryByID_WrongOwner tests 404 when user doesn't own the category.
func TestGetCategoryByID_WrongOwner(t *testing.T) {
	mockService := &mockCustomCategoryService{
		category: createTestCategory("other-user", "Groceries"),
	}

	handler := NewCategoryHandler(mockService)

	input := &GetCategoryByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "cat-123",
	}

	output, err := handler.GetCategoryByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCreateCategory_Success tests successful category creation.
func TestCreateCategory_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCustomCategoryService{
		category: createTestCategory(userID, "New Category"),
	}

	handler := NewCategoryHandler(mockService)

	color := "#AABBCC"
	input := &CreateCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateCustomCategoryRequest{
			Name:  "New Category",
			Color: &color,
		},
	}

	output, err := handler.CreateCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "New Category", output.Body.Name)
}

// TestCreateCategory_LimitExceeded tests 403 when category limit is exceeded.
func TestCreateCategory_LimitExceeded(t *testing.T) {
	mockService := &mockCustomCategoryService{
		createErr: domain.ErrCategoryLimitExceeded,
	}

	handler := NewCategoryHandler(mockService)

	input := &CreateCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCustomCategoryRequest{
			Name: "New Category",
		},
	}

	output, err := handler.CreateCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestCreateCategory_DuplicateName tests 409 when category name already exists.
func TestCreateCategory_DuplicateName(t *testing.T) {
	mockService := &mockCustomCategoryService{
		createErr: domain.ErrCategoryAlreadyExists,
	}

	handler := NewCategoryHandler(mockService)

	input := &CreateCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCustomCategoryRequest{
			Name: "Existing Category",
		},
	}

	output, err := handler.CreateCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestUpdateCategory_Success tests successful category update.
func TestUpdateCategory_Success(t *testing.T) {
	userID := "user-123"
	updatedCat := createTestCategory(userID, "Updated Name")
	mockService := &mockCustomCategoryService{
		category: updatedCat,
	}

	handler := NewCategoryHandler(mockService)

	newName := "Updated Name"
	newColor := "#NEWCLR"
	input := &UpdateCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "cat-123",
		Body: domain.UpdateCustomCategoryRequest{
			Name:  &newName,
			Color: &newColor,
		},
	}

	output, err := handler.UpdateCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Name", output.Body.Name)
}

// TestUpdateCategory_NotFound tests 404 when updating nonexistent category.
func TestUpdateCategory_NotFound(t *testing.T) {
	mockService := &mockCustomCategoryService{
		updateErr: domain.ErrCategoryNotFound,
	}

	handler := NewCategoryHandler(mockService)

	newName := "Updated Name"
	input := &UpdateCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-cat",
		Body: domain.UpdateCustomCategoryRequest{
			Name: &newName,
		},
	}

	output, err := handler.UpdateCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDeleteCategory_Success tests successful category deletion.
func TestDeleteCategory_Success(t *testing.T) {
	mockService := &mockCustomCategoryService{
		deleteErr: nil,
	}

	handler := NewCategoryHandler(mockService)

	input := &DeleteCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "cat-123",
	}

	output, err := handler.DeleteCategory(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, output)
}

// TestDeleteCategory_NotFound tests 404 when deleting nonexistent category.
func TestDeleteCategory_NotFound(t *testing.T) {
	mockService := &mockCustomCategoryService{
		deleteErr: domain.ErrCategoryNotFound,
	}

	handler := NewCategoryHandler(mockService)

	input := &DeleteCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-cat",
	}

	output, err := handler.DeleteCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCategoryRouteRegistration tests that routes are properly registered.
func TestCategoryRouteRegistration(t *testing.T) {
	mockService := &mockCustomCategoryService{
		categories: []*domain.CustomCategory{},
	}

	_, api := humatest.New(t)
	handler := NewCategoryHandler(mockService)
	RegisterCategoryRoutes(api, handler)

	// Test GET /api/v1/categories (without auth, should fail with 401)
	resp := api.Get("/api/v1/categories")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestGetPlaidPrimaryCategories_ReturnsList verifies the handler returns all 18 PFCv2 primaries.
func TestGetPlaidPrimaryCategories_ReturnsList(t *testing.T) {
	mockService := &mockCustomCategoryService{}
	handler := NewCategoryHandler(mockService)

	input := &GetPlaidPrimaryCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPlaidPrimaryCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 18, "should return all 18 PFCv2 primaries")

	// Verify includes PFCv2 additions
	enumSet := make(map[string]string)
	for _, p := range output.Body {
		enumSet[p.Enum] = p.Display
	}
	assert.Equal(t, "Loan Disbursements", enumSet["LOAN_DISBURSEMENTS"])
	assert.Equal(t, "Other", enumSet["OTHER"])
}

// TestGetPlaidPrimaryCategories_RequiresAuth verifies the route is auth-protected.
func TestGetPlaidPrimaryCategories_RequiresAuth(t *testing.T) {
	mockService := &mockCustomCategoryService{}
	_, api := humatest.New(t)
	handler := NewCategoryHandler(mockService)
	RegisterCategoryRoutes(api, handler)

	resp := api.Get("/api/v1/categories/plaid-primaries")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Contains(t, resp.Body.String(), "User not authenticated")
}

// TestGetPlaidDetailedCategories_ReturnsList verifies the handler returns the full PFCv2 detailed taxonomy.
func TestGetPlaidDetailedCategories_ReturnsList(t *testing.T) {
	mockService := &mockCustomCategoryService{}
	handler := NewCategoryHandler(mockService)

	input := &GetPlaidDetailedCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPlaidDetailedCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Greater(t, len(output.Body), 100, "should return ~120+ detailed entries")

	// Verify LOAN_DISBURSEMENTS_AUTO is present (PFCv2 new primary)
	found := false
	for _, d := range output.Body {
		if d.Enum == "LOAN_DISBURSEMENTS_AUTO" {
			found = true
			assert.Equal(t, "LOAN_DISBURSEMENTS", d.PrimaryEnum)
			assert.Equal(t, "Loan Disbursements", d.PrimaryDisplay)
			assert.Equal(t, "Auto", d.Display)
		}
	}
	assert.True(t, found, "LOAN_DISBURSEMENTS_AUTO should be in the response")
}

// TestGetPlaidDetailedCategories_RequiresAuth verifies the route is auth-protected.
func TestGetPlaidDetailedCategories_RequiresAuth(t *testing.T) {
	mockService := &mockCustomCategoryService{}
	_, api := humatest.New(t)
	handler := NewCategoryHandler(mockService)
	RegisterCategoryRoutes(api, handler)

	resp := api.Get("/api/v1/categories/plaid-detailed")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Contains(t, resp.Body.String(), "User not authenticated")
}

// TestCategoryResponseJSON tests JSON serialization of response.
func TestCategoryResponseJSON(t *testing.T) {
	mockService := &mockCustomCategoryService{
		categories: []*domain.CustomCategory{},
	}

	_, api := humatest.New(t)
	handler := NewCategoryHandler(mockService)
	RegisterCategoryRoutes(api, handler)

	resp := api.Get("/api/v1/categories")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
