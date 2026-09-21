package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// duplicateNamePgError is the exact Postgres 23505 the mcp_recipes_user_id_name_unique
// constraint raises (migration 000085_create_mcp_recipes.up.sql:22).
func duplicateNamePgError() *pgconn.PgError {
	return &pgconn.PgError{Code: "23505", ConstraintName: "mcp_recipes_user_id_name_unique"}
}

func setupRecipeMockDB(t *testing.T) (pgxmock.PgxPoolIface, RecipeRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewRecipeRepository(mock)
	return mock, repo
}

// recipeColumns matches the SELECT order in List / Get.
var recipeColumns = []string{
	"id", "user_id", "name", "description", "spec", "schedule", "enabled", "created_at", "updated_at",
}

func makeRecipeRow(id, userID, name string) []interface{} {
	return []interface{}{
		id,
		userID,
		name,
		"",             // description
		[]byte("[]"),   // spec JSONB (empty array)
		(*string)(nil), // schedule (nil)
		true,           // enabled
		time.Now(),
		time.Now(),
	}
}

// --- Constructor ---

func TestNewRecipeRepository_ReturnsNonNil(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewRecipeRepository(mock)
	assert.NotNil(t, repo)
}

// --- Create ---

func TestRecipeRepository_Create_Success(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		UserID:      uuid.New().String(),
		Name:        "My Recipe",
		Description: "A test recipe",
		Spec:        []domain.MCPRecipeCall{},
		Enabled:     true,
	}

	now := time.Now()
	newID := uuid.New().String()
	returnRows := pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
		AddRow(newID, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO mcp_recipes`)).
		WithArgs(
			recipe.UserID,
			recipe.Name,
			recipe.Description,
			[]byte("[]"), // specJSON (marshaled empty slice)
			recipe.Schedule,
			recipe.Enabled,
		).
		WillReturnRows(returnRows)

	err := repo.Create(ctx, recipe)

	require.NoError(t, err)
	assert.Equal(t, newID, recipe.ID)
	assert.False(t, recipe.CreatedAt.IsZero())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Create_Error(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		UserID:  uuid.New().String(),
		Name:    "My Recipe",
		Spec:    []domain.MCPRecipeCall{},
		Enabled: true,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO mcp_recipes`)).
		WithArgs(
			recipe.UserID,
			recipe.Name,
			recipe.Description,
			[]byte("[]"), // specJSON: empty Spec marshals deterministically
			recipe.Schedule,
			recipe.Enabled,
		).
		WillReturnError(errors.New("db error"))

	err := repo.Create(ctx, recipe)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Create_DuplicateNameMapsToSentinel(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		UserID:  uuid.New().String(),
		Name:    "My Recipe",
		Spec:    []domain.MCPRecipeCall{},
		Enabled: true,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO mcp_recipes`)).
		WithArgs(recipe.UserID, recipe.Name, recipe.Description, []byte("[]"), recipe.Schedule, recipe.Enabled).
		WillReturnError(duplicateNamePgError())

	err := repo.Create(ctx, recipe)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrDuplicateRecipeName), "expected ErrDuplicateRecipeName, got %v", err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Create_OtherUniqueViolationNotMisclassified(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		UserID:  uuid.New().String(),
		Name:    "My Recipe",
		Spec:    []domain.MCPRecipeCall{},
		Enabled: true,
	}

	otherConstraint := &pgconn.PgError{Code: "23505", ConstraintName: "some_other_table_unique"}
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO mcp_recipes`)).
		WithArgs(recipe.UserID, recipe.Name, recipe.Description, []byte("[]"), recipe.Schedule, recipe.Enabled).
		WillReturnError(otherConstraint)

	err := repo.Create(ctx, recipe)

	require.Error(t, err)
	assert.False(t, errors.Is(err, domain.ErrDuplicateRecipeName), "a different unique constraint must not be misclassified")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- List ---

func TestRecipeRepository_List_Success(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	id1 := uuid.New().String()
	id2 := uuid.New().String()

	rows := pgxmock.NewRows(recipeColumns)
	rows.AddRow(makeRecipeRow(id1, userID, "Recipe One")...)
	rows.AddRow(makeRecipeRow(id2, userID, "Recipe Two")...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.List(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, id1, results[0].ID)
	assert.Equal(t, "Recipe One", results[0].Name)
	assert.Equal(t, id2, results[1].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_List_Empty(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows(recipeColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.List(ctx, userID)

	require.NoError(t, err)
	assert.Nil(t, results) // returns nil (no rows appended)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_List_QueryError(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(userID).
		WillReturnError(errors.New("db error"))

	results, err := repo.List(ctx, userID)

	assert.Nil(t, results)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_List_InvalidJSON(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	id1 := uuid.New().String()

	rows := pgxmock.NewRows(recipeColumns)
	// Return malformed JSON for spec
	rows.AddRow(
		id1, userID, "Bad Recipe", "", []byte("not valid json"), (*string)(nil), true, time.Now(), time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.List(ctx, userID)

	assert.Nil(t, results)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Get ---

func TestRecipeRepository_Get_Success(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipeID := uuid.New().String()
	userID := uuid.New().String()

	rows := pgxmock.NewRows(recipeColumns)
	rows.AddRow(makeRecipeRow(recipeID, userID, "Test Recipe")...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(recipeID, userID).
		WillReturnRows(rows)

	result, err := repo.Get(ctx, recipeID, userID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, recipeID, result.ID)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, "Test Recipe", result.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Get_NotFound(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipeID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(recipeID, userID).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.Get(ctx, recipeID, userID)

	// isNotFoundError(pgx.ErrNoRows) → return nil, nil
	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Get_InvalidJSON(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipeID := uuid.New().String()
	userID := uuid.New().String()

	rows := pgxmock.NewRows(recipeColumns)
	rows.AddRow(
		recipeID, userID, "Bad Recipe", "", []byte("{invalid json}"), (*string)(nil), true, time.Now(), time.Now(),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, name`)).
		WithArgs(recipeID, userID).
		WillReturnRows(rows)

	result, err := repo.Get(ctx, recipeID, userID)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Update ---

func TestRecipeRepository_Update_Success(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		ID:          uuid.New().String(),
		UserID:      uuid.New().String(),
		Name:        "Updated Recipe",
		Description: "Updated description",
		Spec:        []domain.MCPRecipeCall{},
		Enabled:     true,
	}

	updatedAt := time.Now()
	returnRows := pgxmock.NewRows([]string{"updated_at"}).AddRow(updatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE mcp_recipes`)).
		WithArgs(
			recipe.ID,
			recipe.Name,
			recipe.Description,
			[]byte("[]"), // specJSON: empty Spec marshals deterministically
			recipe.Schedule,
			recipe.Enabled,
			recipe.UserID,
		).
		WillReturnRows(returnRows)

	err := repo.Update(ctx, recipe)

	require.NoError(t, err)
	assert.Equal(t, updatedAt, recipe.UpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Update_Error(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		ID:      uuid.New().String(),
		UserID:  uuid.New().String(),
		Name:    "Recipe",
		Spec:    []domain.MCPRecipeCall{},
		Enabled: true,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE mcp_recipes`)).
		WithArgs(
			recipe.ID,
			recipe.Name,
			recipe.Description,
			[]byte("[]"), // specJSON: empty Spec marshals deterministically
			recipe.Schedule,
			recipe.Enabled,
			recipe.UserID,
		).
		WillReturnError(errors.New("db error"))

	err := repo.Update(ctx, recipe)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Update_DuplicateNameMapsToSentinel(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipe := &domain.MCPRecipe{
		ID:      uuid.New().String(),
		UserID:  uuid.New().String(),
		Name:    "Renamed Recipe",
		Spec:    []domain.MCPRecipeCall{},
		Enabled: true,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE mcp_recipes`)).
		WithArgs(recipe.ID, recipe.Name, recipe.Description, []byte("[]"), recipe.Schedule, recipe.Enabled, recipe.UserID).
		WillReturnError(duplicateNamePgError())

	err := repo.Update(ctx, recipe)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrDuplicateRecipeName), "expected ErrDuplicateRecipeName, got %v", err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Delete ---

func TestRecipeRepository_Delete_Success(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipeID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mcp_recipes`)).
		WithArgs(recipeID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.Delete(ctx, recipeID, userID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecipeRepository_Delete_Error(t *testing.T) {
	mock, repo := setupRecipeMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	recipeID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mcp_recipes`)).
		WithArgs(recipeID, userID).
		WillReturnError(errors.New("db error"))

	err := repo.Delete(ctx, recipeID, userID)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
