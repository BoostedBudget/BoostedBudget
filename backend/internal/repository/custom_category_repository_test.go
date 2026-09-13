package repository

import (
	"context"

	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
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
		ID:               uuid.New().String(),
		UserID:           uuid.New().String(),
		Name:             "Home Improvement",
		ParentPFCPrimary: &parent,
		Icon:             &icon,
		Color:            &color,
		UsageCount:       0,
	}

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(category.UserID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
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
	mock.ExpectCommit()

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

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(category.UserID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
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
	mock.ExpectRollback()

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
		WHERE id = $1 AND deleted_at IS NULL
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
		WHERE id = $1 AND deleted_at IS NULL
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
		WHERE user_id = $1 AND deleted_at IS NULL
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
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
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

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(category.UserID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`UPDATE custom_categories[\s\S]*RETURNING parent_pfc_primary,parent_id`).WithArgs(
		category.Name,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		category.ID,
		category.UserID,
	).WillReturnRows(pgxmock.NewRows([]string{"parent_pfc_primary", "parent_id"}).AddRow(nil, nil))
	mock.ExpectCommit()

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

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(`UPDATE custom_categories[\s\S]*RETURNING parent_pfc_primary,parent_id`).WithArgs(
		category.Name,
		category.Icon,
		category.Color,
		category.UsageCount,
		pgxmock.AnyArg(),
		category.ID,
		category.UserID,
	).WillReturnRows(pgxmock.NewRows([]string{"parent_pfc_primary", "parent_id"}))
	mock.ExpectRollback()

	err := repo.Update(ctx, category)

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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1 AND deleted_at IS NULL`)).
		WithArgs(userID).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 12, count)
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
		WHERE parent_id = $1 AND deleted_at IS NULL
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
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM custom_categories WHERE parent_id = $1 AND deleted_at IS NULL)`)).
		WithArgs(id).WillReturnRows(rows)

	has, err := repo.HasChildren(ctx, id)

	require.NoError(t, err)
	assert.True(t, has)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_GetCategoryUsage returns live reference counts (Issue #1203).
func TestCustomCategoryRepository_GetCategoryUsage(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM transactions WHERE user_id = $1 AND custom_category_override_id = $2`)).
		WithArgs(userID, categoryID).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM budget_categories bc`).
		WithArgs(userID, categoryID).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM categorization_rules WHERE user_id = $1 AND target_custom_category_id = $2`)).
		WithArgs(userID, categoryID).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1 AND parent_id = $2 AND deleted_at IS NULL`)).
		WithArgs(userID, categoryID).WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))

	usage, err := repo.GetCategoryUsage(ctx, userID, categoryID)
	require.NoError(t, err)
	assert.Equal(t, 5, usage.TransactionCount)
	assert.Equal(t, 2, usage.BudgetCount)
	assert.Equal(t, 1, usage.RuleCount)
	assert.Equal(t, 3, usage.ChildCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_DeleteWithReassignment pins the SHAPE of the tombstone
// transaction: tombstone the category, disable (not delete, not blank) the rules that targeted
// it, reparent its children, tombstone its budget lines.
//
// ⚠ THIS TEST WAS INVERTED BY ISSUE #1569 TASK 9, NOT REWRITTEN FROM SCRATCH. It used to open with
//
//	mock.ExpectExec(`UPDATE transactions[\s\S]*category_source = 'plaid'[\s\S]*reviewed = false`)
//
// which pinned the bug as intended behaviour: deleting a category refiled every past transaction
// under Plaid's guess and wiped the user's `reviewed` state. There is now NO transactions
// statement in this transaction at all, and the assertion that proves it is
// mock.ExpectationsWereMet() over an expectation set that contains none — pgxmock fails an
// unexpected Exec, so a re-added revert turns this RED rather than passing unnoticed. The
// behavioural half (that the columns really do survive) is
// TestCategoryTombstone_DeleteKeepsHistoryAndReviewedState_Integration, because pgxmock never
// executes anything.
func TestCustomCategoryRepository_DeleteWithReassignment(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(userID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(`UPDATE custom_categories[\s\S]*SET deleted_at = NOW\(\)`).
		WithArgs(categoryID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec(`UPDATE categorization_rules[\s\S]*SET enabled = false`).
		WithArgs(userID, categoryID).WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	mock.ExpectExec(`UPDATE custom_categories SET parent_id = NULL`).
		WithArgs(userID, categoryID).WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	mock.ExpectExec(`UPDATE budget_categories[\s\S]*SET deleted_at = NOW\(\)[\s\S]*SELECT b.id FROM budgets b WHERE b.user_id = \$1`).
		WithArgs(userID, categoryID, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectQuery(`SELECT b.id FROM budgets b`).WithArgs(userID).WillReturnRows(pgxmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	// nil auditLog: the audit entry is optional at this layer, and its in-transaction write is
	// pinned against a real database by TestCategoryTombstone_DeleteIsAudited_Integration.
	err := repo.DeleteWithReassignment(ctx, userID, categoryID, nil)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCustomCategoryRepository_DeleteWithReassignment_NotFound rolls back, and does NOTHING else,
// when the tombstone UPDATE matches no row — the category is absent, another user's, or already
// tombstoned. The absence of any further expectation is the assertion: pgxmock fails an
// unexpected Exec, so a version that disabled rules or tombstoned budget lines for a category it
// never owned would turn this RED.
func TestCustomCategoryRepository_DeleteWithReassignment_NotFound(t *testing.T) {
	mock, repo := setupCategoryMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	categoryID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(userID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(`UPDATE custom_categories[\s\S]*SET deleted_at = NOW\(\)`).
		WithArgs(categoryID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	err := repo.DeleteWithReassignment(ctx, userID, categoryID, nil)
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}
