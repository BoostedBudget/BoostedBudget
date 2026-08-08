package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Sentinel errors for RecordOperatorResolution, which is the only write in this file whose caller has
// something distinct to SAY about each way it can decline. Issue #1497 item 2.
//
// ⚠ THEY EXIST BECAUSE "NOTHING HAPPENED" IS NOT AN ACCEPTABLE ANSWER HERE. MarkResolved can report
// its no-op as a plain `false` because its caller is a sweep, for which "somebody else already did
// this" is a perfectly good outcome nobody needs to be told about. This write's caller is a HUMAN who
// just typed a note: if the write is declined and the endpoint answers 200, the operator's note is
// silently discarded and they are left believing it was recorded. That is the defect Y1 fixed on the
// dead-letter surface an hour of this branch's history ago, and it is not being reintroduced here.
var (
	// ErrOrphanedItemNotFound: no plaid_orphaned_items row has that id. The endpoint answers 404.
	ErrOrphanedItemNotFound = errors.New("orphaned Plaid item record not found")
	// ErrOrphanedItemAlreadyAttested: the row already carries an operator attestation, which is
	// first-wins and is never overwritten. The endpoint answers 409 and names the existing attester,
	// so the second operator knows their note was NOT recorded and whose account stands.
	ErrOrphanedItemAlreadyAttested = errors.New("orphaned Plaid item record already carries an operator attestation")
)

// plaidOrphanedItemRepository is the read/update side of plaid_orphaned_items — the table that
// outlives a GDPR erasure so a Plaid Item stranded by one is not silently lost. Issue #1497.
//
// ⚠ IT DOES NOT WRITE NEW ROWS. The one and only producer is DeleteUserCascade's INSERT ... SELECT
// (profile_repository.go), which must run inside the cascade's own transaction, before the delete, or
// the record is not guaranteed to exist at all. An Insert method here would be an invitation to write
// that row from outside that transaction, which is the precise failure #1494 closed. Read that
// statement, not this file, for how a row is born.
type plaidOrphanedItemRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// PlaidOrphanedItemRepository is the interface the retry sweep, the purge sweep and the admin read all
// depend on. Split deliberately narrow — four verbs, one of which (PurgeExpiredTokens) must keep
// working even when everything else in this file is unused.
type PlaidOrphanedItemRepository interface {
	// PurgeExpiredTokens nulls the retained credential of every record whose token_expires_at has
	// passed, up to limit rows, and reports how many it took. See the method comment: this is the
	// guaranteed half of the retention bargain and it depends on nothing else in this file.
	PurgeExpiredTokens(ctx context.Context, asOf time.Time, limit int) (int, error)
	// GetRetryablePage returns up to limit records that still hold a usable, unexpired credential and
	// are not resolved, ordered for keyset pagination. after == nil starts from the beginning.
	GetRetryablePage(ctx context.Context, asOf time.Time, after *OrphanRetryCursor, limit int) ([]domain.PlaidOrphanedItem, error)
	// MarkResolved records a PROVED removal and purges the credential in the same statement. Reports
	// false when the record was already resolved (first-wins).
	MarkResolved(ctx context.Context, id string, asOf time.Time) (bool, error)
	// RecordOperatorResolution records an UNVERIFIED operator attestation — "I removed this Item by
	// hand in Plaid's dashboard" — and touches nothing else: not resolved_at, not the credential, not
	// the attempt count. Returns ErrOrphanedItemNotFound or ErrOrphanedItemAlreadyAttested rather than
	// declining silently. ⚠ It is NOT a variant of MarkResolved; see its method comment.
	RecordOperatorResolution(ctx context.Context, id, operatorID, note string, asOf time.Time) error
	// RecordRetryFailure records one failed attempt and its error code WITHOUT touching the
	// credential — only the deadline ends the retrying.
	RecordRetryFailure(ctx context.Context, id, errorCode string, asOf time.Time) error
	// List returns a page of records for the admin surface, newest first, plus the total count.
	List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error)
}

// NewPlaidOrphanedItemRepository builds the repository. enc is REQUIRED: every identifying column in
// this table is ciphertext produced by the same FieldEncryptor that encrypts linked_items, because the
// cascade copies those values verbatim rather than re-encrypting them (a SQL statement has no key).
func NewPlaidOrphanedItemRepository(db DB, enc *appcrypto.FieldEncryptor) *plaidOrphanedItemRepository { //nolint:revive // deliberate: unexported return mirrors NewLinkedItemRepository/NewProfileRepository, whose integration tests reach for the concrete type
	return &plaidOrphanedItemRepository{db: db, enc: enc}
}

// OrphanRetryCursor is the keyset cursor for GetRetryablePage: the (orphaned_at, id) of the last row
// the previous page yielded.
//
// orphaned_at alone is NOT a total order — nothing stops two erasures stranding items in the same
// microsecond, and a cursor on a non-total order can revisit or skip rows indefinitely. id is the
// tie-breaker that makes it total, and
// TestPlaidOrphanedItemRepository_Integration_GetRetryablePage_PagesThroughIdenticalTimestamps
// deliberately seeds three rows sharing one timestamp so the tie-breaker is actually exercised: a
// tie-breaker no test ever ties on is indistinguishable from an absent one.
type OrphanRetryCursor struct {
	OrphanedAt time.Time
	ID         string
}

// orphanSelectColumns is the column list every read in this file shares, so a new column cannot be
// added to one read and forgotten in the other.
const orphanSelectColumns = `
	id, linked_item_id, plaid_item_id_encrypted, institution_id_encrypted, institution_name_encrypted,
	linked_item_status, last_error_code, terminal_since, orphaned_at,
	access_token_encrypted, token_expires_at, token_purged_at,
	retry_attempt_count, last_retry_at, last_retry_error_code, resolved_at,
	operator_resolved_at, operator_resolved_by, operator_resolution_note`

// orphanListQuery is List's ONE statement: the page and its total, in a single snapshot.
//
// Hoisted to a package-level constant for the reason deletionResidueStatsQuery is (job_health_repository.go):
// it lets a plain unit test assert the single-statement property without a database, so the guard
// against somebody reintroducing a separate `SELECT COUNT(*)` cannot be skipped along with the
// integration suite. See TestOrphanListQuery_TakesItsTotalFromTheSameStatementAsThePage and List's
// own comment for why that property is correctness rather than tidiness.
//
// The window count is the LAST column so it appends cleanly to orphanSelectColumns and to
// scanOrphan's destination list.
const orphanListQuery = `SELECT ` + orphanSelectColumns + `, COUNT(*) OVER () AS total
	FROM plaid_orphaned_items
	ORDER BY orphaned_at DESC, id DESC
	LIMIT $1 OFFSET $2`

// PurgeExpiredTokens is THE GUARANTEED HALF OF THE RETENTION BARGAIN, and the reason it is a separate
// statement from anything to do with retrying. Issue #1497.
//
// ITS PREDICATE IS THE DEADLINE AND NOTHING ELSE. Not resolved_at, not retry_attempt_count, not
// last_retry_at, not whether a retry job exists, is registered, is leader-elected, or has ever run. If
// purging were a side effect of a successful retry — which is the obvious implementation and is wrong —
// then a broken, unwired or starved retry job would leave erased users' bank credentials in this
// database forever, which is this branch's signature defect (state marked, work never happened, mark
// survives forever) wearing a compliance hat. TestPlaidOrphanedItemRepository_Integration_
// PurgeExpiredTokens_PurgesWithNoRetryEverHavingRun constructs no retry job at all.
//
// BOTH COLUMNS MOVE IN ONE STATEMENT. token_purged_at is what every "the credential is gone" claim in
// this system reads, and migration 000135's CHECK constraint refuses the pairing where the stamp is
// set and the ciphertext is not cleared — so there is no ordering, and no partial failure, in which the
// claim can be false. `access_token_encrypted IS NOT NULL` in the predicate makes the statement
// idempotent and keeps token_purged_at from being re-stamped on a row already purged.
//
// asOf rather than the database's own NOW(), for the reason #1466 threaded asOf everywhere else: a
// predicate that reads the clock internally cannot be tested at the boundary it exists to enforce.
// The same value stamps token_purged_at, so one instant decides and records the purge.
//
// BOUNDED, like every other sweep on this branch. limit caps one statement's write volume; a remainder
// is simply deferred to the next run, which finds it unchanged (an expired deadline does not un-expire).
// Ordered by the deadline so the most overdue credential is always taken first — the direction that
// minimises worst-case retention if the cap is ever actually reached.
//
// ⚠ WHAT THIS THEREFORE DOES AND DOES NOT PROMISE. It destroys the credential at the FIRST CALL AT OR
// AFTER the deadline — so, given its caller runs every domain.OrphanedItemTokenPurgeInterval (1 hour),
// the credential normally still EXISTS for up to an hour past its deadline, and longer whenever a sweep
// does not run or does not drain (limit reached, downtime, leader election, a failed run). See that
// constant for the full list; the bound is per sweep, not absolute. It does NOT make the credential
// unusable at the deadline; nothing
// waits for this statement for that. GetRetryablePage below and the retry sweep's per-row recheck
// enforce unusability exactly, on a fresh clock, independently of this having ever run. Two halves of
// one deadline, deliberately of different strength: state the bound, do not round it to zero.
func (r *plaidOrphanedItemRepository) PurgeExpiredTokens(ctx context.Context, asOf time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("PurgeExpiredTokens: limit must be positive, got %d", limit)
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE plaid_orphaned_items
		SET access_token_encrypted = NULL,
		    token_purged_at = $1
		WHERE id IN (
			SELECT id FROM plaid_orphaned_items
			WHERE access_token_encrypted IS NOT NULL
			  AND token_expires_at <= $1
			ORDER BY token_expires_at, id
			LIMIT $2
		)`, asOf, limit)
	if err != nil {
		return 0, fmt.Errorf("PurgeExpiredTokens: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// GetRetryablePage selects the records an automated /item/remove retry may act on.
//
// THE PREDICATE IS INDEPENDENT OF THE PURGE SWEEP, deliberately duplicating its deadline check
// (`token_expires_at > asOf`) rather than relying on the token having been purged. The two guards
// answer different questions — "may this credential still be used?" and "must this credential be
// destroyed?" — and the retry must fail closed on the first even if the second has never run. Without
// the duplication, a purge sweep that was broken for a week would have the retry presenting
// week-expired credentials to Plaid.
//
// resolved_at IS NULL keeps a proved removal from being re-attempted; access_token_encrypted IS NOT
// NULL is there because there is nothing to send otherwise. Note what is NOT here: no attempt-count
// ceiling and no error-code exclusion. Nothing in this system gives up on a count, and the one
// genuinely hopeless case (an undecryptable token) is handled by the caller — see the retry worker —
// because it cannot be expressed in SQL without the key.
//
// ⚠ THAT ABSENCE IS WHY THIS SWEEP NEEDS A CROSS-RUN CURSOR (Issue #1477), and it is the sharpest
// case of the four. With no ceiling and no backoff, a record whose /item/remove keeps failing is
// written back by RecordRetryFailure WITHOUT its orphaned_at moving and WITHOUT leaving this
// predicate — so it holds the head of the very first page of every run. Before #1477 the caller
// restarted from `after == nil` each run, so a backlog larger than its cap
// (maxOrphanRetriesPerSweep, queue/jobs/plaid_orphan_retry.go) meant every run re-spent its whole
// budget on the same oldest records and nothing past the cap was EVER retried — until those records'
// credentials expired, after which no automated path can remove their Items at all. The caller now
// PERSISTS the timestamp key a capped run stopped at (the sweep_cursors table —
// repository/sweep_cursor_repository.go) and passes it back here as the next run's `after`.
//
// Unlike item_cleanup's, this starving head needs no database failure: an ordinary Plaid failure,
// the case this sweep exists for, is enough. The replacement guarantee is BOUNDED, not absolute — it
// is stated once, in full, in repository/sweep_cursor_repository.go's header. Do not restate it as
// "no starvation".
//
// ⚠ AND operator_resolved_at IS DELIBERATELY NOT IN THIS PREDICATE — A DECISION, NOT AN OMISSION.
// Issue #1497 item 2 added an operator attestation ("I removed this Item by hand in Plaid's
// dashboard"), and it is genuinely arguable whether this sweep should then stop re-attempting the row.
// It does not, for four reasons, and the alternative's merit is stated rather than dismissed:
//
//  1. AN ATTESTATION IS NOT PROOF. This whole column exists because a human's claim and a Plaid
//     response are different facts (see domain.OrphanedItemOperatorAttested). Letting the claim
//     control machinery would treat it as the proof it explicitly is not — the exact conflation the
//     design was chosen to prevent, and it would arrive by the back door of a WHERE clause.
//  2. RETRYING IS THE ONLY THING THAT CAN EVER UPGRADE THE CLAIM. If the operator really did remove
//     the Item, the next sweep's /item/remove comes back ITEM_NOT_FOUND, the service treats that as
//     the same outcome as a success, and MarkResolved stamps resolved_at. So continuing to retry
//     SELF-HEALS an honest attestation into a proved one at zero operator cost — and, if the claim
//     was mistaken, actually removes the Item that is still billing. Excluding attested rows forecloses
//     both outcomes permanently.
//  3. THE FAILURE DIRECTIONS ARE NOT SYMMETRIC. Retrying an Item somebody already removed costs one
//     Plaid call per row per day against a table migration 000134 argues holds single digits of rows,
//     bounded anyway by the 30-day credential retention. NOT retrying an Item somebody only THOUGHT
//     they removed costs an erased user's bank connection continuing to exist, and possibly to bill,
//     with the row wearing a reassuring label. One mistaken click would permanently silence the only
//     automated path — a mark outliving the work, which is this branch's signature defect.
//  4. THE HUMAN IS RESPECTED WHERE IT ACTUALLY HELPS THEM: in what they are ASKED to do. AdminState
//     reports 'operator_attested' rather than 'manual_intervention_required', so nobody is told to
//     redo the work, and the row leaves the manual-intervention count and gauge. Deference belongs in
//     the instruction to the operator, not in the machinery's willingness to check.
//
// THE COUNTER-ARGUMENT, IN FAIRNESS: continuing to retry means an attested row keeps accumulating
// retry_attempt_count and can therefore trip the re-attempt gauge's intended >20 alert, which an
// operator may read as "my attestation was ignored". That is accepted deliberately — a claim nothing
// has been able to confirm in twenty attempts IS worth a second look, and the alternative is a gauge
// an unverified sentence can switch off. It is documented in
// domain.SummarisePlaidOrphanedItems' comment beside the count it affects.
//
// TestPlaidOrphanedItemRepository_Integration_GetRetryablePage_StillReturnsOperatorAttestedRows pins
// this, with a proved-resolved control alongside it, so the decision cannot be reversed by accident.
func (r *plaidOrphanedItemRepository) GetRetryablePage(ctx context.Context, asOf time.Time, after *OrphanRetryCursor, limit int) ([]domain.PlaidOrphanedItem, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("GetRetryablePage: limit must be positive, got %d", limit)
	}

	query := `SELECT ` + orphanSelectColumns + `
		FROM plaid_orphaned_items
		WHERE resolved_at IS NULL
		  AND access_token_encrypted IS NOT NULL
		  AND token_expires_at > $1`
	args := []interface{}{asOf}
	if after != nil {
		// Row-value comparison, which is exactly the (orphaned_at, id) lexicographic order the ORDER BY
		// below imposes — spelled as a tuple rather than as an expanded OR chain so the two cannot drift.
		query += ` AND (orphaned_at, id) > ($2, $3)`
		args = append(args, after.OrphanedAt, after.ID)
	}
	query += ` ORDER BY orphaned_at, id LIMIT ` + fmt.Sprintf("%d", limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetRetryablePage: %w", err)
	}
	defer rows.Close()

	var out []domain.PlaidOrphanedItem
	for rows.Next() {
		item, scanErr := r.scanOrphan(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("GetRetryablePage: %w", scanErr)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetRetryablePage: %w", err)
	}
	return out, nil
}

// MarkResolved records that the Item really is gone from Plaid, and purges the credential in the SAME
// statement because the purpose it was retained for is spent the instant the removal succeeds.
//
// ONE STATEMENT, NOT TWO. Split into "mark resolved" then "purge", a failure between them leaves a row
// that says the work is done while still holding a bank credential — and nothing would ever look at it
// again, because every retry predicate excludes resolved rows. The credential would then survive until
// the purge sweep's deadline, which is the correct backstop but not an excuse for creating the state.
//
// FIRST-WINS on resolved_at (`AND resolved_at IS NULL`), mirroring MarkPlaidRemovalFailed's refusal to
// overwrite: "when was this Item removed" must stay answerable, and a duplicate in-flight retry must
// not move the timestamp. The false return is the legitimate "somebody else already did this", not an
// error.
func (r *plaidOrphanedItemRepository) MarkResolved(ctx context.Context, id string, asOf time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE plaid_orphaned_items
		SET resolved_at = $2,
		    access_token_encrypted = NULL,
		    token_purged_at = COALESCE(token_purged_at, $2),
		    last_retry_at = $2,
		    last_retry_error_code = NULL,
		    retry_attempt_count = retry_attempt_count + 1
		WHERE id = $1 AND resolved_at IS NULL`, id, asOf)
	if err != nil {
		return false, fmt.Errorf("MarkResolved: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RecordOperatorResolution records that AN OPERATOR SAYS they removed this Item by hand in Plaid's own
// dashboard. Issue #1497 item 2.
//
// ⚠ IT IS NOT A VARIANT OF MarkResolved AND MUST NEVER BE MADE ONE. MarkResolved's documented meaning
// is "a PROVED removal": an automated /item/remove came back success or ITEM_NOT_FOUND, i.e. Plaid
// itself answered, which is why it also purges the credential in the same statement. This statement
// records an UNVERIFIED HUMAN ASSERTION. Nothing checked it; nothing can check it retroactively.
// Recording a claim in a proof's column is precisely how linked_items.status = 'replaced' came to be
// believed as evidence that an Item had been removed at Plaid — the defect this table exists to undo —
// so the two live in separate columns and this write touches NONE of the proof's artefacts:
//
//   - NOT resolved_at. That column answers "when did Plaid confirm this gone", and it must stay
//     answerable and stay true. A row that is attested and later actually proved removed carries both,
//     at their true strengths, and domain.PlaidOrphanedItem.AdminState reports the stronger one.
//   - NOT access_token_encrypted and NOT token_purged_at. A claim licenses no purge: it proves nothing,
//     and destroying the credential would remove the only means by which the claim could ever be
//     CONFIRMED (a retry needs a token). The credential's life is governed by token_expires_at and by
//     PurgeExpiredTokens alone — the one guarantee this feature's GDPR defensibility rests on, and it
//     is not being made to depend on what somebody typed into a form.
//   - NOT retry_attempt_count and NOT last_retry_at. An operator acting in Plaid's dashboard is not an
//     automated attempt, and counting it as one would corrupt the one signal (#1497 item 4's gauge)
//     that reports how hard this system has been trying.
//
// FIRST-WINS on operator_resolved_at, mirroring MarkResolved and MarkPlaidRemovalFailed: "who said
// this, and when" must stay answerable, so a later attestation cannot overwrite an earlier one.
//
// ⚠ AND THE DECLINE IS REPORTED, not swallowed as a bare false. Both no-op cases return a sentinel
// (ErrOrphanedItemNotFound / ErrOrphanedItemAlreadyAttested) because the caller here is a human who
// has just written a note: answering "fine" while discarding it is how an operator comes to believe a
// record exists that does not. The second statement runs ONLY on the rare zero-row path, so the happy
// path is still one round-trip.
func (r *plaidOrphanedItemRepository) RecordOperatorResolution(ctx context.Context, id, operatorID, note string, asOf time.Time) error {
	// Guarded here as well as at the API layer because migration 000137's CHECK constraint (tightened
	// by 000139) enforces "an attestation is never anonymous" and a violation would surface as an
	// opaque 23514 rather than as the thing that is actually wrong.
	//
	// ⚠ TRIMMED BEFORE IT IS VALIDATED, AND THE TRIMMED VALUE IS WHAT IS STORED. An exact-"" check
	// lets "   " and "\t" through, and those are anonymous attestations by any reading that matters —
	// which was the whole defect 000139 exists to close on the database side. Storing the trimmed form
	// as well means two attestations can never differ only by invisible characters, so "who said this"
	// stays a question with one answer.
	operatorID = strings.TrimSpace(operatorID)
	if operatorID == "" {
		return fmt.Errorf("RecordOperatorResolution: an attestation must name the operator making it")
	}

	tag, err := r.db.Exec(ctx, `
		UPDATE plaid_orphaned_items
		SET operator_resolved_at = $2,
		    operator_resolved_by = $3,
		    operator_resolution_note = $4
		WHERE id = $1 AND operator_resolved_at IS NULL`, id, asOf, operatorID, nullIfEmptyNote(note))
	if err != nil {
		return fmt.Errorf("RecordOperatorResolution: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	// Zero rows means either "no such record" or "already attested", and the two need different
	// answers from the endpoint (404 vs 409). Neither is an internal error.
	var exists bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plaid_orphaned_items WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("RecordOperatorResolution: disambiguate no-op: %w", err)
	}
	if !exists {
		return ErrOrphanedItemNotFound
	}
	return ErrOrphanedItemAlreadyAttested
}

// nullIfEmptyNote keeps an empty note out of the column as NULL rather than as '', so "no note" has
// one representation. Migration 000137's CHECK permits a NULL note beside an attestation (a legitimate
// repair may have none); the API layer is where a non-empty note is required of new writes.
//
// ⚠ WHITESPACE-ONLY COUNTS AS EMPTY, and the trimmed form is what is stored. "   " stored as a note
// renders on the admin surface as an explanation that explains nothing while being, to code, a note
// that exists — the two-representations-of-one-fact defect this branch has now closed several times.
// Migration 000139 makes the same rule structural (plaid_orphaned_items_operator_note_is_not_blank),
// so this function is now the normalisation and the constraint is the guarantee, rather than this
// function being the only thing standing between a blank note and the column.
func nullIfEmptyNote(note string) *string {
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// RecordRetryFailure records one failed attempt: the count, the instant, and the error code.
//
// IT DOES NOT TOUCH THE CREDENTIAL, and that is the deliberate asymmetry with MarkResolved. A failure
// is not evidence that the token is useless — Plaid outages, rate limits and 5xx all land here — and
// only token_expires_at ends the retention. Nor does it write last_error_code: migration 000134 defines
// that column as the code AT THE MOMENT OF ERASURE, which is the diagnostic explaining why the row
// exists at all, so retries write last_retry_error_code instead and the two facts stay separable.
//
// `AND resolved_at IS NULL` stops a late-arriving failure from a duplicate in-flight attempt reopening
// a record whose Item is already gone.
func (r *plaidOrphanedItemRepository) RecordRetryFailure(ctx context.Context, id, errorCode string, asOf time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE plaid_orphaned_items
		SET retry_attempt_count = retry_attempt_count + 1,
		    last_retry_at = $2,
		    last_retry_error_code = $3
		WHERE id = $1 AND resolved_at IS NULL`, id, asOf, errorCode)
	if err != nil {
		return fmt.Errorf("RecordRetryFailure: %w", err)
	}
	// A zero-row result is not an error: it means the record was resolved (or deleted) between the
	// attempt and this write, and in both cases there is nothing to record and nothing to retry.
	return nil
}

// List returns a page of orphan records for the admin surface, plus the total count.
//
// NEWEST FIRST, offset-paginated rather than keyset-paginated, and that is a considered difference from
// GetRetryablePage: this is a human reading a list that migration 000134 expects to hold single digits
// of rows, where a page boundary shifting under a re-sort costs nothing, whereas the sweep must not
// skip a row it has never processed. Do not "unify" them.
//
// It filters NOTHING. Resolved rows appear (as history), purged rows appear (as the manual-intervention
// work list), and the caller derives which of the two operator stories each row is in via
// domain.PlaidOrphanedItem.AdminState. Filtering here would make the state machine's inputs invisible
// to the surface that reports it.
//
// ⚠ THE TOTAL AND THE PAGE COME FROM ONE STATEMENT, AND THAT IS A CORRECTNESS PROPERTY, NOT A
// ROUND-TRIP OPTIMISATION. This used to run `SELECT COUNT(*)` and then the page as two separate,
// non-transactional statements. Under READ COMMITTED each takes its OWN snapshot, so a row inserted
// between them makes the two answers describe different tables — and the direction that failure runs
// is the one that matters here. domain.SummarisePlaidOrphanedItemsBounded refuses to publish a state
// count when `total` says the table is past domain.PlaidOrphanedItemHealthScanCap, exactly so a
// partial scan is never reported as a total; a `total` read BEFORE the page could say 500 while the
// page it is paired with was truncated out of a 502-row table, and the reader would publish a
// silent UNDER-COUNT of the manual-intervention population — the one direction a compliance surface
// must never err in, and the precise case the cap exists to prevent.
//
// `COUNT(*) OVER ()` is evaluated after ORDER BY but BEFORE LIMIT/OFFSET, so it is the total over the
// whole result set the page was cut from, taken in the same snapshot as the page itself. Nothing
// about isolation levels, retries or transaction bookkeeping is required for that: one statement is
// one snapshot. Do not "simplify" this back into a separate COUNT.
//
// THE ONE PATH THAT STILL NEEDS A SECOND STATEMENT is an EMPTY page at a NON-ZERO offset, where the
// window function has no row to ride out on and would report 0 for a table that is not empty. That
// is a caller who paged past the end, it has no page for the total to be inconsistent WITH, and it
// is unreachable from the two metrics readers (both read offset 0, where zero rows is a true zero).
// See SummarisePlaidOrphanedItemsBounded's own guard, which fails closed on that shape anyway.
func (r *plaidOrphanedItemRepository) List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("List: limit must be positive, got %d", limit)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("List: offset must not be negative, got %d", offset)
	}

	rows, err := r.db.Query(ctx, orphanListQuery, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var out []domain.PlaidOrphanedItem
	var total int
	for rows.Next() {
		// The extra destination is the window count, identical on every row of the page.
		item, scanErr := r.scanOrphan(rows, &total)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("List: %w", scanErr)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("List: %w", err)
	}

	if len(out) == 0 && offset > 0 {
		// See the method comment: the only shape where the window count cannot speak for itself.
		if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM plaid_orphaned_items`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("List: count for empty page past offset %d: %w", offset, err)
		}
	}
	return out, total, nil
}

// rowScanner is the minimum both pgx.Rows and pgx.Row satisfy, so scanOrphan serves every read here.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanOrphan maps one row into the domain type and DECRYPTS the four ciphertext columns.
//
// ⚠ DECRYPTION FAILURE IS NOT AN ERROR FOR THE TOKEN, and that asymmetry is the point. If
// plaid_item_id cannot be decrypted the record is unusable to a human as well as to code, so that is a
// hard error. But an undecryptable ACCESS TOKEN — a rotated key — must not fail the whole page: it
// would take out every other record in the same sweep, including ones whose tokens decrypt fine. So it
// yields AccessToken == "" plus TokenUndecryptable == true, and the caller (the retry worker) records
// domain.OrphanTokenUndecryptableCode against that single row and moves on. The credential is NOT
// purged on this path — see OrphanTokenUndecryptableCode's doc comment for why a missing key is
// treated as an operations problem that may come back, not as proof the token is worthless.
//
// FieldEncryptor.DecryptString PASSES THROUGH any value lacking the 'ENC$' prefix (dual-read support
// for rows predating migration 000053), so a plaintext legacy value reads correctly here too — the
// same property migration 000134's comment documents for plaid_item_id_encrypted.
//
// extraDests are destinations for columns a caller SELECTed after orphanSelectColumns, in the order
// they appear there. Only List uses it, for its `COUNT(*) OVER ()` window total; the trailing
// position is what lets the shared column list stay the shared column list rather than forking into
// a with-total and a without-total copy that could drift.
func (r *plaidOrphanedItemRepository) scanOrphan(row rowScanner, extraDests ...interface{}) (domain.PlaidOrphanedItem, error) {
	var (
		item                                    domain.PlaidOrphanedItem
		encItemID                               string
		encInstID, encInstName                  *string
		status, lastErrorCode, lastRetryErrCode *string
		encToken                                *string
		operatorBy, operatorNote                *string
	)

	dests := []interface{}{
		&item.ID, &item.LinkedItemID, &encItemID, &encInstID, &encInstName,
		&status, &lastErrorCode, &item.TerminalSince, &item.OrphanedAt,
		&encToken, &item.TokenExpiresAt, &item.TokenPurgedAt,
		&item.RetryAttemptCount, &item.LastRetryAt, &lastRetryErrCode, &item.ResolvedAt,
		&item.OperatorResolvedAt, &operatorBy, &operatorNote,
	}
	dests = append(dests, extraDests...)

	if err := row.Scan(dests...); err != nil {
		return domain.PlaidOrphanedItem{}, fmt.Errorf("scan orphaned item: %w", err)
	}

	plainItemID, err := r.enc.DecryptString(encItemID)
	if err != nil {
		return domain.PlaidOrphanedItem{}, fmt.Errorf("decrypt plaid item id for orphan record %s: %w", item.ID, err)
	}
	item.PlaidItemID = plainItemID

	if encInstID != nil {
		plain, decErr := r.enc.DecryptString(*encInstID)
		if decErr != nil {
			return domain.PlaidOrphanedItem{}, fmt.Errorf("decrypt institution id for orphan record %s: %w", item.ID, decErr)
		}
		item.InstitutionID = plain
	}
	if encInstName != nil {
		plain, decErr := r.enc.DecryptString(*encInstName)
		if decErr != nil {
			return domain.PlaidOrphanedItem{}, fmt.Errorf("decrypt institution name for orphan record %s: %w", item.ID, decErr)
		}
		item.InstitutionName = plain
	}
	if encToken != nil {
		plain, decErr := r.enc.DecryptString(*encToken)
		if decErr != nil {
			item.TokenUndecryptable = true
		} else {
			item.AccessToken = plain
		}
	}

	if status != nil {
		item.LinkedItemStatus = *status
	}
	if lastErrorCode != nil {
		item.LastErrorCode = *lastErrorCode
	}
	if lastRetryErrCode != nil {
		item.LastRetryErrorCode = *lastRetryErrCode
	}
	// The operator attestation's three columns are read PLAINTEXT and undecrypted, deliberately:
	// operator_resolved_by is the attesting STAFF member's Clerk id rather than the erased subject's,
	// and the note is staff-written prose. Neither is the encrypted-identifier class this table's other
	// columns belong to — see migration 000137's prose on why. Issue #1497 item 2.
	if operatorBy != nil {
		item.OperatorResolvedBy = *operatorBy
	}
	if operatorNote != nil {
		item.OperatorResolutionNote = *operatorNote
	}
	return item, nil
}
