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

// setupLinkedItemMockDB creates a mock database and sqlmock instance for linked item repository testing
func setupLinkedItemMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, LinkedItemRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewLinkedItemRepository(db)
	return db, mock, repo
}

// TestLinkedItemRepository_Create tests successful creation of a linked item
func TestLinkedItemRepository_Create(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()

	linkedItem := &domain.LinkedItem{
		ID:                   uuid.New().String(),
		UserID:               uuid.New().String(),
		ItemID:               "plaid-item-123",
		AccessTokenEncrypted: "encrypted-base64-token",
		InstitutionID:        "ins_109508",
		InstitutionName:      "Chase",
		Provider:             "plaid",
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO linked_items
		(id, user_id, item_id, access_token_encrypted, institution_id, institution_name, provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)).WithArgs(
		linkedItem.ID,
		linkedItem.UserID,
		linkedItem.ItemID,
		linkedItem.AccessTokenEncrypted,
		linkedItem.InstitutionID,
		linkedItem.InstitutionName,
		linkedItem.Provider,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.Create(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserID tests successful retrieval of all linked items for a user
func TestLinkedItemRepository_GetByUserID(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), userID, "item-1", "encrypted-token-1",
			"ins_1", "Chase", "plaid", nil, nil,
			now, now,
		).
		AddRow(
			uuid.New().String(), userID, "item-2", "encrypted-token-2",
			"ins_2", "Wells Fargo", "plaid", "cursor-abc", &now,
			now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE user_id = $1
		ORDER BY created_at DESC
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	items, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, userID, items[0].UserID)
	assert.Equal(t, "Chase", items[0].InstitutionName)
	assert.Equal(t, "Wells Fargo", items[1].InstitutionName)
	assert.NotNil(t, items[1].Cursor)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByItemID tests retrieval of a specific linked item by item ID
func TestLinkedItemRepository_GetByItemID(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := "plaid-item-123"
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"created_at", "updated_at",
	}).AddRow(
		linkedItemID, userID, itemID, "encrypted-token",
		"ins_123", "Chase", "plaid", nil, nil,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE item_id = $1
	`)).WithArgs(itemID).WillReturnRows(rows)

	// Execute
	item, err := repo.GetByItemID(ctx, itemID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, linkedItemID, item.ID)
	assert.Equal(t, itemID, item.ItemID)
	assert.Equal(t, "Chase", item.InstitutionName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByItemID_NotFound tests when linked item is not found
func TestLinkedItemRepository_GetByItemID_NotFound(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := "nonexistent-item"

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE item_id = $1
	`)).WithArgs(itemID).WillReturnError(sql.ErrNoRows)

	// Execute
	item, err := repo.GetByItemID(ctx, itemID)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, item)
	assert.Equal(t, ErrLinkedItemNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_UpdateCursor tests updating the transaction sync cursor
func TestLinkedItemRepository_UpdateCursor(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	newCursor := "cursor-xyz-789"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE linked_items
		SET cursor = $1, last_synced_at = $2, updated_at = $3
		WHERE id = $4
	`)).WithArgs(
		newCursor,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		itemID,
	).WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute
	err := repo.UpdateCursor(ctx, itemID, newCursor)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_Delete tests deletion of a linked item (and cascade to accounts)
func TestLinkedItemRepository_Delete(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM linked_items
		WHERE id = $1
	`)).WithArgs(itemID).WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute
	err := repo.Delete(ctx, itemID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByIDAndUser_Success tests successful retrieval with ownership validation
func TestLinkedItemRepository_GetByIDAndUser_Success(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1 AND user_id = $2
	`

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider",
		"cursor", "last_synced_at", "created_at", "updated_at",
	}).AddRow(
		itemID, userID, "plaid-item-123", "encrypted-token",
		"ins_chase", "Chase", "plaid",
		sql.NullString{}, sql.NullTime{}, time.Now(), time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnRows(rows)

	// Execute
	item, err := repo.GetByIDAndUser(ctx, itemID, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, itemID, item.ID)
	assert.Equal(t, userID, item.UserID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByIDAndUser_NotFound tests when item doesn't exist
func TestLinkedItemRepository_GetByIDAndUser_NotFound(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1 AND user_id = $2
	`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnError(sql.ErrNoRows)

	// Execute
	item, err := repo.GetByIDAndUser(ctx, itemID, userID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.Nil(t, item)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByIDAndUser_NotOwner tests when user doesn't own the item
func TestLinkedItemRepository_GetByIDAndUser_NotOwner(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	wrongUserID := uuid.New().String()

	expectedQuery := `
		SELECT id, user_id, item_id, access_token_encrypted,
		       institution_id, institution_name, provider,
		       cursor, last_synced_at,
		       created_at, updated_at
		FROM linked_items
		WHERE id = $1 AND user_id = $2
	`

	// No rows returned (ownership check fails)
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, wrongUserID).
		WillReturnError(sql.ErrNoRows)

	// Execute
	item, err := repo.GetByIDAndUser(ctx, itemID, wrongUserID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.Nil(t, item)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_DeleteByUser_Success tests successful deletion with ownership validation
func TestLinkedItemRepository_DeleteByUser_Success(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected

	// Execute
	err := repo.DeleteByUser(ctx, itemID, userID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_DeleteByUser_NotFound tests deletion when item doesn't exist
func TestLinkedItemRepository_DeleteByUser_NotFound(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// Execute
	err := repo.DeleteByUser(ctx, itemID, userID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_DeleteByUser_NotOwner tests deletion when user doesn't own the item
func TestLinkedItemRepository_DeleteByUser_NotOwner(t *testing.T) {
	db, mock, repo := setupLinkedItemMockDB(t)
	defer db.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	wrongUserID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, wrongUserID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected (ownership check fails)

	// Execute
	err := repo.DeleteByUser(ctx, itemID, wrongUserID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}
