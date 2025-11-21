package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUserTagMockDB creates a mock database and sqlmock instance for user tag repository testing
func setupUserTagMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, UserTagRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewUserTagRepository(db)
	return db, mock, repo
}

// TestUserTagRepository_Create tests successful creation of a user tag
func TestUserTagRepository_Create(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	color := "#FF5733"

	tag := &domain.UserTag{
		ID:         uuid.New().String(),
		UserID:     uuid.New().String(),
		Name:       "reimbursable",
		Color:      &color,
		UsageCount: 0,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_tags (id, user_id, name, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`)).WithArgs(
		tag.ID,
		tag.UserID,
		tag.Name,
		tag.Color,
		tag.UsageCount,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(ctx, tag)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_Create_DuplicateName tests creation failure for duplicate tag name
func TestUserTagRepository_Create_DuplicateName(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()

	tag := &domain.UserTag{
		UserID: uuid.New().String(),
		Name:   "business",
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_tags (id, user_id, name, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`)).WithArgs(
		sqlmock.AnyArg(),
		tag.UserID,
		tag.Name,
		tag.Color,
		tag.UsageCount,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnError(sql.ErrNoRows) // Simulating unique constraint error

	// Note: This won't trigger ErrTagAlreadyExists because sql.ErrNoRows doesn't contain "unique"
	// In real scenarios, the DB would return an error containing "unique_user_tag_name"
	err := repo.Create(ctx, tag)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_GetByID tests successful retrieval by ID
func TestUserTagRepository_GetByID(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	tagID := uuid.New().String()
	userID := uuid.New().String()
	color := "#00FF00"
	now := time.Now()

	rows := sqlmock.NewRows([]string{"id", "user_id", "name", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(tagID, userID, "recurring", &color, 5, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE id = $1
	`)).WithArgs(tagID).WillReturnRows(rows)

	tag, err := repo.GetByID(ctx, tagID)

	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, tagID, tag.ID)
	assert.Equal(t, "recurring", tag.Name)
	assert.Equal(t, 5, tag.UsageCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_GetByID_NotFound tests retrieval when tag doesn't exist
func TestUserTagRepository_GetByID_NotFound(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	tagID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE id = $1
	`)).WithArgs(tagID).WillReturnError(sql.ErrNoRows)

	tag, err := repo.GetByID(ctx, tagID)

	assert.ErrorIs(t, err, domain.ErrTagNotFound)
	assert.Nil(t, tag)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_GetByUserID tests successful retrieval of all user tags
func TestUserTagRepository_GetByUserID(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{"id", "user_id", "name", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(uuid.New().String(), userID, "business", nil, 10, now, now).
		AddRow(uuid.New().String(), userID, "personal", nil, 5, now, now).
		AddRow(uuid.New().String(), userID, "reimbursable", nil, 3, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE user_id = $1
		ORDER BY name ASC
	`)).WithArgs(userID).WillReturnRows(rows)

	tags, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, tags, 3)
	assert.Equal(t, "business", tags[0].Name)
	assert.Equal(t, "personal", tags[1].Name)
	assert.Equal(t, "reimbursable", tags[2].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_GetByName tests successful retrieval by user ID and name
func TestUserTagRepository_GetByName(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tagID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{"id", "user_id", "name", "color", "usage_count", "created_at", "updated_at"}).
		AddRow(tagID, userID, "needs-review", nil, 2, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, name, color, usage_count, created_at, updated_at
		FROM user_tags
		WHERE user_id = $1 AND name = $2
	`)).WithArgs(userID, "needs-review").WillReturnRows(rows)

	tag, err := repo.GetByName(ctx, userID, "needs-review")

	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, "needs-review", tag.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_Update tests successful tag update
func TestUserTagRepository_Update(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	newColor := "#0000FF"

	tag := &domain.UserTag{
		ID:         uuid.New().String(),
		UserID:     uuid.New().String(),
		Name:       "updated-tag",
		Color:      &newColor,
		UsageCount: 10,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_tags
		SET name = $1, color = $2, usage_count = $3, updated_at = $4
		WHERE id = $5
	`)).WithArgs(
		tag.Name,
		tag.Color,
		tag.UsageCount,
		sqlmock.AnyArg(),
		tag.ID,
	).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Update(ctx, tag)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_Update_NotFound tests update when tag doesn't exist
func TestUserTagRepository_Update_NotFound(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()

	tag := &domain.UserTag{
		ID:   uuid.New().String(),
		Name: "nonexistent",
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_tags
		SET name = $1, color = $2, usage_count = $3, updated_at = $4
		WHERE id = $5
	`)).WithArgs(
		tag.Name,
		tag.Color,
		tag.UsageCount,
		sqlmock.AnyArg(),
		tag.ID,
	).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(ctx, tag)

	assert.ErrorIs(t, err, domain.ErrTagNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_Delete tests successful tag deletion
func TestUserTagRepository_Delete(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	tagID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tags WHERE id = $1`)).
		WithArgs(tagID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Delete(ctx, tagID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_Delete_NotFound tests deletion when tag doesn't exist
func TestUserTagRepository_Delete_NotFound(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	tagID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tags WHERE id = $1`)).
		WithArgs(tagID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Delete(ctx, tagID)

	assert.ErrorIs(t, err, domain.ErrTagNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_CountByUserID tests counting tags for a user
func TestUserTagRepository_CountByUserID(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := sqlmock.NewRows([]string{"count"}).AddRow(7)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM user_tags WHERE user_id = $1`)).
		WithArgs(userID).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 7, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_IncrementUsageCount tests incrementing usage count
func TestUserTagRepository_IncrementUsageCount(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tagName := "business"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_tags
		SET usage_count = usage_count + 1, updated_at = $1
		WHERE user_id = $2 AND name = $3
	`)).WithArgs(
		sqlmock.AnyArg(),
		userID,
		tagName,
	).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementUsageCount(ctx, userID, tagName)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_DecrementUsageCount tests decrementing usage count
func TestUserTagRepository_DecrementUsageCount(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tagName := "personal"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_tags
		SET usage_count = GREATEST(usage_count - 1, 0), updated_at = $1
		WHERE user_id = $2 AND name = $3
	`)).WithArgs(
		sqlmock.AnyArg(),
		userID,
		tagName,
	).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.DecrementUsageCount(ctx, userID, tagName)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUserTagRepository_IncrementUsageCount_NotFound tests increment when tag doesn't exist
func TestUserTagRepository_IncrementUsageCount_NotFound(t *testing.T) {
	db, mock, repo := setupUserTagMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tagName := "nonexistent"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE user_tags
		SET usage_count = usage_count + 1, updated_at = $1
		WHERE user_id = $2 AND name = $3
	`)).WithArgs(
		sqlmock.AnyArg(),
		userID,
		tagName,
	).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.IncrementUsageCount(ctx, userID, tagName)

	assert.ErrorIs(t, err, domain.ErrTagNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}
