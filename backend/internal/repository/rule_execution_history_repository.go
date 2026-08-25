package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 5 (#1572) — data access for the automation audit trail.
//
// `rule_execution_history` was created by migration 000020, given an ON DELETE SET NULL FK to
// transactions by 000157, and given user_id / rule_name / definition_snapshot plus its own GDPR
// erasure path by 000164. Through all of that it had NO Go code at all: both those migrations say
// in their own comments that `grep -rn 'rule_execution_history' backend --include='*.go'` matches
// only migrations and test fixtures. That is no longer true as of this file, and the claim in
// 000157's and 000164's headers should be read as "was true when written".
//
// The consequence of having no repository was not merely that nothing wrote the table. Nothing READ
// it either — no service, no endpoint, no UI — so even a writer on its own would have shipped
// something with no observable effect. boost_executions is the template followed here: writer, a
// per-user read, and an endpoint.

// RuleExecutionHistoryRepository persists and reads the automation audit trail.
type RuleExecutionHistoryRepository interface {
	Create(ctx context.Context, execution *domain.RuleExecution) error
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.RuleExecution, int, error)
}

type ruleExecutionHistoryRepository struct {
	db DB
}

// NewRuleExecutionHistoryRepository creates a RuleExecutionHistoryRepository.
func NewRuleExecutionHistoryRepository(db DB) RuleExecutionHistoryRepository {
	return &ruleExecutionHistoryRepository{db: db}
}

// ruleExecutionColumns is the single column list both reads use, so a new column cannot be added to
// one scan and forgotten in the other.
const ruleExecutionColumns = `id, rule_id, transaction_id, user_id, rule_name,
	definition_snapshot, action_type, result, executed_at`

// Create writes one execution row.
//
// ⚠ ON CONFLICT (rule_id, transaction_id) DO NOTHING — THE FIRST EXECUTION WINS, and the choice is
// forced. `unique_rule_transaction UNIQUE (rule_id, transaction_id)` means the table as it stands
// CANNOT hold more than one execution of a rule against a transaction, so neither conflict action
// is lossless: DO NOTHING discards every later execution, DO UPDATE keeps the latest and OVERWRITES
// the original point-in-time snapshot. Lossless history would need an execution/version key in the
// unique constraint, which is a schema change and is not this task.
//
// DO NOTHING is chosen because DO UPDATE would break the invariant the snapshot exists to hold —
// boost_execution_repository.go states it plainly: "the point of a snapshot is that a later rename,
// edit or purge does not rewrite what history says happened." A conflict action that rewrites
// rule_name and definition_snapshot in place is exactly the drift these columns were added to
// prevent. Given the table can only keep one row, keeping the FIRST, with the definition as it was
// the first time the automation fired, is the answer that does not silently falsify a row.
//
// A conflict is in any case near-unreachable on the only writer that exists: EmitBatchSideEffects
// is gated on CreateBatch's insertedIDs, which is derived from `xmax::text::bigint = 0` — genuinely
// new rows only. A second execution against the same transaction id would need that id to be
// INSERTed twice, which the transactions primary key forbids. The clause is a guard against a
// future second writer, not a hot path.
//
// executed_at is clock_timestamp(), NOT the column's NOW() default. now() is transaction-start
// time, so a whole batch written inside one transaction would share a single timestamp and the
// ORDER BY in GetByUserID would be a coin toss between them.
func (r *ruleExecutionHistoryRepository) Create(ctx context.Context, execution *domain.RuleExecution) error {
	if execution.ID == "" {
		execution.ID = uuid.New().String()
	}

	// nil (not []byte{}) so the column is SQL NULL rather than invalid JSON when unset.
	var snapshot []byte
	if len(execution.DefinitionSnapshot) > 0 {
		snapshot = execution.DefinitionSnapshot
	}

	query := `
		INSERT INTO rule_execution_history (
			id, rule_id, transaction_id, user_id, rule_name, definition_snapshot,
			action_type, result, executed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, clock_timestamp())
		ON CONFLICT (rule_id, transaction_id) DO NOTHING
		RETURNING id, executed_at
	`

	err := r.db.QueryRow(ctx, query,
		execution.ID,
		execution.RuleID,
		execution.TransactionID,
		execution.UserID,
		execution.RuleName,
		snapshot,
		execution.ActionType,
		execution.Result,
	).Scan(&execution.ID, &execution.ExecutedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// DO NOTHING fired: this rule already has a row for this transaction, and that earlier row
		// — with its earlier snapshot — is deliberately left untouched. Not an error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to record rule execution: %w", err)
	}
	return nil
}

// GetByUserID returns the user's automation history, newest first, with the total row count.
//
// FILTERS ON THE ROW'S OWN user_id, never on a join to categorization_rules. That is the whole
// point of the column migration 000164 added: fk_rule_execution_history_rule_id is ON DELETE SET
// NULL and CategorizationRuleRepository.Delete still hard-deletes, so deleting a rule in the UI
// leaves its executions with rule_id NULL. A read keyed on rule_id would make every one of those
// rows invisible — retention that reads as deletion, which is the failure this whole plan is about.
// Such a row still carries rule_name and definition_snapshot, so a client can say what the
// automation was.
//
// The id tiebreak is not decoration: clock_timestamp() has microsecond resolution and two rows
// written for the same transaction in the same batch can land in the same microsecond.
func (r *ruleExecutionHistoryRepository) GetByUserID(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]*domain.RuleExecution, int, error) {
	var total int
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count rule executions: %w", err)
	}

	rows, err := r.db.Query(ctx, `
		SELECT `+ruleExecutionColumns+`
		FROM rule_execution_history
		WHERE user_id = $1
		ORDER BY executed_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query rule executions: %w", err)
	}
	defer rows.Close()

	executions := make([]*domain.RuleExecution, 0)
	for rows.Next() {
		execution := &domain.RuleExecution{}
		var snapshot []byte
		if err := rows.Scan(
			&execution.ID,
			&execution.RuleID,
			&execution.TransactionID,
			&execution.UserID,
			&execution.RuleName,
			&snapshot,
			&execution.ActionType,
			&execution.Result,
			&execution.ExecutedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan rule execution: %w", err)
		}
		if len(snapshot) > 0 {
			execution.DefinitionSnapshot = snapshot
		}
		executions = append(executions, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate rule executions: %w", err)
	}

	return executions, total, nil
}
