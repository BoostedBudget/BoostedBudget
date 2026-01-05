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

// setupRuleMockDB creates a mock database for categorization rule repository testing
func setupRuleMockDB(t *testing.T) (pgxmock.PgxPoolIface, CategorizationRuleRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewCategorizationRuleRepository(mock)
	return mock, repo
}

// createTestRule creates a test categorization rule
func createTestRule(userID string) *domain.CategorizationRule {
	desc := "Categorize coffee shop purchases"
	return &domain.CategorizationRule{
		ID:             uuid.New().String(),
		UserID:         userID,
		Name:           "Coffee Shops",
		Description:    &desc,
		Enabled:        true,
		Priority:       10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
		ConditionLogic: "AND",
		TargetCategory: []string{"Food & Drink", "Coffee"},
		TargetTags:     []string{"recurring"},
		TagBehavior:    "add",
		Source:         "manual",
		MatchCount:     0,
		OverrideCount:  0,
	}
}

// TestCategorizationRuleRepository_Create tests successful rule creation
func TestCategorizationRuleRepository_Create(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO categorization_rules`)).WithArgs(
		pgxmock.AnyArg(), // id
		rule.UserID,
		rule.Name,
		rule.Description,
		rule.Enabled,
		rule.Priority,
		pgxmock.AnyArg(), // conditions (JSON)
		rule.ConditionLogic,
		pgxmock.AnyArg(), // target_category (JSON)
		pgxmock.AnyArg(), // target_tags (JSON)
		rule.TagBehavior,
		rule.Source,
		rule.MatchCount,
		rule.OverrideCount,
		pgxmock.AnyArg(), // last_matched_at
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, rule)

	assert.NoError(t, err)
	assert.NotEmpty(t, rule.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Create_DuplicateName tests duplicate name error
func TestCategorizationRuleRepository_Create_DuplicateName(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO categorization_rules`)).WithArgs(
		pgxmock.AnyArg(),
		rule.UserID,
		rule.Name,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
	).WillReturnError(pgx.ErrNoRows) // Simulating constraint violation

	err := repo.Create(ctx, rule)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_GetByID tests retrieval by ID
func TestCategorizationRuleRepository_GetByID(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic", "target_category", "target_tags",
		"tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(ruleID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			ruleID,
			userID,
			"Coffee Shops",
			"Test description",
			true,
			10,
			`[{"type":"merchant_contains","value":"starbucks"}]`,
			"AND",
			`["Food & Drink","Coffee"]`,
			`["recurring"]`,
			"add",
			"manual",
			5,
			1,
			now,
			now,
			now,
		))

	rule, err := repo.GetByID(ctx, ruleID)

	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, ruleID, rule.ID)
	assert.Equal(t, userID, rule.UserID)
	assert.Equal(t, "Coffee Shops", rule.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_GetByID_NotFound tests retrieval of non-existent rule
func TestCategorizationRuleRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(ruleID).
		WillReturnError(pgx.ErrNoRows)

	rule, err := repo.GetByID(ctx, ruleID)

	assert.Error(t, err)
	assert.Nil(t, rule)
	assert.Equal(t, domain.ErrRuleNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_GetByUserID tests retrieval by user ID
func TestCategorizationRuleRepository_GetByUserID(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic", "target_category", "target_tags",
		"tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Rule 1",
				"Desc 1",
				true,
				10,
				`[]`,
				"AND",
				`["Cat1"]`,
				`[]`,
				"add",
				"manual",
				0,
				0,
				nil,
				now,
				now,
			).
			AddRow(
				uuid.New().String(),
				userID,
				"Rule 2",
				"Desc 2",
				true,
				5,
				`[]`,
				"AND",
				`["Cat2"]`,
				`[]`,
				"add",
				"manual",
				0,
				0,
				nil,
				now,
				now,
			))

	rules, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, rules, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_GetEnabledByUserID tests retrieval of enabled rules only
func TestCategorizationRuleRepository_GetEnabledByUserID(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic", "target_category", "target_tags",
		"tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New().String(),
			userID,
			"Enabled Rule",
			"Desc",
			true,
			10,
			`[]`,
			"AND",
			`["Cat"]`,
			`[]`,
			"add",
			"manual",
			0,
			0,
			nil,
			now,
			now,
		))

	rules, err := repo.GetEnabledByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.True(t, rules[0].Enabled)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Update tests rule update
func TestCategorizationRuleRepository_Update(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())
	rule.ID = uuid.New().String()
	rule.Name = "Updated Name"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(
			rule.Name,
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			rule.ID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, rule)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Update_NotFound tests update of non-existent rule
func TestCategorizationRuleRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())
	rule.ID = uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			rule.ID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, rule)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrRuleNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Delete tests rule deletion
func TestCategorizationRuleRepository_Delete(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM categorization_rules`)).
		WithArgs(ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Delete(ctx, ruleID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Delete_NotFound tests deletion of non-existent rule
func TestCategorizationRuleRepository_Delete_NotFound(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM categorization_rules`)).
		WithArgs(ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Delete(ctx, ruleID)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrRuleNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_CountByUserID tests counting rules by user
func TestCategorizationRuleRepository_CountByUserID(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(5))

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 5, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_IncrementMatchCount tests incrementing match count
func TestCategorizationRuleRepository_IncrementMatchCount(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(pgxmock.AnyArg(), ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.IncrementMatchCount(ctx, ruleID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_IncrementOverrideCount tests incrementing override count
func TestCategorizationRuleRepository_IncrementOverrideCount(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ruleID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(ruleID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.IncrementOverrideCount(ctx, ruleID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
