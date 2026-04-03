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

// setupLinkedItemMockDB creates a mock database and sqlmock instance for linked item repository testing
func setupLinkedItemMockDB(t *testing.T) (pgxmock.PgxPoolIface, LinkedItemRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewLinkedItemRepository(mock)
	return mock, repo
}

// TestLinkedItemRepository_Create tests successful creation of a linked item
func TestLinkedItemRepository_Create(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

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
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserID tests successful retrieval of all linked items for a user
func TestLinkedItemRepository_GetByUserID(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helper for nullable string
	cursor := "cursor-abc"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).
		AddRow(
			uuid.New().String(), userID, "item-1", "encrypted-token-1",
			"ins_1", "Chase", "plaid", nil, nil,
			"active", nil, nil, nil, 0, nil,
			nil, nil,
			now, now,
			nil, nil,
		).
		AddRow(
			uuid.New().String(), userID, "item-2", "encrypted-token-2",
			"ins_2", "Wells Fargo", "plaid", &cursor, &now,
			"active", nil, nil, nil, 0, nil,
			nil, nil,
			now, now,
			nil, nil,
		)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id").
		WithArgs(userID).WillReturnRows(rows)

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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := "plaid-item-123"
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).AddRow(
		linkedItemID, userID, itemID, "encrypted-token",
		"ins_123", "Chase", "plaid", nil, nil,
		"active", nil, nil, nil, 0, nil,
		nil, nil,
		now, now,
		nil, nil,
	)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE item_id").
		WithArgs(itemID).WillReturnRows(rows)

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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := "nonexistent-item"

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE item_id").
		WithArgs(itemID).WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	newCursor := "cursor-xyz-789"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE linked_items
		SET cursor = $1, last_synced_at = $2, updated_at = $3
		WHERE id = $4
	`)).WithArgs(
		newCursor,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		itemID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.UpdateCursor(ctx, itemID, newCursor)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_Delete tests deletion of a linked item (and cascade to accounts)
func TestLinkedItemRepository_Delete(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM linked_items
		WHERE id = $1
	`)).WithArgs(itemID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.Delete(ctx, itemID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByIDAndUser_Success tests successful retrieval with ownership validation
func TestLinkedItemRepository_GetByIDAndUser_Success(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).AddRow(
		itemID, userID, "plaid-item-123", "encrypted-token",
		"ins_chase", "Chase", "plaid", nil, nil,
		"active", nil, nil, nil, 0, nil,
		nil, nil,
		now, now,
		nil, nil,
	)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE id.*AND user_id").
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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE id.*AND user_id").
		WithArgs(itemID, userID).
		WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	wrongUserID := uuid.New().String()

	// No rows returned (ownership check fails)
	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE id.*AND user_id").
		WithArgs(itemID, wrongUserID).
		WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1)) // 1 row affected

	// Execute
	err := repo.DeleteByUser(ctx, itemID, userID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_DeleteByUser_NotFound tests deletion when item doesn't exist
func TestLinkedItemRepository_DeleteByUser_NotFound(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	userID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected

	// Execute
	err := repo.DeleteByUser(ctx, itemID, userID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_DeleteByUser_NotOwner tests deletion when user doesn't own the item
func TestLinkedItemRepository_DeleteByUser_NotOwner(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	wrongUserID := uuid.New().String()

	expectedQuery := `DELETE FROM linked_items WHERE id = $1 AND user_id = $2`

	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(itemID, wrongUserID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected (ownership check fails)

	// Execute
	err := repo.DeleteByUser(ctx, itemID, wrongUserID)

	// Assert
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// Issue #171: Re-link Handling Tests
// ============================================================================

// TestLinkedItemRepository_GetByUserAndInstitution_Active tests finding active linked items by institution
func TestLinkedItemRepository_GetByUserAndInstitution_Active(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"
	itemID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).
		AddRow(
			itemID, userID, "item-1", "encrypted-token",
			institutionID, "Chase", "plaid", nil, nil,
			"active", nil, nil, nil, 0, nil,
			nil, nil,
			now, now,
			nil, nil,
		)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status").
		WithArgs(userID, institutionID, "active").
		WillReturnRows(rows)

	// Execute - this method should exist for re-link detection
	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "active")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, itemID, item.ID)
	assert.Equal(t, institutionID, item.InstitutionID)
	assert.Equal(t, domain.LinkedItemStatusActive, item.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserAndInstitution_NotFound tests when no matching item exists
func TestLinkedItemRepository_GetByUserAndInstitution_NotFound(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_different"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	})

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status").
		WithArgs(userID, institutionID, "active").
		WillReturnRows(rows)

	// Execute
	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "active")

	// Assert - should return nil, nil (not an error) when no match found
	assert.NoError(t, err)
	assert.Nil(t, item)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserAndInstitution_Replaced tests finding replaced items
func TestLinkedItemRepository_GetByUserAndInstitution_Replaced(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"
	itemID := uuid.New().String()
	replacedByID := uuid.New().String()
	now := time.Now()
	replacedAt := now.Add(-time.Hour)

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).
		AddRow(
			itemID, userID, "item-old", "encrypted-token",
			institutionID, "Chase", "plaid", nil, nil,
			"replaced", nil, nil, nil, 0, nil,
			&replacedAt, &replacedByID,
			now, now,
			nil, nil,
		)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status").
		WithArgs(userID, institutionID, "replaced").
		WillReturnRows(rows)

	// Execute
	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "replaced")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, domain.LinkedItemStatusReplaced, item.Status)
	assert.NotNil(t, item.ReplacedAt)
	assert.NotNil(t, item.ReplacedByID)
	assert.Equal(t, replacedByID, *item.ReplacedByID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_Update_WithSoftDeleteFields tests updating with replaced_at and replaced_by_id
func TestLinkedItemRepository_Update_WithSoftDeleteFields(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := uuid.New().String()
	replacedByID := uuid.New().String()
	now := time.Now()

	item := &domain.LinkedItem{
		ID:              itemID,
		UserID:          uuid.New().String(),
		ItemID:          "item-123",
		InstitutionID:   "ins_chase",
		InstitutionName: "Chase",
		Provider:        "plaid",
		Status:          domain.LinkedItemStatusReplaced,
		ReplacedAt:      &now,
		ReplacedByID:    &replacedByID,
	}

	// Expect Update to include replaced_at and replaced_by_id
	mock.ExpectExec("UPDATE linked_items.*SET.*replaced_at.*replaced_by_id").
		WithArgs(
			pgxmock.AnyArg(), // id
			pgxmock.AnyArg(), // institution_id
			pgxmock.AnyArg(), // institution_name
			pgxmock.AnyArg(), // provider
			pgxmock.AnyArg(), // cursor
			pgxmock.AnyArg(), // last_synced_at
			pgxmock.AnyArg(), // status
			pgxmock.AnyArg(), // error_code
			pgxmock.AnyArg(), // error_message
			pgxmock.AnyArg(), // last_error_at
			pgxmock.AnyArg(), // retry_count
			pgxmock.AnyArg(), // next_retry_at
			pgxmock.AnyArg(), // replaced_at
			pgxmock.AnyArg(), // replaced_by_id
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err := repo.Update(ctx, item)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// Issue #896: Resubscription Item History Continuity Tests
// ============================================================================

// TestLinkedItemRepository_GetByUserAndInstitution_MultipleDisconnected_ReturnsMostRecent
// verifies that when multiple disconnected items exist at the same institution,
// the query uses ORDER BY created_at DESC to return the most recent one.
func TestLinkedItemRepository_GetByUserAndInstitution_MultipleDisconnected_ReturnsMostRecent(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"
	mostRecentItemID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).AddRow(
		mostRecentItemID, userID, "item-recent", "encrypted-token",
		institutionID, "Chase", "plaid", nil, nil,
		"disconnected", nil, nil, nil, 0, nil,
		nil, nil,
		now, now,
		nil, nil,
	)

	// The regex must include ORDER BY created_at DESC to verify the fix is in place.
	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status.*ORDER BY created_at DESC").
		WithArgs(userID, institutionID, "disconnected").
		WillReturnRows(rows)

	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "disconnected")

	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, mostRecentItemID, item.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserAndInstitution_SingleDisconnected_ReturnsIt tests that
// a single disconnected item at an institution is returned successfully.
func TestLinkedItemRepository_GetByUserAndInstitution_SingleDisconnected_ReturnsIt(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_bofa"
	itemID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).AddRow(
		itemID, userID, "item-disconnected", "encrypted-token",
		institutionID, "Bank of America", "plaid", nil, nil,
		"disconnected", nil, nil, nil, 0, nil,
		nil, nil,
		now, now,
		nil, nil,
	)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status.*ORDER BY created_at DESC").
		WithArgs(userID, institutionID, "disconnected").
		WillReturnRows(rows)

	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "disconnected")

	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, itemID, item.ID)
	assert.Equal(t, domain.LinkedItemStatusDisconnected, item.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserAndInstitution_NoMatch_ReturnsNotFound tests that
// when no item matches, the method returns nil, nil (not an error).
func TestLinkedItemRepository_GetByUserAndInstitution_NoMatch_ReturnsNotFound(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_nonexistent"

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	})

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id.*AND institution_id.*AND status.*ORDER BY created_at DESC").
		WithArgs(userID, institutionID, "disconnected").
		WillReturnRows(rows)

	item, err := repo.GetByUserAndInstitution(ctx, userID, institutionID, "disconnected")

	assert.Nil(t, item)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// Issue #659: Plaid Investments - C1 Fix: investments_last_synced_date in SELECT
// ============================================================================

// TestLinkedItemRepository_GetByItemID_ReturnsInvestmentsLastSyncedDate verifies that
// GetByItemID includes investments_last_synced_date in the SELECT query (C1 fix).
// RED: fails before implementation fix because the SQL won't match mock expectation.
func TestLinkedItemRepository_GetByItemID_ReturnsInvestmentsLastSyncedDate(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	itemID := "plaid-item-123"
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	syncDate := now.Truncate(24 * time.Hour)

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).AddRow(
		linkedItemID, userID, itemID, "encrypted-token",
		"ins_123", "Chase", "plaid", nil, nil,
		"active", nil, nil, nil, 0, nil,
		nil, nil,
		now, now,
		&syncDate, nil,
	)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE item_id").
		WithArgs(itemID).WillReturnRows(rows)

	item, err := repo.GetByItemID(ctx, itemID)

	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.NotNil(t, item.InvestmentsLastSyncedDate)
	assert.Equal(t, syncDate, *item.InvestmentsLastSyncedDate)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLinkedItemRepository_GetByUserID_ExcludesReplacedByDefault tests that GetByUserID
// should only return active items (not replaced ones) for normal use cases
// Note: This behavior may need adjustment based on business requirements
func TestLinkedItemRepository_GetByUserID_IncludesAllStatuses(t *testing.T) {
	mock, repo := setupLinkedItemMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "item_id", "access_token_encrypted",
		"institution_id", "institution_name", "provider", "cursor", "last_synced_at",
		"status", "error_code", "error_message", "last_error_at", "retry_count", "next_retry_at",
		"replaced_at", "replaced_by_id",
		"created_at", "updated_at",
		"investments_last_synced_date", "liabilities_last_synced_at",
	}).
		AddRow(
			uuid.New().String(), userID, "item-1", "encrypted-token-1",
			"ins_1", "Chase", "plaid", nil, nil,
			"active", nil, nil, nil, 0, nil,
			nil, nil,
			now, now,
			nil, nil,
		).
		AddRow(
			uuid.New().String(), userID, "item-2", "encrypted-token-2",
			"ins_1", "Chase", "plaid", nil, nil,
			"replaced", nil, nil, nil, 0, nil,
			&now, nil,
			now, now,
			nil, nil,
		)

	mock.ExpectQuery("SELECT.*FROM linked_items.*WHERE user_id").
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	items, err := repo.GetByUserID(ctx, userID)

	// Assert - for now, GetByUserID returns all items
	// The service layer should filter by status as needed
	assert.NoError(t, err)
	assert.Len(t, items, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}
