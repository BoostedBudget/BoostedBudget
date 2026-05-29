package repository

import (
	"context"
	
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupCategoryMockDB creates a mock database for custom category repository testing
func setupCategoryMockDB(t *testing.T) (pgxmock.PgxPoolIface, CustomCategoryRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewCustomCategoryRepository(mock)
	return mock, repo
}

// TestCustomCategoryRepository_Create tests successful creation of a custom category
func TestCustomCategoryRepository_Create(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	color := "#FF5733"
	icon := "🏠"
	parent := "GENERAL_MERCHANDISE"

	category := &domain.CustomCategory{
		ID:             uuid.New().String(),
		UserID:         uuid.New().String(),
		Name:           "Home Improvement",
		ParentPFCPrimary: &parent,
		Icon:           &icon,
		Color:          &color,
		UsageCount:     0,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		category.ID,
		category.UserID,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, category)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_Create_DuplicateName tests creation failure for duplicate name
func TestCustomCategoryRepository_Create_DuplicateName(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	category := &domain.CustomCategory{
		UserID: uuid.New().String(),
		Name:   "Groceries",
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`)).WithArgs(
		pgxmock.AnyArg(),
		category.UserID,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnError(pgx.ErrNoRows)

	err := repo.Create(ctx, category)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetByID tests successful retrieval by ID
func TestCustomCategoryRepository_GetByID(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	userID := uuid.New().String()
	color := "#00FF00"
	now := time.Now()

	rows := pgxmock.NewRows([]string{"id", "user_id", "name", "parent_pfc_primary", "parent_id", "icon", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(categoryID, userID, "Electronics", nil, nil, nil, &color, 5, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE id = $1
	`)).WithArgs(categoryID).WillReturnRows(rows)

	category, err := repo.GetByID(ctx, categoryID)

	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, categoryID, category.ID)
	assert.Equal(t, "Electronics", category.Name)
	assert.Equal(t, 5, category.UsageCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetByID_NotFound tests retrieval when category doesn't exist
func TestCustomCategoryRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE id = $1
	`)).WithArgs(categoryID).WillReturnError(pgx.ErrNoRows)

	category, err := repo.GetByID(ctx, categoryID)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, category)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetByUserID tests successful retrieval of all user categories
func TestCustomCategoryRepository_GetByUserID(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{"id", "user_id", "name", "parent_pfc_primary", "parent_id", "icon", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(uuid.New().String(), userID, "Coffee", nil, nil, nil, nil, 10, now, now).
		AddRow(uuid.New().String(), userID, "Groceries", nil, nil, nil, nil, 5, now, now).
		AddRow(uuid.New().String(), userID, "Utilities", nil, nil, nil, nil, 3, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1
		ORDER BY name ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	categories, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, categories, 3)
	assert.Equal(t, "Coffee", categories[0].Name)
	assert.Equal(t, "Groceries", categories[1].Name)
	assert.Equal(t, "Utilities", categories[2].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetByName tests successful retrieval by user ID and name
func TestCustomCategoryRepository_GetByName(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{"id", "user_id", "name", "parent_pfc_primary", "parent_id", "icon", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(categoryID, userID, "Subscriptions", nil, nil, nil, nil, 2, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1 AND name = $2
	`)).WithArgs(userID, "Subscriptions").WillReturnRows(rows)

	category, err := repo.GetByName(ctx, userID, "Subscriptions")

	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, "Subscriptions", category.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_Update tests successful category update
func TestCustomCategoryRepository_Update(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	newColor := "#0000FF"
	newIcon := "🚗"

	category := &domain.CustomCategory{
		ID:         uuid.New().String(),
		UserID:     uuid.New().String(),
		Name:       "Transportation",
		Icon:       &newIcon,
		Color:      &newColor,
		UsageCount: 10,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE custom_categories
		SET name = $1, parent_pfc_primary = $2, parent_id = $3, icon = $4, color = $5, usage_count = $6, updated_at = $7
		WHERE id = $8
	`)).WithArgs(
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		category.ID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, category)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_Update_NotFound tests update when category doesn't exist
func TestCustomCategoryRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	category := &domain.CustomCategory{
		ID:   uuid.New().String(),
		Name: "nonexistent",
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE custom_categories
		SET name = $1, parent_pfc_primary = $2, parent_id = $3, icon = $4, color = $5, usage_count = $6, updated_at = $7
		WHERE id = $8
	`)).WithArgs(
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		category.ID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, category)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_Delete tests successful category deletion
func TestCustomCategoryRepository_Delete(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM custom_categories WHERE id = $1`)).
		WithArgs(categoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Delete(ctx, categoryID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_Delete_NotFound tests deletion when category doesn't exist
func TestCustomCategoryRepository_Delete_NotFound(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM custom_categories WHERE id = $1`)).
		WithArgs(categoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Delete(ctx, categoryID)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_CountByUserID tests counting categories for a user
func TestCustomCategoryRepository_CountByUserID(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(12)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`)).
		WithArgs(userID).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 12, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_IncrementUsageCount tests incrementing usage count
func TestCustomCategoryRepository_IncrementUsageCount(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryName := "Groceries"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE custom_categories
		SET usage_count = usage_count + 1, updated_at = $1
		WHERE user_id = $2 AND name = $3
	`)).WithArgs(
		pgxmock.AnyArg(),
		userID,
		categoryName,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.IncrementUsageCount(ctx, userID, categoryName)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_DecrementUsageCount tests decrementing usage count
func TestCustomCategoryRepository_DecrementUsageCount(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryName := "Entertainment"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE custom_categories
		SET usage_count = GREATEST(usage_count - 1, 0), updated_at = $1
		WHERE user_id = $2 AND name = $3
	`)).WithArgs(
		pgxmock.AnyArg(),
		userID,
		categoryName,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.DecrementUsageCount(ctx, userID, categoryName)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetNamesByIDs_EmptyInput tests that empty input returns empty map without DB roundtrip
func TestCustomCategoryRepository_GetNamesByIDs_EmptyInput(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	result, err := repo.GetNamesByIDs(ctx, userID, []string{})

	assert.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet()) // no DB calls expected
}

// TestCustomCategoryRepository_GetNamesByIDs_ValidIDs tests that valid IDs return id→name mapping
func TestCustomCategoryRepository_GetNamesByIDs_ValidIDs(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	id1 := uuid.New().String()
	id2 := uuid.New().String()
	unknownID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"id", "name"}).
		AddRow(id1, "Groceries").
		AddRow(id2, "Dining Out")

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
	`)).WithArgs(pgxmock.AnyArg(), userID).WillReturnRows(rows)

	result, err := repo.GetNamesByIDs(ctx, userID, []string{id1, id2, unknownID})

	require.NoError(t, err)
	assert.Equal(t, map[string]string{id1: "Groceries", id2: "Dining Out"}, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetNamesByIDs_DuplicateInput tests that duplicate input IDs are deduped
func TestCustomCategoryRepository_GetNamesByIDs_DuplicateInput(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	id1 := uuid.New().String()

	rows := pgxmock.NewRows([]string{"id", "name"}).AddRow(id1, "Travel")

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
	`)).WithArgs(pgxmock.AnyArg(), userID).WillReturnRows(rows)

	// Pass the same ID twice — should dedupe before querying
	result, err := repo.GetNamesByIDs(ctx, userID, []string{id1, id1})

	require.NoError(t, err)
	assert.Equal(t, map[string]string{id1: "Travel"}, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetNamesByIDs_CrossUser tests that IDs owned by another user are not returned
func TestCustomCategoryRepository_GetNamesByIDs_CrossUser(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userA := uuid.New().String()
	otherUserID := uuid.New().String()
	_ = otherUserID // represents cross-user ID: should not appear in result

	id1 := uuid.New().String()

	// DB returns empty rows because the ID belongs to a different user
	rows := pgxmock.NewRows([]string{"id", "name"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
	`)).WithArgs(pgxmock.AnyArg(), userA).WillReturnRows(rows)

	result, err := repo.GetNamesByIDs(ctx, userA, []string{id1})

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetChildren tests retrieval of direct children (Issue #1203)
func TestCustomCategoryRepository_GetChildren(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	parentID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{"id", "user_id", "name", "parent_pfc_primary", "parent_id", "icon", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(uuid.New().String(), userID, "Fast Food", nil, &parentID, nil, nil, 4, now, now).
		AddRow(uuid.New().String(), userID, "Fine Dining", nil, &parentID, nil, nil, 1, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE parent_id = $1
		ORDER BY name ASC
	`)).WithArgs(parentID).WillReturnRows(rows)

	children, err := repo.GetChildren(ctx, parentID)

	require.NoError(t, err)
	assert.Len(t, children, 2)
	assert.Equal(t, "Fast Food", children[0].Name)
	require.NotNil(t, children[0].ParentID)
	assert.Equal(t, parentID, *children[0].ParentID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_HasChildren tests the children-existence check (Issue #1203)
func TestCustomCategoryRepository_HasChildren(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	rows := pgxmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM custom_categories WHERE parent_id = $1)`)).
		WithArgs(id).WillReturnRows(rows)

	has, err := repo.HasChildren(ctx, id)

	require.NoError(t, err)
	assert.True(t, has)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_ClearParentForChildren tests reparenting children to top-level (Issue #1203)
func TestCustomCategoryRepository_ClearParentForChildren(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	parentID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE custom_categories SET parent_id = NULL, updated_at = $1 WHERE parent_id = $2`)).
		WithArgs(pgxmock.AnyArg(), parentID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := repo.ClearParentForChildren(ctx, parentID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
