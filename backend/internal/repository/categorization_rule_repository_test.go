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
		TargetKind:     func() *string { s := "pfc"; return &s }(),
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
		rule.TargetKind,  // target_kind
		pgxmock.AnyArg(), // target_pfc_primary
		pgxmock.AnyArg(), // target_pfc_detailed
		pgxmock.AnyArg(), // target_custom_category_id
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

	// Helper for nullable string
	desc := "Test description"

	targetKind := "pfc"
	targetPFCPrimary := "FOOD_AND_DRINK"
	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic",
		"target_kind", "target_pfc_primary", "target_pfc_detailed", "target_custom_category_id",
		"target_tags", "tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at", "version",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(ruleID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			ruleID,
			userID,
			"Coffee Shops",
			&desc,
			true,
			10,
			`[{"type":"merchant_contains","value":"starbucks"}]`,
			"AND",
			&targetKind,
			&targetPFCPrimary,
			nil, // target_pfc_detailed
			nil, // target_custom_category_id
			`["recurring"]`,
			"add",
			"manual",
			5,
			1,
			&now,
			now,
			now,
			int64(1),
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

	// Helper for nullable strings
	desc1, desc2 := "Desc 1", "Desc 2"

	pfcPrimary1 := "FOOD_AND_DRINK"
	pfcPrimary2 := "GENERAL_MERCHANDISE"
	targetKind1, targetKind2 := "pfc", "pfc"
	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic",
		"target_kind", "target_pfc_primary", "target_pfc_detailed", "target_custom_category_id",
		"target_tags", "tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at", "version",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Rule 1",
				&desc1,
				true,
				10,
				`[]`,
				"AND",
				&targetKind1,
				&pfcPrimary1,
				nil, // target_pfc_detailed
				nil, // target_custom_category_id
				`[]`,
				"add",
				"manual",
				0,
				0,
				nil,
				now,
				now,
				int64(1),
			).
			AddRow(
				uuid.New().String(),
				userID,
				"Rule 2",
				&desc2,
				true,
				5,
				`[]`,
				"AND",
				&targetKind2,
				&pfcPrimary2,
				nil, // target_pfc_detailed
				nil, // target_custom_category_id
				`[]`,
				"add",
				"manual",
				0,
				0,
				nil,
				now,
				now,
				int64(1),
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

	// Helper for nullable string
	desc := "Desc"

	pfcPrimary := "FOOD_AND_DRINK"
	targetKind := "pfc"
	columns := []string{
		"id", "user_id", "name", "description", "enabled", "priority",
		"conditions", "condition_logic",
		"target_kind", "target_pfc_primary", "target_pfc_detailed", "target_custom_category_id",
		"target_tags", "tag_behavior", "source", "match_count", "override_count",
		"last_matched_at", "created_at", "updated_at", "version",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New().String(),
			userID,
			"Enabled Rule",
			&desc,
			true,
			10,
			`[]`,
			"AND",
			&targetKind,
			&pfcPrimary,
			nil, // target_pfc_detailed
			nil, // target_custom_category_id
			`[]`,
			"add",
			"manual",
			0,
			0,
			nil,
			now,
			now,
			int64(1),
		))

	rules, err := repo.GetEnabledByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.True(t, rules[0].Enabled)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_ListAllForQuarantineCensus tests the #1625 Task 18 census
// query: it must scan the WHOLE table (no user_id filter — this is the point, a per-user view
// cannot deduplicate a rule seen across many users' syncs), and it must populate only the three
// columns the classifier needs.
func TestCategorizationRuleRepository_ListAllForQuarantineCensus(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id1, id2 := uuid.New().String(), uuid.New().String()
	columns := []string{"id", "conditions", "condition_logic"}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, conditions, condition_logic FROM categorization_rules`)).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(id1, `[]`, "AND").
			AddRow(id2, `[{"type":"merchant_contains","value":"starbucks"}]`, "OR"))

	rules, err := repo.ListAllForQuarantineCensus(ctx)

	assert.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, id1, rules[0].ID)
	assert.Empty(t, rules[0].Conditions)
	assert.Equal(t, "AND", rules[0].ConditionLogic)
	assert.Equal(t, id2, rules[1].ID)
	require.Len(t, rules[1].Conditions, 1)
	assert.Equal(t, "OR", rules[1].ConditionLogic)
	// Fields the classifier never reads must be left at the zero value, not silently populated —
	// a caller must not be tempted to use this result for anything but classification.
	assert.Empty(t, rules[0].UserID)
	assert.Empty(t, rules[0].Name)
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
	rule.UpdatedAt = time.Now().Add(-time.Hour)
	rule.Version = 7 // the version observed by the caller's prior read; the concurrency predicate

	returnedUpdatedAt := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(
			rule.Name,
			pgxmock.AnyArg(), // description
			pgxmock.AnyArg(), // enabled
			pgxmock.AnyArg(), // priority
			pgxmock.AnyArg(), // conditions JSON
			pgxmock.AnyArg(), // condition_logic
			pgxmock.AnyArg(), // target_kind
			pgxmock.AnyArg(), // target_pfc_primary
			pgxmock.AnyArg(), // target_pfc_detailed
			pgxmock.AnyArg(), // target_custom_category_id
			pgxmock.AnyArg(), // target_tags JSON
			pgxmock.AnyArg(), // tag_behavior
			rule.ID,
			// ⚠ #1625 final review B2: the predicate is `version`, NOT `updated_at`. A BEFORE
			// UPDATE trigger stamps updated_at on every write including IncrementMatchCount, so
			// keying on it made an ordinary sync look like a competing edit.
			int64(7),
		).WillReturnRows(pgxmock.NewRows([]string{"updated_at", "version"}).AddRow(returnedUpdatedAt, int64(8)))

	err := repo.Update(ctx, rule)

	assert.NoError(t, err)
	assert.Equal(t, returnedUpdatedAt, rule.UpdatedAt, "the trigger-stamped value must be read back, not assumed")
	assert.Equal(t, int64(8), rule.Version, "the bumped version must be read back so a second save from the same object works")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Update_NotFound tests update of non-existent rule: zero rows
// matched because the id itself does not exist, disambiguated from a concurrency conflict below
// by the follow-up EXISTS check (#1625 Task 19).
func TestCategorizationRuleRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())
	rule.ID = uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(
			pgxmock.AnyArg(), // name
			pgxmock.AnyArg(), // description
			pgxmock.AnyArg(), // enabled
			pgxmock.AnyArg(), // priority
			pgxmock.AnyArg(), // conditions JSON
			pgxmock.AnyArg(), // condition_logic
			pgxmock.AnyArg(), // target_kind
			pgxmock.AnyArg(), // target_pfc_primary
			pgxmock.AnyArg(), // target_pfc_detailed
			pgxmock.AnyArg(), // target_custom_category_id
			pgxmock.AnyArg(), // target_tags JSON
			pgxmock.AnyArg(), // tag_behavior
			rule.ID,
			rule.Version,
		).WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM categorization_rules WHERE id = $1)`)).
		WithArgs(rule.ID).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	err := repo.Update(ctx, rule)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrRuleNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCategorizationRuleRepository_Update_ConcurrentModification tests the other zero-rows case:
// the id exists but `version` moved underneath this write — the optimistic-concurrency conflict
// #1625 Task 19 exists to catch, distinguished from Update_NotFound by the follow-up EXISTS check.
func TestCategorizationRuleRepository_Update_ConcurrentModification(t *testing.T) {
	mock, repo := setupRuleMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	rule := createTestRule(uuid.New().String())
	rule.ID = uuid.New().String()
	rule.Version = 3 // stale — another USER edit already moved the row on

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE categorization_rules`)).
		WithArgs(
			pgxmock.AnyArg(), // name
			pgxmock.AnyArg(), // description
			pgxmock.AnyArg(), // enabled
			pgxmock.AnyArg(), // priority
			pgxmock.AnyArg(), // conditions JSON
			pgxmock.AnyArg(), // condition_logic
			pgxmock.AnyArg(), // target_kind
			pgxmock.AnyArg(), // target_pfc_primary
			pgxmock.AnyArg(), // target_pfc_detailed
			pgxmock.AnyArg(), // target_custom_category_id
			pgxmock.AnyArg(), // target_tags JSON
			pgxmock.AnyArg(), // tag_behavior
			rule.ID,
			rule.Version,
		).WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM categorization_rules WHERE id = $1)`)).
		WithArgs(rule.ID).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	err := repo.Update(ctx, rule)

	assert.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrRuleConcurrentlyModified)
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
