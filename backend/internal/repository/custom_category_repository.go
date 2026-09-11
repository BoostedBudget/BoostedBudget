package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// CustomCategoryRepository defines the interface for custom category data access
type CustomCategoryRepository interface {
	// Create creates a new custom category in the database
	Create(ctx context.Context, category *domain.CustomCategory) error

	// GetByID retrieves a category by its ID
	GetByID(ctx context.Context, id string) (*domain.CustomCategory, error)

	// GetByUserID retrieves all categories for a specific user
	GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	// GetByName retrieves a category by user ID and name
	GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error)

	// Update updates an existing category
	Update(ctx context.Context, category *domain.CustomCategory) error

	// UpdateInTx is Update's counterpart for a caller-owned transaction (Issue #1630 B3b): it
	// applies the identical write query INSIDE tx instead of opening and committing its own, so a
	// caller composing a check against a DIFFERENT repository's locked rows (e.g.
	// customCategoryService's reparent-containment guard, which locks every affected budget via
	// BudgetRepository.GetCategoriesByBudgetIDLockedInTx) can persist both the guard's outcome and
	// this write atomically. The per-user hierarchy-serialization advisory lock (lockHierarchySQL)
	// is still taken, now scoped to the caller's transaction rather than this method's own.
	UpdateInTx(ctx context.Context, tx Transaction, category *domain.CustomCategory) error

	// CountByUserID counts the total number of categories for a user
	CountByUserID(ctx context.Context, userID string) (int, error)

	// GetNamesByIDs returns a map of id→name for the given IDs scoped to userID.
	// IDs not owned by userID or not found are omitted from the result.
	// Returns an empty map (not an error) when ids is empty.
	//
	// ⚠ THIS IS THE HISTORICAL name resolver and it DELIBERATELY SEES TOMBSTONED CATEGORIES
	// (Issue #1569 Task 9). Its display callers — transaction_service's list enrichment and
	// categorization_rule_service's — are rendering rows that already reference the category;
	// filtering here would turn a past transaction's category into a bare UUID, which is the
	// silent history rewrite this task removes. Write-time ownership validation must use
	// GetLiveNamesByIDs instead.
	GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error)

	// GetLiveNamesByIDs is GetNamesByIDs restricted to LIVE (non-tombstoned) categories. It is the
	// write-time ownership gate: budget_service and categorization_rule_service use it to reject a
	// custom_category_id that is not the caller's (Issue #1540) — and, since Issue #1569 Task 9,
	// one that has been deleted. Attaching a new budget line or rule to a tombstoned category
	// would resurrect it in every list that reads through those tables.
	GetLiveNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error)

	// GetChildren returns the direct children of the given parent category (Issue #1203).
	GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error)

	// HasChildren reports whether the given category has any direct children.
	HasChildren(ctx context.Context, id string) (bool, error)

	// GetCategoryUsage returns live counts of transactions, budget categories,
	// rules, and child categories referencing the given category (Issue #1203).
	GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error)

	// DeleteWithReassignment TOMBSTONES a custom category in a single transaction: it sets
	// deleted_at/deleted_reason/deleted_by instead of issuing a DELETE, disables the rules that
	// targeted it, reparents its children to top-level, and tombstones the budget lines that
	// referenced it. Historical transactions are left completely alone — see the method's own
	// doc comment for what each step is and why (Issue #1569 Task 9).
	//
	// Runs in one transaction with a "custom_category.deleted" audit entry written via
	// auditLog.LogInTx, so the entry and the state change commit or roll back together.
	// auditLog may be nil (the MCP tx-scoped construction path does not wire one), in which case
	// the delete still works and writes no entry.
	//
	// Returns domain.ErrCategoryNotFound when the category does not exist, is not the caller's,
	// or is ALREADY tombstoned.
	DeleteWithReassignment(ctx context.Context, userID, categoryID string, auditLog AuditLogRepository) error
}

// lockHierarchySQL takes a per-user transaction-scoped advisory lock so concurrent
// hierarchy mutations (create/reparent) serialize and cannot race past the 2-level
// trigger into a cycle or 3-level tree (Issue #1203). hashtextextended returns a bigint,
// matching pg_advisory_xact_lock(bigint).
const lockHierarchySQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// customCategoryRepository implements CustomCategoryRepository using PostgreSQL
type customCategoryRepository struct {
	db DB
}

// NewCustomCategoryRepository creates a new CustomCategoryRepository instance
func NewCustomCategoryRepository(db DB) CustomCategoryRepository {
	return &customCategoryRepository{db: db}
}

// Create creates a new custom category in the database
func (r *customCategoryRepository) Create(ctx context.Context, category *domain.CustomCategory) error {
	// Generate UUID if not already set
	if category.ID == "" {
		category.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	category.CreatedAt = now
	category.UpdatedAt = now

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize hierarchy mutations per user before writing (Issue #1203).
	if _, err := tx.Exec(ctx, lockHierarchySQL, category.UserID); err != nil {
		return err
	}

	query := `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	if _, err := tx.Exec(ctx, query,
		category.ID,
		category.UserID,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.CreatedAt,
		category.UpdatedAt,
	); err != nil {
		return mapCategoryWriteError(err)
	}

	return tx.Commit(ctx)
}

// mapCategoryWriteError translates DB constraint/trigger violations on
// custom_categories writes into domain errors (Issue #1203).
func mapCategoryWriteError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unique_user_category_name") || strings.Contains(msg, "duplicate key"):
		return domain.ErrCategoryAlreadyExists
	case strings.Contains(msg, "no grandchildren"):
		return domain.ErrCategoryParentHasParent
	case strings.Contains(msg, "cannot be nested"):
		return domain.ErrCategoryHasChildren
	case strings.Contains(msg, "same user") ||
		strings.Contains(msg, "custom_category_single_parent") ||
		strings.Contains(msg, "custom_category_no_self_parent"):
		return domain.ErrCategoryInvalidParent
	default:
		return err
	}
}

// GetByID retrieves a category by its ID
func (r *customCategoryRepository) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE id = $1 AND deleted_at IS NULL
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentPFCPrimary,
		&category.ParentID,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, err
	}

	return category, nil
}

// GetByUserID retrieves all categories for a specific user
func (r *customCategoryRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY name ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []*domain.CustomCategory
	for rows.Next() {
		category := &domain.CustomCategory{}
		err := rows.Scan(
			&category.ID,
			&category.UserID,
			&category.Name,
			&category.ParentPFCPrimary,
			&category.ParentID,
			&category.Icon,
			&category.Color,
			&category.UsageCount,
			&category.CreatedAt,
			&category.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// GetByName retrieves a category by user ID and name
func (r *customCategoryRepository) GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
	`

	category := &domain.CustomCategory{}
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&category.ID,
		&category.UserID,
		&category.Name,
		&category.ParentPFCPrimary,
		&category.ParentID,
		&category.Icon,
		&category.Color,
		&category.UsageCount,
		&category.CreatedAt,
		&category.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCategoryNotFound
		}
		return nil, err
	}

	return category, nil
}

// Update updates an existing category
func (r *customCategoryRepository) Update(ctx context.Context, category *domain.CustomCategory) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.updateQuerier(ctx, tx, category); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UpdateInTx implements CustomCategoryRepository.UpdateInTx — see that method's doc comment.
func (r *customCategoryRepository) UpdateInTx(ctx context.Context, tx Transaction, category *domain.CustomCategory) error {
	return r.updateQuerier(ctx, tx, category)
}

// updateQuerier is Update / UpdateInTx's shared body, parameterized over Querier so UpdateInTx can
// run the identical write INSIDE a caller's transaction instead of duplicating this query
// (Issue #1630 B3b, mirrors budget_repository.go's createCategoryQuerier pattern).
func (r *customCategoryRepository) updateQuerier(ctx context.Context, q Querier, category *domain.CustomCategory) error {
	category.UpdatedAt = time.Now()

	// Serialize hierarchy mutations per user before reparenting (Issue #1203). Scoped to q's
	// transaction (pg_advisory_xact_lock auto-releases at transaction end), so a caller composing
	// this inside a wider transaction (UpdateInTx) holds it for that transaction's full duration.
	if _, err := q.Exec(ctx, lockHierarchySQL, category.UserID); err != nil {
		return err
	}

	query := `
		-- tombstone-exempt(write:custom_categories): liveness is the SERVICE layer's gate (#1569 T8
		-- write-target audit). CustomCategoryService.UpdateCategory (custom_category_service.go:166)
		-- resolves the id through the
		-- tombstone-filtered GetByID before calling this, so a tombstoned category cannot reach it.
		-- Narrowing the predicate here would turn a refusal into a silent zero-row update, which is
		-- the worse failure -- the caller could not tell "not yours" from "deleted".
		UPDATE custom_categories
		SET name = $1, parent_pfc_primary = $2, parent_id = $3, icon = $4, color = $5, usage_count = $6, updated_at = $7
		WHERE id = $8
	`

	ct, err := q.Exec(ctx, query,
		category.Name,
		category.ParentPFCPrimary,
		category.ParentID,
		category.Icon,
		category.Color,
		category.UsageCount,
		category.UpdatedAt,
		category.ID,
	)
	if err != nil {
		return mapCategoryWriteError(err)
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	return nil
}

// CountByUserID counts the total number of categories for a user
func (r *customCategoryRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM custom_categories WHERE user_id = $1 AND deleted_at IS NULL`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetNamesByIDs returns a map of id→name for the given IDs scoped to userID.
// Dedupes the input slice. Returns an empty map for empty input without hitting the DB.
// IDs not found or owned by a different user are omitted from the result.
//
// See the interface method for why this one is tombstone-blind and GetLiveNamesByIDs is not.
func (r *customCategoryRepository) GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	return r.namesByIDs(ctx, userID, ids, `
		-- tombstone-exempt(custom_categories): HISTORICAL name resolution (#1569 Task 9). This is the enrichment
		-- path for rows that ALREADY reference the category — a transaction filed under it, a
		-- rule that targets it. Excluding tombstones here would render the user's own past
		-- transactions with a blank category, which is the silent history rewrite this task
		-- removes. Write-time validation uses GetLiveNamesByIDs, which does filter.
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
	`)
}

// GetLiveNamesByIDs is GetNamesByIDs restricted to live categories — see the interface method.
func (r *customCategoryRepository) GetLiveNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	return r.namesByIDs(ctx, userID, ids, `
		SELECT id, name
		FROM custom_categories
		WHERE id = ANY($1::uuid[])
		  AND user_id = $2
		  AND deleted_at IS NULL
	`)
}

// namesByIDs is the shared body of GetNamesByIDs and GetLiveNamesByIDs. The two differ by exactly
// one predicate, and keeping them one function is what stops the dedupe/empty-input/scan handling
// from drifting apart between the display path and the validation path.
func (r *customCategoryRepository) namesByIDs(ctx context.Context, userID string, ids []string, query string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}

	// Dedupe
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}

	rows, err := r.db.Query(ctx, query, unique, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string, len(unique))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetChildren returns the direct children of the given parent category.
func (r *customCategoryRepository) GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error) {
	query := `
		SELECT id, user_id, name, parent_pfc_primary, parent_id, icon, color, usage_count, created_at, updated_at
		FROM custom_categories
		WHERE parent_id = $1 AND deleted_at IS NULL
		ORDER BY name ASC
	`

	rows, err := r.db.Query(ctx, query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []*domain.CustomCategory
	for rows.Next() {
		category := &domain.CustomCategory{}
		err := rows.Scan(
			&category.ID,
			&category.UserID,
			&category.Name,
			&category.ParentPFCPrimary,
			&category.ParentID,
			&category.Icon,
			&category.Color,
			&category.UsageCount,
			&category.CreatedAt,
			&category.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// HasChildren reports whether the given category has any direct children.
func (r *customCategoryRepository) HasChildren(ctx context.Context, id string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM custom_categories WHERE parent_id = $1 AND deleted_at IS NULL)`

	var exists bool
	if err := r.db.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// GetCategoryUsage returns live reference counts for a custom category.
func (r *customCategoryRepository) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	usage := &domain.CategoryUsage{}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id = $1 AND custom_category_override_id = $2 AND deleted_at IS NULL`,
		userID, categoryID,
	).Scan(&usage.TransactionCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM budget_categories bc
		 JOIN budgets b ON b.id = bc.budget_id AND b.deleted_at IS NULL
		 WHERE b.user_id = $1 AND bc.custom_category_id = $2 AND bc.deleted_at IS NULL`,
		userID, categoryID,
	).Scan(&usage.BudgetCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM categorization_rules WHERE user_id = $1 AND target_custom_category_id = $2`,
		userID, categoryID,
	).Scan(&usage.RuleCount); err != nil {
		return nil, err
	}

	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1 AND parent_id = $2 AND deleted_at IS NULL`,
		userID, categoryID,
	).Scan(&usage.ChildCount); err != nil {
		return nil, err
	}

	return usage, nil
}

// DeleteWithReassignment TOMBSTONES a custom category and settles everything that referenced it,
// in one transaction (Issue #1203, rewritten for Issue #1569 Task 9).
//
// # WHAT THIS USED TO DO, AND WHY IT DOESN'T ANY MORE
//
// Until #1569 this was a five-step hard delete whose first step was:
//
//	UPDATE transactions SET category_override_kind = NULL, custom_category_override_id = NULL,
//	    pfc_primary_override = NULL, pfc_detailed_override = NULL, category_source = 'plaid',
//	    reviewed = false, reviewed_at = NULL, effective_category_name = pfc_primary
//	WHERE user_id = $1 AND custom_category_override_id = $2
//
// Deleting one row of configuration silently refiled every past transaction under Plaid's guess
// AND wiped the user's hand-curated review state. That step is GONE. Nothing in this method
// touches the transactions table at all any more; a historical transaction keeps its override, its
// category_source, its effective_category_name, its `reviewed` and its `reviewed_at`. The name
// still resolves because GetNamesByIDs is deliberately tombstone-blind.
//
// # FK-ACTION DESIGN NOTE, RE-DERIVED FOR THE TOMBSTONE (was Issue #1540 / #1531 slice H2)
//
// Three tables reference custom_categories(id), plus the self-FK:
//
//	transactions.custom_category_override_id        RESTRICT
//	budget_categories.custom_category_id            CASCADE
//	categorization_rules.target_custom_category_id  SET NULL
//	custom_categories.parent_id (self)              RESTRICT
//
// NONE of the four fires any more, because this method no longer issues a DELETE. That is the
// point, and it is what makes stopping the transaction revert safe: the RESTRICT that the old
// unfiltered revert existed to satisfy is never evaluated. The one remaining hard delete of this
// table is GDPR erasure, and profile_repository.DeleteUserCascade already clears dangling
// overrides ahead of it. Pinned by TestCategoryTombstone_RestrictStillSatisfiable_Integration,
// which also proves the RESTRICT is still armed by attempting the hard delete and requiring it to
// fail.
//
// # THE STEPS, AND THE RULING BEHIND EACH
//
//  1. Tombstone the category. First, so that a not-found/already-deleted category costs nothing
//     else and nothing downstream runs against a row that was never ours.
//
//  2. DISABLE the rules that targeted it; do not delete them, and do not blank their target.
//     Both of the old behaviours were wrong in different ways:
//     - DELETE (old step 2) destroys rule_execution_history with it —
//     fk_rule_execution_history_rule_id is ON DELETE CASCADE (migration 000020:159) — so the
//     automation audit trail vanishes. That is history, and #1569 exists to keep history.
//     - BLANKING the target (old step 3) left the rule ENABLED with target_kind, the two pfc
//     targets and target_custom_category_id all NULL. The rule kept matching and kept firing,
//     now applying only its tags: a DIFFERENT effect from the one the user configured, applied
//     silently, forever. That is worse than either alternative.
//     Disabling is the only option that keeps the trail, keeps the user's configuration legible
//     ("this rule filed things as Coffee"), and guarantees it cannot fire with a changed effect —
//     the engine reads WHERE enabled = true (categorization_rule_repository.GetEnabledByUserID).
//     Rules are active automation rather than history, which is why they are not simply left
//     alone. Pinned by TestCategoryTombstone_RulesAreDisabledNotDeletedOrBlanked_Integration.
//
//  3. REPARENT LIVE children to top-level; do not tombstone them. A child category is LIVE
//     configuration the user did not ask to delete — tombstoning it would destroy strictly more
//     than the behaviour it replaces. Leaving parent_id pointing at a tombstoned row is the other
//     rejected option: every tree read would then render a node whose parent is invisible.
//     What is lost is one nullable pointer of configuration, not history. `AND user_id = $1` is
//     defence-in-depth only: enforce_custom_category_two_levels (migration 000078) already makes
//     a cross-user parent_id unwritable, which is why the old statement could omit it without
//     being a cross-user bug. Pinned by TestCategoryTombstone_ChildrenReparentedToTopLevel_
//     Integration and _ParentIdCannotCrossUsers_Integration.
//
//     ⚠ AND `AND deleted_at IS NULL` IS NOT DEFENCE-IN-DEPTH — it is the ruling, added in #1569 T8
//     fix round 2. Read the justification above literally: "every tree read would then render a
//     node whose parent is invisible" is an argument about a LIVE child, because a live child is
//     what a tree read renders. A TOMBSTONED child is rendered by no tree read at all, so the
//     argument does not reach it, and clearing its parent_id buys nothing while destroying the one
//     fact worth keeping about it — what it was filed under when the user deleted it. Delete the
//     child, then its parent, and the unfiltered statement rewrote history for no reader.
//
//     ⚠ The exemption this replaces claimed the filter "would break the delete", because "the
//     parent is being deleted in this same transaction". IT IS NOT: step 1 TOMBSTONES the parent,
//     and there is no `DELETE FROM custom_categories` in this method at all, so
//     fk_custom_categories_parent_id (ON DELETE RESTRICT, verified against the live schema) was
//     never at risk. The reason was fabricated and the write-target rule was waved through on it.
//
//     What the filter DOES create is a self-referencing tombstoned pair, which RESTRICT could
//     refuse during GDPR erasure — the one trade this branch may never make. That is why the
//     change ships with two tests, not one:
//     TestCategoryTombstone_TombstonedChildKeepsItsRecordedParent_Integration for the behaviour
//     and TestCategoryTombstone_GDPRErasureDestroysASelfReferencingTombstonedPair_Integration for
//     the risk.
//
//  4. TOMBSTONE the budget lines that referenced it, with deleted_reason = 'parent_deleted'.
//     This reproduces what the ON DELETE CASCADE used to do from the user's point of view — the
//     budget line disappears — without reproducing what it also did: hard-deleting
//     budget_categories cascades rollover_history away (fk_rollover_history_source_budget_
//     category_id), which is exactly what task 8's migration 000158 made this table soft-deletable
//     to prevent. Scoping is by custom_category_id alone, matching what the CASCADE did: since
//     Issue #1540 a budget_categories row's custom_category_id is guaranteed to belong to the same
//     user as the budget it hangs off, so this cannot reach another user's row.
//
//  5. Audit. LogInTx so the entry and the state change commit or roll back together (#1507).
func (r *customCategoryRepository) DeleteWithReassignment(ctx context.Context, userID, categoryID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Tombstone the category itself. `deleted_at IS NULL` makes a second delete a no-op that
	//    reports not-found rather than re-stamping the timestamp and re-writing an audit entry.
	ct, err := tx.Exec(ctx, `
		UPDATE custom_categories
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, categoryID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser)
	if err != nil {
		return fmt.Errorf("failed to tombstone category: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}

	// 2. Disable, do not delete and do not blank, the rules that targeted it. See the ruling above.
	//
	// ⚠ `version` is bumped, and that is load-bearing rather than tidy (#1625 second fix wave H1).
	// CategorizationRuleRepository.Update's optimistic guard is `WHERE id = $13 AND version = $14`,
	// and its doc comment justifies keying on `version` as "moved only by user EDITS, so a
	// competing VISIBLE change still conflicts and background bookkeeping does not". This
	// statement is one of those visible changes: the user deleted the category, the card flips to
	// "Disabled", and the engine stops firing the rule. Leaving `version` alone made it invisible
	// to the guard, so an editor opened BEFORE the delete could save a name-only PATCH — which
	// triggers neither of UpdateRule's live-category rechecks — and write the whole stale row
	// back, restoring `enabled = true` on a rule pointing at a tombstoned category. That is the
	// exact failure this disable exists to prevent, reintroduced by the lock meant to stop lost
	// updates. Pinned by
	// TestCategoryTombstone_DisablingRulesMovesTheConcurrencyToken_Integration.
	//
	// The two bookkeeping writers (IncrementMatchCount, IncrementOverrideCount) still must NOT
	// bump it: they are invisible to the user, and bumping there is the false-conflict defect
	// #1625 final review B2 removed. The test for that is
	// TestCategorizationRuleRepository_MatchCountBumpIsNotAConflict_Integration. The rule is
	// "would the user see this change if they were looking at the rule?", not "is this a write".
	if _, err := tx.Exec(ctx, `
		UPDATE categorization_rules
		SET enabled = false, updated_at = NOW(), version = version + 1
		WHERE user_id = $1 AND target_custom_category_id = $2 AND enabled = true
	`, userID, categoryID); err != nil {
		return fmt.Errorf("failed to disable rules targeting the category: %w", err)
	}

	// 3. Reparent LIVE children to top-level. A tombstoned child keeps its recorded parent.
	if _, err := tx.Exec(ctx,
		`UPDATE custom_categories SET parent_id = NULL, updated_at = NOW()
		 WHERE parent_id = $2 AND user_id = $1 AND deleted_at IS NULL`,
		userID, categoryID,
	); err != nil {
		return fmt.Errorf("failed to reparent children: %w", err)
	}

	// 4. Tombstone the budget lines instead of letting the FK cascade hard-delete them.
	//
	// Scoped through the owning budget rather than by custom_category_id alone. The CASCADE it
	// replaces had no user predicate either, and #1540 guarantees a budget_categories row's
	// custom_category_id belongs to the budget's own owner -- but that is an argument from an
	// application invariant, and this statement can now stand on its own.
	if _, err := tx.Exec(ctx, `
		UPDATE budget_categories
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE custom_category_id = $2
		  AND deleted_at IS NULL
		  AND budget_id IN (
		    -- tombstone-exempt(budgets b): OWNERSHIP SCOPING, not a listing (#1569 Task 9). This subquery
		    -- exists to answer "whose budget is this line on", and a TOMBSTONED budget is still
		    -- the same user's. Adding b.deleted_at IS NULL would silently skip any live budget
		    -- line hanging off a deleted budget, leaving it pointing at a deleted category --
		    -- the opposite of what this step is for.
		    SELECT b.id FROM budgets b WHERE b.user_id = $1
		  )
	`, userID, categoryID, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser); err != nil {
		return fmt.Errorf("failed to tombstone budget categories: %w", err)
	}

	// 5. Audit, inside the same transaction as the state change.
	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "custom_category.deleted", "custom_category", categoryID); err != nil {
			return fmt.Errorf("failed to audit category delete: %w", err)
		}
	}

	return tx.Commit(ctx)
}
