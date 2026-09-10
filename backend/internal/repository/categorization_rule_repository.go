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

	// ListAllForQuarantineCensus returns every rule in the table (all users), populated with only
	// the fields the quarantine classifier reads (id, conditions, condition_logic). #1625 Task 18:
	// the shadow-mode counter in the observability package increments once per SYNC PASS a rule is
	// re-observed in, so it cannot answer "how many distinct rules are quarantined" — a rule
	// re-evaluated 1000 times and 1000 distinct rules look identical on that counter. Answering the
	// real question requires seeing every rule exactly once, which a per-user, per-sync view
	// structurally cannot do. This is a full-table scan; it is meant for a periodic census, not a
	// request hot path — see services.RunRuleQuarantineCensus.
	ListAllForQuarantineCensus(ctx context.Context) ([]*domain.CategorizationRule, error)

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
	// version is seeded by the column DEFAULT (migration 000192) rather than written here, so the
	// database is the only thing that decides a new rule's starting token. Mirrored onto the struct
	// because the caller returns this object straight to the client, and a zero would make the
	// FIRST update look like a stale write.
	rule.Version = 1

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
			last_matched_at, created_at, updated_at, version
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
		&rule.Version,
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
	normalizeLoadedConditions(rule)
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
			last_matched_at, created_at, updated_at, version
		FROM categorization_rules
		WHERE user_id = $1
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID)
}

// GetEnabledByUserID retrieves all enabled rules for a user, ordered by priority (desc).
//
// This is the set the rule ENGINE fires (ruleApplicationService.ApplyRulesToTransaction /
// ApplyRulesToBatch) as well as the set GET /rules?enabled returns, so Issue #1569 Task 9's
// promise -- a rule may not act on a category the user can no longer see -- has to hold HERE, not
// only at the two service methods that can flip `enabled`. Those gates
// (categorization_rule_service.assertTargetCategoryOwnedAndLive) give the user a proper 404
// instead of a silently inert rule; this predicate is what makes the guarantee unconditional,
// covering any future writer of `enabled` and any direct-SQL repair that re-arms a rule by hand.
//
// A rule with no custom target is unaffected: the EXISTS is guarded by
// `target_custom_category_id IS NULL`, so PFC-target and tag-only rules keep firing exactly as
// before.
func (r *categorizationRuleRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			conditions, condition_logic,
			target_kind, target_pfc_primary, target_pfc_detailed, target_custom_category_id,
			target_tags, tag_behavior, source, match_count, override_count,
			last_matched_at, created_at, updated_at, version
		FROM categorization_rules
		WHERE user_id = $1 AND enabled = true
		  AND (
		    target_custom_category_id IS NULL
		    OR EXISTS (
		      SELECT 1 FROM custom_categories cc
		      WHERE cc.id = target_custom_category_id AND cc.deleted_at IS NULL
		    )
		  )
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
			last_matched_at, created_at, updated_at, version
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
		&rule.Version,
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
	normalizeLoadedConditions(rule)
	if err := json.Unmarshal(tagsJSON, &rule.TargetTags); err != nil {
		return nil, err
	}

	return rule, nil
}

// ListAllForQuarantineCensus implements CategorizationRuleRepository.ListAllForQuarantineCensus —
// see that method's doc comment for why a table-wide scan is the correct shape here. Only the
// three fields the classifier reads are populated; every other field on the returned rules is the
// zero value, so this result must never be used for anything but classification.
func (r *categorizationRuleRepository) ListAllForQuarantineCensus(ctx context.Context) ([]*domain.CategorizationRule, error) {
	query := `SELECT id, conditions, condition_logic FROM categorization_rules`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*domain.CategorizationRule
	for rows.Next() {
		rule := &domain.CategorizationRule{}
		var conditionsJSON []byte
		if err := rows.Scan(&rule.ID, &conditionsJSON, &rule.ConditionLogic); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
			return nil, err
		}
		normalizeLoadedConditions(rule)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

// Update updates an existing rule.
//
// #1625 Task 19: the WHERE clause is guarded by a VERSION token, not id alone. (It said
// "guarded by updated_at" until #1625 second fix wave L7 — directly above the `version` predicate
// that final review B2 replaced it with.) UpdateRule and
// ToggleRule both read-modify-write the WHOLE row (every column, not just the field either caller
// meant to change), so without this guard a toggle racing a repair could win the write and put
// the repair's PRIOR, malformed conditions back — last-writer-wins across the entire row. rule
// carries the version its caller observed via GetByID; if the stored value has since moved, this
// UPDATE matches zero rows and the write is refused rather than silently overwriting whatever
// changed it.
//
// ⚠ #1625 final review B2: the guard was `AND updated_at = $14` and that produced FALSE conflicts.
// The table carries a BEFORE UPDATE trigger (update_categorization_rules_updated_at) that stamps
// NEW.updated_at = NOW() on EVERY successful write, and IncrementMatchCount is a write — engine
// bookkeeping during an ordinary sync, with no user involved. A sync landing inside a user's
// read-modify-write window therefore refused their save as "changed by another request", for a
// change nobody made, with a likelihood that scaled with how well the rule worked.
//
// The rule `version` (migration 000192) obeys is "would the user SEE this change if they were
// looking at the rule?", not "is this a write". So a competing user-visible change still
// conflicts and background bookkeeping does not. Reverting to `WHERE id = $13` alone was the
// ship-sooner option and was rejected: it reinstates the lost update Task 19 closed, and a silent
// lost update is worse than a false 409 precisely because the user cannot see it.
//
// ⚠ #1625 third fix wave F1: this said `version` moves ONLY here. It does not, and has not since
// the second fix wave's H1 fix. There are TWO production writers, and any third must satisfy the
// rule above rather than this list:
//
//	repository.categorizationRuleRepository.Update            (this function) — UpdateRule, ToggleRule
//	repository.customCategoryRepository (custom_category_repository.go ~:629) — deleting a custom
//	    category disables the rules that targeted it, which the user sees as the card flipping to
//	    "Disabled". Not bumping there let a stale editor's name-only PATCH write `enabled = true`
//	    back onto a rule pointing at a tombstoned category.
//
// The two writers that must NOT bump it are still IncrementMatchCount and IncrementOverrideCount,
// for the reason above: invisible to the user. Re-derive the set with
//
//	/usr/bin/grep -rn --include='*.go' --exclude='*_test.go' 'UPDATE categorization_rules' backend/internal
//
// updated_at is still read back via RETURNING rather than assumed to be time.Now() from this
// process, because the trigger — not this query — decides it.
func (r *categorizationRuleRepository) Update(ctx context.Context, rule *domain.CategorizationRule) error {
	expectedVersion := rule.Version // observed by the caller's prior read; the concurrency guard

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
			target_tags = $11, tag_behavior = $12,
			version = version + 1
		WHERE id = $13 AND version = $14
		RETURNING updated_at, version
	`

	err = r.db.QueryRow(ctx, query,
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
		rule.ID,
		expectedVersion,
	).Scan(&rule.UpdatedAt, &rule.Version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Zero rows matched either because the id doesn't exist, or because it exists but
			// `version` moved underneath us. Those are different failures for the caller (404
			// vs. 409), so a second, cheap lookup disambiguates rather than guessing.
			var exists bool
			if existsErr := r.db.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM categorization_rules WHERE id = $1)`, rule.ID,
			).Scan(&exists); existsErr != nil {
				return existsErr
			}
			if !exists {
				return domain.ErrRuleNotFound
			}
			return domain.ErrRuleConcurrentlyModified
		}
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_rule_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrRuleAlreadyExists
		}
		return err
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
//
// ⚠ It must NEVER touch `version`. This is engine bookkeeping that runs per matched transaction
// during a sync; bumping the optimistic-concurrency token here would refuse the user's next save
// as a conflict they did not cause — the exact defect #1625 final review B2 fixed. Pinned by
// TestCategorizationRuleRepository_MatchCountBumpIsNotAConflict_Integration, which asserts on the
// COLUMN and not merely on the absence of a 409.
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
//
// ⚠ Same invariant as IncrementMatchCount above: background bookkeeping, so no `version` bump.
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
			&rule.Version,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
			return nil, err
		}
		normalizeLoadedConditions(rule)
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

// normalizeLoadedConditions maps each persisted condition into its canonical
// (Field, Operator) pair so legacy fused rows evaluate through the post-#1625 registry with NO
// data migration. Errors are deliberately IGNORED: a row that cannot be normalized is junk that
// was written before the value shape was validated, and it must keep behaving exactly as it did
// (the registry returns no-match for an unknown field) rather than failing a read of the user's
// whole rule list. Shadow-mode quarantine is what makes such rows visible.
func normalizeLoadedConditions(rule *domain.CategorizationRule) {
	for i := range rule.Conditions {
		if err := rule.Conditions[i].Normalize(); err != nil && rule.Conditions[i].Type == "" {
			// Codex review: `type` is documented as always present on a response, but a row that
			// cannot be normalized would otherwise emit no `type` at all (the field is
			// `omitempty`, because INPUT may legitimately send field+operator instead). Stamping
			// the reserved sentinel keeps that promise true for every response, and is honest:
			// the condition genuinely has no classifiable fused type.
			rule.Conditions[i].Type = domain.ConditionTypeExpression
		}
	}
}
