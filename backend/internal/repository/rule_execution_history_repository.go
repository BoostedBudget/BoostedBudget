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
// 000157's and 000164's headers should be read as "was true when written". Both migration files now
// carry a dated SUPERSEDED note saying so (#1569 T8): applied migrations are not edited for SCHEMA,
// but a comment-only amendment costs nothing and is the only way a reader who opens the migration
// finds the correction — leaving it solely here means it is discoverable only from the side that
// already knows.
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
// one scan and forgotten in the other. Alias-qualified for the LEFT JOIN in visibleRuleExecutions.
const ruleExecutionColumns = `h.id, h.rule_id, h.transaction_id, h.user_id, h.rule_name,
	h.definition_snapshot, h.action_type, h.result, h.executed_at`

// visibleRuleExecutions is the FROM + WHERE both the count and the page share, so the total can
// never disagree with the rows it counts.
//
// ⚠ THE LEFT JOIN IS NOT DECORATION — it fixes a duplicate every settling pending transaction
// produced (R74, review round 1 D3). Rules run BEFORE CreateBatch, and a posted twin routinely
// arrives already carrying category_source='rule' (transaction_repository.go's
// settlementCategoryMoveCond explains why at length). The pending row and its posted twin have
// DIFFERENT ids, so unique_rule_transaction UNIQUE (rule_id, transaction_id) does not collide:
// rule R legitimately gets one row for the pending id and another for the posted id, for ONE
// charge. Both came back from this read, and the pending one pointed at a transaction that every
// other read in the app hides via `deleted_at IS NULL`. The trail said the automation fired twice.
//
// THE ROW IS RETAINED, NOT DELETED. This is a default-visibility decision, exactly like the
// tombstone itself: the history of what a rule did to the pending row is still there, still
// GDPR-erasable through user_id, and still returned by any future read that asks for it. What it
// is not is a second line item in the user's automation history for a single charge.
//
// THE `h.transaction_id IS NULL` LEG IS EXPLICIT, NOT LOAD-BEARING, and saying which is the point.
// fk_rule_execution_history_transaction_id is ON DELETE SET NULL (migration 000157), so a
// hard-deleted transaction leaves its execution row with a NULL pointer. Such a row must stay
// visible — NULL means "the transaction is gone", never "no transaction" — and it WOULD stay
// visible without this leg, because an unmatched LEFT JOIN yields a NULL `t.deleted_at` and
// `NULL IS NULL` is TRUE. The leg is kept because that is a coincidence of three-valued logic, and
// the day someone "simplifies" this to an INNER JOIN the coincidence stops holding and every
// orphaned execution silently disappears. Pinned by
// TestRuleExecutionHistory_OrphanedTransactionPointerStaysVisible_Integration.
//
// ⚠ THE TOMBSTONE LEG IS transactionTombstoneCondition, NOT A SECOND NOTION OF VISIBILITY (#1569
// T8.2, Item A). It used to be a hand-written `t.deleted_at IS NULL`, which hides EVERY tombstone
// whatever its reason — far broader than the settlement de-duplication described above, and wrong
// for the one reason that has no twin. `provider_removed_posted` is a bank reversal of a row that
// had already POSTED: the transaction itself stays surfacable (domain.TxnDeletedProviderRemovedPosted,
// TransactionFilters.IncludeReversed), so a trail row pointing at it is history the app is still
// showing the user, and hiding it is retention that reads as deletion. Calling the shared renderer
// with includeReversed=true means this read admits EXACTLY the one reason transactions admits and
// no other — provider_removed_pending, the settled twin, stays hidden, which is the whole point of
// the de-duplication. If that widening ever changes, it changes in one place.
//
// Pinned in both directions: TestRuleExecutionHistory_PostedReversalStaysVisible_Integration (a
// reversal's trail is returned) and TestRuleExecutionHistory_SettlementDoesNotShowTheSameChargeTwice_Integration
// (a settled pending's is not).
var visibleRuleExecutions = fmt.Sprintf(`
		FROM rule_execution_history h
		LEFT JOIN transactions t ON t.id = h.transaction_id
		-- tombstone-exempt(transactions t): transactionTombstoneCondition("t", true) is interpolated
		-- at the %%s below. It is the SHARED renderer, deliberately called with includeReversed=true:
		-- this read hides a settled pending's trail row and keeps a posted reversal's, matching
		-- exactly what the transactions reads themselves surface.
		WHERE h.user_id = $1
		  AND (h.transaction_id IS NULL OR %s)`,
	transactionTombstoneCondition("t", true))

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
// ⚠ AN EARLIER VERSION OF THIS COMMENT CALLED A CONFLICT "near-unreachable", on the grounds that
// EmitBatchSideEffects is gated on insertedIDs and a transaction id cannot be INSERTed twice. That
// is factually true and it was the wrong thing to say, because it reads as reassurance while
// describing the exact mechanism behind a real duplicate: ids do not repeat, so a settling pending
// transaction and its posted twin are two DIFFERENT ids, the constraint never fires, and one charge
// got two trail rows. The uniqueness of transaction ids is what CAUSED D3, not what prevented it.
// Read visibleRuleExecutions before trusting this clause to deduplicate anything.
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
		//
		// ⚠ CLEAR THE ID (D7, review round 1). RETURNING yields nothing on a conflict, so without
		// this the caller is handed a freshly minted UUID that names no row in the database and an
		// ExecutedAt of the zero time, and cannot tell that from a successful write. An empty ID
		// after a nil-error Create means exactly one thing: a row for this (rule, transaction)
		// already existed and was kept. ExecutedAt stays zero for the same reason — this call did
		// not write a row, so it has no execution time to report.
		execution.ID = ""
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
// EXCLUDES EXECUTIONS AGAINST A SETTLED PENDING TRANSACTION — see visibleRuleExecutions for why,
// which is the settlement duplicate, not tidiness. It does NOT exclude every tombstone: a posted
// transaction the bank later reversed stays visible here, because it stays visible everywhere else.
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
		`SELECT COUNT(*)`+visibleRuleExecutions, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count rule executions: %w", err)
	}

	rows, err := r.db.Query(ctx, `
		SELECT `+ruleExecutionColumns+visibleRuleExecutions+`
		ORDER BY h.executed_at DESC, h.id DESC
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
