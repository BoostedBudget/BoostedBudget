package repository

import (
	"context"
	"fmt"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	retry_attempt_count, last_retry_at, last_retry_error_code, resolved_at`

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
func (r *plaidOrphanedItemRepository) List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("List: limit must be positive, got %d", limit)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("List: offset must not be negative, got %d", offset)
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM plaid_orphaned_items`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("List: count: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT `+orphanSelectColumns+`
		FROM plaid_orphaned_items
		ORDER BY orphaned_at DESC, id DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var out []domain.PlaidOrphanedItem
	for rows.Next() {
		item, scanErr := r.scanOrphan(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("List: %w", scanErr)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("List: %w", err)
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
func (r *plaidOrphanedItemRepository) scanOrphan(row rowScanner) (domain.PlaidOrphanedItem, error) {
	var (
		item                                    domain.PlaidOrphanedItem
		encItemID                               string
		encInstID, encInstName                  *string
		status, lastErrorCode, lastRetryErrCode *string
		encToken                                *string
	)

	if err := row.Scan(
		&item.ID, &item.LinkedItemID, &encItemID, &encInstID, &encInstName,
		&status, &lastErrorCode, &item.TerminalSince, &item.OrphanedAt,
		&encToken, &item.TokenExpiresAt, &item.TokenPurgedAt,
		&item.RetryAttemptCount, &item.LastRetryAt, &lastRetryErrCode, &item.ResolvedAt,
	); err != nil {
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
	return item, nil
}
