package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// CategorizationRuleRepository defines the interface for categorization rule data access.
type CategorizationRuleRepository interface {
	// Create creates a new categorization rule in the database.
	Create(ctx context.Context, rule *domain.CategorizationRule) error

	// GetByID retrieves a rule by its ID.
	GetByID(ctx context.Context, id string) (*domain.CategorizationRule, error)

	// GetByUserID retrieves all rules for a specific user, ordered by priority (desc).
	GetByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)

	// GetEnabledByUserID retrieves all enabled rules for a user, ordered by priority (desc).
	GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)

	// GetByName retrieves a rule by user ID and name.
	GetByName(ctx context.Context, userID, name string) (*domain.CategorizationRule, error)

	// Update updates an existing rule.
	Update(ctx context.Context, rule *domain.CategorizationRule) error

	// Delete removes a rule from the database.
	Delete(ctx context.Context, id string) error

	// CountByUserID counts the total number of rules for a user.
	CountByUserID(ctx context.Context, userID string) (int, error)

	// IncrementMatchCount increments the match count for a rule.
	IncrementMatchCount(ctx context.Context, ruleID string) error

	// IncrementOverrideCount increments the override count for a rule.
	IncrementOverrideCount(ctx context.Context, ruleID string) error
}

// categorizationRuleRepository implements CategorizationRuleRepository using PostgreSQL.
type categorizationRuleRepository struct {
	db DB
}

// NewCategorizationRuleRepository creates a new CategorizationRuleRepository instance.
func NewCategorizationRuleRepository(db DB) CategorizationRuleRepository {
	return &categorizationRuleRepository{db: db}
}

// Create creates a new categorization rule in the database.
func (r *categorizationRuleRepository) Create(ctx context.Context, rule *domain.CategorizationRule) error {
	// Generate UUID if not already set
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	// Marshal JSON fields
	conditionsJSON, err := json.Marshal(rule.Conditions)
	if err != nil {
		return err
	}

	tagsJSON, err := json.Marshal(rule.TargetTags)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO categorization_rules (
			id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`

	_, err = r.db.Exec(ctx, query,
		rule.ID,
		rule.UserID,
		rule.Name,
		rule.Description,
		rule.Enabled,
		rule.Priority,
		conditionsJSON,
		rule.ConditionLogic,
		rule.TargetKind,
		rule.TargetPFCPrimary,
		rule.TargetPFCDetailed,
		rule.TargetCustomCategoryID,
		tagsJSON,
		rule.TagBehavior,
		rule.Source,
		rule.MatchCount,
		rule.OverrideCount,
		rule.LastMatchedAt,
		rule.CreatedAt,
		rule.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_rule_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrRuleAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a rule by its ID.
func (r *categorizationRuleRepository) GetByID(ctx context.Context, id string) (*domain.CategorizationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at
		FROM categorization_rules
		WHERE id = $1
	`

	rule := &domain.CategorizationRule{}
	var conditionsJSON, tagsJSON []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&rule.ID,
		&rule.UserID,
		&rule.Name,
		&rule.Description,
		&rule.Enabled,
		&rule.Priority,
		&conditionsJSON,
		&rule.ConditionLogic,
		&rule.TargetKind,
		&rule.TargetPFCPrimary,
		&rule.TargetPFCDetailed,
		&rule.TargetCustomCategoryID,
		&tagsJSON,
		&rule.TagBehavior,
		&rule.Source,
		&rule.MatchCount,
		&rule.OverrideCount,
		&rule.LastMatchedAt,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRuleNotFound
		}
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(tagsJSON, &rule.TargetTags); err != nil {
		return nil, err
	}

	return rule, nil
}

// GetByUserID retrieves all rules for a specific user, ordered by priority (desc).
func (r *categorizationRuleRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at
		FROM categorization_rules
		WHERE user_id = $1
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID)
}

// GetEnabledByUserID retrieves all enabled rules for a user, ordered by priority (desc).
func (r *categorizationRuleRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at
		FROM categorization_rules
		WHERE user_id = $1 AND enabled = true
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID)
}

// GetByName retrieves a rule by user ID and name.
func (r *categorizationRuleRepository) GetByName(ctx context.Context, userID, name string) (*domain.CategorizationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at
		FROM categorization_rules
		WHERE user_id = $1 AND name = $2
	`

	rule := &domain.CategorizationRule{}
	var conditionsJSON, tagsJSON []byte

	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&rule.ID,
		&rule.UserID,
		&rule.Name,
		&rule.Description,
		&rule.Enabled,
		&rule.Priority,
		&conditionsJSON,
		&rule.ConditionLogic,
		&rule.TargetKind,
		&rule.TargetPFCPrimary,
		&rule.TargetPFCDetailed,
		&rule.TargetCustomCategoryID,
		&tagsJSON,
		&rule.TagBehavior,
		&rule.Source,
		&rule.MatchCount,
		&rule.OverrideCount,
		&rule.LastMatchedAt,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRuleNotFound
		}
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(tagsJSON, &rule.TargetTags); err != nil {
		return nil, err
	}

	return rule, nil
}

// Update updates an existing rule.
func (r *categorizationRuleRepository) Update(ctx context.Context, rule *domain.CategorizationRule) error {
	rule.UpdatedAt = time.Now()

	// Marshal JSON fields
	conditionsJSON, err := json.Marshal(rule.Conditions)
	if err != nil {
		return err
	}

	tagsJSON, err := json.Marshal(rule.TargetTags)
	if err != nil {
		return err
	}

	query := `
		UPDATE categorization_rules
		SET name = $1, description = $2, enabled = $3, priority = $4,
			conditions = $5, condition_logic = $6,
			target_kind = $7, target_pfc_primary = $8, target_pfc_detailed = $9,
			target_custom_category_id = $10,
			target_tags = $11, tag_behavior = $12, updated_at = $13
		WHERE id = $14
	`

	ct, err := r.db.Exec(ctx, query,
		rule.Name,
		rule.Description,
		rule.Enabled,
		rule.Priority,
		conditionsJSON,
		rule.ConditionLogic,
		rule.TargetKind,
		rule.TargetPFCPrimary,
		rule.TargetPFCDetailed,
		rule.TargetCustomCategoryID,
		tagsJSON,
		rule.TagBehavior,
		rule.UpdatedAt,
		rule.ID,
	)
	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_rule_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrRuleAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrRuleNotFound
	}

	return nil
}

// Delete removes a rule from the database.
func (r *categorizationRuleRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM categorization_rules WHERE id = $1`

	ct, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrRuleNotFound
	}

	return nil
}

// CountByUserID counts the total number of rules for a user.
func (r *categorizationRuleRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM categorization_rules WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// IncrementMatchCount increments the match count and updates last_matched_at.
func (r *categorizationRuleRepository) IncrementMatchCount(ctx context.Context, ruleID string) error {
	query := `
		UPDATE categorization_rules
		SET match_count = match_count + 1, last_matched_at = $1, updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), ruleID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrRuleNotFound
	}

	return nil
}

// IncrementOverrideCount increments the override count.
func (r *categorizationRuleRepository) IncrementOverrideCount(ctx context.Context, ruleID string) error {
	query := `
		UPDATE categorization_rules
		SET override_count = override_count + 1, updated_at = NOW()
		WHERE id = $1
	`

	ct, err := r.db.Exec(ctx, query, ruleID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrRuleNotFound
	}

	return nil
}

// queryRules is a helper to query and scan multiple rules.
func (r *categorizationRuleRepository) queryRules(ctx context.Context, query string, args ...interface{}) ([]*domain.CategorizationRule, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*domain.CategorizationRule
	for rows.Next() {
		rule := &domain.CategorizationRule{}
		var conditionsJSON, tagsJSON []byte

		err := rows.Scan(
			&rule.ID,
			&rule.UserID,
			&rule.Name,
			&rule.Description,
			&rule.Enabled,
			&rule.Priority,
			&conditionsJSON,
			&rule.ConditionLogic,
			&rule.TargetKind,
			&rule.TargetPFCPrimary,
			&rule.TargetPFCDetailed,
			&rule.TargetCustomCategoryID,
			&tagsJSON,
			&rule.TagBehavior,
			&rule.Source,
			&rule.MatchCount,
			&rule.OverrideCount,
			&rule.LastMatchedAt,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsJSON, &rule.TargetTags); err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}
