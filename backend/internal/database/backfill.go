package database

// #1001: BackfillEncryption — startup safety net for pre-encryption rows.
//
// When field-level encryption is deployed but the encrypt-migrate backfill tool hasn't
// been run yet, pre-existing rows retain plaintext email/clerk_user_id values with
// NULL blind indexes. All lookups query by blind index, so these rows are invisible and
// trigger duplicate account creation on next login.
//
// This function runs at server startup, detects unmigrated rows, and encrypts them
// in-place before the HTTP server begins accepting traffic. This prevents the
// duplicate-account bug even if the manual encrypt-migrate step is skipped.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// backfillQuerier is the minimal DB interface needed by backfillAccountsUserAccountIDsWithDB.
// Both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy it.
type backfillQuerier interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
}

// BackfillEncryption checks for unmigrated plaintext rows and encrypts them at startup.
// It runs synchronously before the server begins accepting requests. If encryption fails
// for any row, it logs the error and returns it (caller should exit).
func BackfillEncryption(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	if db == nil || enc == nil {
		return nil // encryption not configured (local dev without ENCRYPTION_KEY)
	}

	if err := backfillUsers(ctx, db, enc); err != nil {
		return fmt.Errorf("users backfill: %w", err)
	}
	if err := backfillUserIdentities(ctx, db, enc); err != nil {
		return fmt.Errorf("user_identities backfill: %w", err)
	}
	return nil
}

func backfillUsers(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	rows, err := db.Query(ctx, `
		SELECT id, email, clerk_user_id
		FROM users
		WHERE email_blind_idx IS NULL
	`)
	if err != nil {
		return fmt.Errorf("query unmigrated users: %w", err)
	}
	defer rows.Close()

	var ids, emails, clerkIDs []string
	for rows.Next() {
		var id, email, clerkID string
		if err := rows.Scan(&id, &email, &clerkID); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		ids = append(ids, id)
		emails = append(emails, email)
		clerkIDs = append(clerkIDs, clerkID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	slog.WarnContext(ctx, "#1001: found unmigrated users without blind indexes, backfilling at startup",
		"count", len(ids))

	for i, id := range ids {
		email := emails[i]
		clerkID := clerkIDs[i]

		encEmail, emailBlindIdx, err := encryptWithBlind(enc, email, true)
		if err != nil {
			return fmt.Errorf("encrypt email for user %s: %w", id, err)
		}
		encClerkID, clerkBlindIdx, err := encryptWithBlind(enc, clerkID, false)
		if err != nil {
			return fmt.Errorf("encrypt clerk_user_id for user %s: %w", id, err)
		}

		_, err = db.Exec(ctx, `
			UPDATE users
			SET email = $1, email_blind_idx = $2,
			    clerk_user_id = $3, clerk_user_id_blind_idx = $4
			WHERE id = $5
		`, encEmail, emailBlindIdx, encClerkID, clerkBlindIdx, id)
		if err != nil {
			return fmt.Errorf("update user %s: %w", id, err)
		}
	}

	slog.InfoContext(ctx, "#1001: user backfill complete", "migrated", len(ids))
	return nil
}

func backfillUserIdentities(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	rows, err := db.Query(ctx, `
		SELECT id, clerk_user_id
		FROM user_identities
		WHERE clerk_user_id_blind_idx IS NULL
	`)
	if err != nil {
		return fmt.Errorf("query unmigrated identities: %w", err)
	}
	defer rows.Close()

	var ids, clerkIDs []string
	for rows.Next() {
		var id, clerkID string
		if err := rows.Scan(&id, &clerkID); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		ids = append(ids, id)
		clerkIDs = append(clerkIDs, clerkID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	slog.WarnContext(ctx, "#1001: found unmigrated user_identities without blind indexes, backfilling at startup",
		"count", len(ids))

	for i, id := range ids {
		clerkID := clerkIDs[i]

		encClerkID, clerkBlindIdx, err := encryptWithBlind(enc, clerkID, false)
		if err != nil {
			return fmt.Errorf("encrypt clerk_user_id for identity %s: %w", id, err)
		}

		_, err = db.Exec(ctx, `
			UPDATE user_identities
			SET clerk_user_id = $1, clerk_user_id_blind_idx = $2
			WHERE id = $3
		`, encClerkID, clerkBlindIdx, id)
		if err != nil {
			return fmt.Errorf("update identity %s: %w", id, err)
		}
	}

	slog.InfoContext(ctx, "#1001: user_identities backfill complete", "migrated", len(ids))
	return nil
}

// normalizeForBlindIndex returns the value to use for blind index computation.
// If lower is true, returns lowercased and trimmed value; otherwise returns as-is.
func normalizeForBlindIndex(value string, lower bool) string {
	if lower {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return value
}

// encryptWithBlind encrypts a plaintext value and returns (encrypted, blindIndex, error).
// Skips already-encrypted values (those starting with ENC$).
func encryptWithBlind(enc *appcrypto.FieldEncryptor, plaintext string, lower bool) (string, string, error) {
	if appcrypto.IsEncrypted(plaintext) {
		// Already encrypted — just compute blind index via decrypt
		decrypted, err := enc.DecryptString(plaintext)
		if err != nil {
			return "", "", err
		}
		return plaintext, enc.BlindIndex(normalizeForBlindIndex(decrypted, lower)), nil
	}

	encrypted, err := enc.EncryptString(plaintext)
	if err != nil {
		return "", "", err
	}
	return encrypted, enc.BlindIndex(normalizeForBlindIndex(plaintext, lower)), nil
}

// backfillResult tracks the outcome of processing a single user during backfill.
type backfillResult int

const (
	resultProcessed     backfillResult = iota // prefix rows were written
	resultSkippedLocked                       // row was locked by concurrent UpdateUserEmailTx — skip
	resultSkippedDecrypt                      // email could not be decrypted — skip, log, self-heal on next email change
)

// BackfillEmailPrefixIndices populates user_email_prefix_indices for all existing users
// that do not yet have any prefix rows. Called at startup after MigrateUp and BackfillEncryption.
//
// Idempotency guarantees:
//   - Re-run after crash: NOT EXISTS filter skips already-processed users (fast no-op)
//   - Race with UpdateUserEmailTx: FOR UPDATE SKIP LOCKED skips locked rows; the update itself
//     writes correct rows atomically, so no stale rows accumulate
//   - Decrypt error: log + skip; user gains prefix rows on their next email change
func BackfillEmailPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	if db == nil || enc == nil {
		return nil // encryption not configured (local dev without ENCRYPTION_KEY)
	}

	// Load IDs for users that have no prefix rows yet.
	// We do NOT load the email here — we re-read it inside a per-user transaction
	// with a FOR UPDATE SKIP LOCKED lock to prevent races with UpdateUserEmailTx.
	rows, err := db.Query(ctx, `
		SELECT u.id
		FROM users u
		WHERE u.is_deleted = FALSE
		  AND NOT EXISTS (
		      SELECT 1 FROM user_email_prefix_indices p
		      WHERE p.user_id = u.id
		  )
	`)
	if err != nil {
		return fmt.Errorf("query users without prefix indices: %w", err)
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan user id: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	if len(userIDs) == 0 {
		return nil // nothing to backfill — fast path on restarts after first successful run
	}

	slog.InfoContext(ctx, "#1178: backfilling email prefix blind indices", "count", len(userIDs))

	skippedLocked, skippedDecrypt := 0, 0
	for _, userID := range userIDs {
		result, err := backfillUserPrefixIndices(ctx, db, enc, userID)
		if err != nil {
			return fmt.Errorf("backfill user %s: %w", userID, err)
		}
		switch result {
		case resultSkippedLocked:
			skippedLocked++
		case resultSkippedDecrypt:
			skippedDecrypt++
		}
	}

	if skippedDecrypt > 0 {
		slog.WarnContext(ctx, "#1178: backfill complete with decrypt errors (affected users gain prefix rows on next email change)",
			"processed", len(userIDs)-skippedLocked-skippedDecrypt,
			"skipped_locked", skippedLocked, "decrypt_errors", skippedDecrypt)
	} else {
		slog.InfoContext(ctx, "#1178: email prefix index backfill complete",
			"processed", len(userIDs)-skippedLocked, "skipped_locked", skippedLocked)
	}
	return nil
}

// backfillUserPrefixIndices processes one user inside its own transaction.
// Uses FOR UPDATE SKIP LOCKED so concurrent UpdateUserEmailTx (which also writes
// prefix rows atomically) is never raced: if that transaction holds the lock, we skip
// and it will have already written correct rows.
//
// Multi-instance note: if instance B skips a user locked by instance A, and instance A
// crashes before committing, that user will lack prefix rows until the next server restart.
// The backfill runs again at the next startup (BackfillEmailPrefixIndices is called every time),
// so the NOT EXISTS filter picks up the skipped user on the next run. This is the accepted
// trade-off from the design review (Codex Round 4 APPROVE).
func backfillUserPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor, userID string) (backfillResult, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return resultProcessed, err
	}
	// Rollback is a no-op after a successful Commit; the error is intentionally discarded.
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the user row — SKIP LOCKED means we skip rather than wait if UpdateUserEmailTx holds it.
	var encEmail string
	err = tx.QueryRow(ctx,
		"SELECT email FROM users WHERE id = $1 FOR UPDATE SKIP LOCKED", userID,
	).Scan(&encEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		// Row locked by a concurrent UpdateUserEmailTx — that update writes correct rows itself.
		// Or: locked by a concurrent backfill instance — they will commit or roll back; the next
		// startup run picks up any uncommitted users via NOT EXISTS.
		return resultSkippedLocked, nil
	}
	if err != nil {
		return resultProcessed, fmt.Errorf("lock user row: %w", err)
	}

	// Decrypt — handles both ENC$ ciphertext and legacy plaintext.
	var plainEmail string
	if appcrypto.IsEncrypted(encEmail) {
		plainEmail, err = enc.DecryptString(encEmail)
		if err != nil {
			// Corrupt or unreadable email — non-fatal. Prefix rows will be written on next email change.
			slog.WarnContext(ctx, "#1178: skipping user — cannot decrypt email",
				"user_id", userID, "error", err)
			return resultSkippedDecrypt, nil
		}
	} else {
		plainEmail = encEmail // legacy pre-encryption plaintext row
	}

	indices := enc.EmailPrefixBlindIndices(plainEmail)
	if len(indices) > 0 {
		if err = insertPrefixIndicesTx(ctx, tx, userID, indices); err != nil {
			return resultProcessed, fmt.Errorf("insert prefix rows: %w", err)
		}
	}

	return resultProcessed, tx.Commit(ctx)
}

// BackfillUserAccountIDs repairs accounts.user_account_id for rows whose in-memory value
// was silently dropped by the pre-#1306-P1 CreateBatch INSERT bug. Migration 000037 was a
// deliberate NO-OP ("no production data yet"), delegating population to the runtime path
// that the bug then broke. This startup safety-net provides the deferred repair.
//
// Strategy: join accounts to their linked_items, decrypt mask + institution_id, compute
// HMAC blind indexes, look up the matching user_accounts row, then UPDATE. After repairing
// accounts, cascade to transactions, investment_transactions, and holdings via deterministic
// account_id-join SQL (no crypto required for the cascade).
//
// Idempotent: both the scan (WHERE user_account_id IS NULL) and the per-row UPDATE guard
// ensure safe re-runs. No-match and decrypt failures are WARN-logged and skipped;
// they self-heal on the user's next re-link.
func BackfillUserAccountIDs(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	if db == nil || enc == nil {
		return nil // encryption not configured (local dev without ENCRYPTION_KEY)
	}

	if err := backfillAccountsUserAccountIDs(ctx, db, enc); err != nil {
		return fmt.Errorf("accounts user_account_id backfill: %w", err)
	}
	if err := backfillDownstreamUserAccountIDs(ctx, db); err != nil {
		return fmt.Errorf("downstream user_account_id cascade: %w", err)
	}
	return nil
}

func backfillAccountsUserAccountIDs(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	return backfillAccountsUserAccountIDsWithDB(ctx, db, enc)
}

// backfillAccountsUserAccountIDsWithDB is the testable implementation of
// backfillAccountsUserAccountIDs. It accepts a backfillQuerier so that unit
// tests can inject a pgxmock without needing a live database.
// backfillAccountsPageSize and backfillAccountsMaxRowsPerRun bound the accounts leg of the startup
// backfill (#1569 round 14). They are `var`, not `const`, ONLY so the paging tests can express "a
// full page" in two rows; nothing in production reassigns them.
//
// WHY A BOUND AT ALL. This function is called synchronously from cmd/server/main.go before the
// process accepts traffic, and main.go calls os.Exit(1) if it returns an error. Its scan was
// unbounded: it materialised every `a.user_account_id IS NULL` row and then did a decrypt + lookup
// (+ sometimes an INSERT) + UPDATE per row, serially. Steady state is a cheap indexed no-op — zero
// rows — but this backfill exists precisely because the write-path bug it repairs (#1306) has
// recurred, so a large backlog is a state the system is known to be able to reach. At that point
// every rolling restart reprocessed the whole backlog before serving, and failed CLOSED.
//
// THE SHAPE IS COPIED FROM THE RETENTION SWEEP (queue/jobs/retention_cleanup_check.go:
// expiredRetentionPageSize=500, maxUsersPerRetentionSweep=1000), which solved the same problem —
// a page size bounding one query's materialisation, and a separate per-run total bounding how much
// work one invocation does. Same numbers, for the same reason: a page's cost is a row fetch either
// way, and 1,000 per boot is far above any plausible steady-state backlog for a product whose
// steady state here is zero rows.
//
// ⚠ WHAT THIS DOES **NOT** BOUND: THE DATABASE'S WORK AT BOOT (#1569 round 16 task 9, D-2). The cap
// is a GO-SIDE bound — it limits how many rows this process fetches, decrypts and updates — and it
// must not be quoted as "boot does at most 1000 rows of work". A RESUMED run's first page carries
// `OFFSET k` (fetchBackfillAccountPage), and PostgreSQL genuinely discards k rows before returning
// that page.
//
// ⚠ THE SHAPE IS STATED HERE, NOT A TABLE OF BUFFER COUNTS, AND THAT IS DELIBERATE (#1569 round 17
// task 10, B-2). A table of EXPLAIN (ANALYZE, BUFFERS) figures used to stand here and in migration
// 000182. Re-measuring it on postgres:17.11 against a rebuilt worst-case dataset (the real schema,
// 300k accounts of which 100k are candidates ALL SHARING ONE created_at — the worst shape
// ts1_offset exists for — random-uuid ids, this query verbatim, one page of 500) reproduced the
// SHAPE and only the shape: the small-offset per-page counts matched to within a buffer, while the
// no-index figure and the offset at which the plan flips did not reproduce at all. They cannot,
// because a buffer count is a function of row width, heap correlation, shared_buffers, work_mem and
// max_parallel_workers_per_gather. What holds:
//
//	offset 0 or an in-run keyset page   Index Scan using idx_accounts_user_account_backfill,
//	                                    cost bounded by the PAGE.
//	offset k                            same plan, cost LINEAR IN k — Postgres genuinely walks and
//	                                    discards k rows first.
//	k large enough                      the planner ABANDONS the ordered partial index for a bitmap
//	                                    scan of the whole candidate set plus an external merge sort
//	                                    — the sort migration 000182's index exists to prevent. The
//	                                    crossover is a planner decision, not a constant.
//
// So the cost is linear in k, and past a planner crossover it stops using the ordered partial index
// at all. That is the honest statement: tens of milliseconds of synchronous boot at a 100k
// single-timestamp group, against a run that then does 1000 serial decrypt+lookup+UPDATE rows.
// See migration 000182 for why a row count is not a buffer count.
//
// ⚠ THIS WAS WEIGHED AND ACCEPTED, not overlooked. The two alternatives are both worse:
//   - Remove the OFFSET with a keyset predicate. That needs a stored ROW IDENTITY, which
//     sweep_cursors forbids by design (migration 000140: the table must outlive users erased by the
//     sweep that advanced it) and which is the exact shape that produced the all-zero-uuid hole
//     twice. Rejected on the ADR, not on cost.
//   - Cap k. If a tie group is larger than the cap and its head is permanently unrepairable, every
//     boot re-reads the same capped prefix and NEVER reaches past it — the permanent skip this
//     whole cursor exists to remove, reintroduced at one timestamp. Strictly worse than the cost.
//
// WHAT MAKES IT ANALYSABLE: k is a count of rows sharing ONE created_at, never a count of rows
// scanned — advanceBackfillTail resets it at every timestamp boundary. k is therefore bounded by
// one tie group's size.
//
// ⚠ TWO CLAIMS THAT USED TO STAND HERE WERE FALSE AND ARE CORRECTED (#1569 round 17 task 10, B-3).
//
//  1. It said a tie group NECESSARILY comes from one transaction, "created_at defaults to now(),
//     which is the transaction timestamp", i.e. one Plaid item's accounts. Neither half holds. The
//     production write path does not use the column default at all: AccountRepository.CreateBatch
//     takes ONE Go-side time.Now() and binds it as created_at for every row in the batch. So equal
//     timestamps are evidence of one BATCH, not one transaction — and not even that, since two
//     CreateBatch calls whose time.Now() falls in the same microsecond (timestamptz's resolution)
//     tie, and any writer that supplies created_at explicitly can tie arbitrarily. What survives is
//     the weaker, still-useful statement: the DOMINANT source of a tie group is a single
//     CreateBatch, i.e. one Plaid item's accounts, because that call stamps the whole batch alike.
//  2. It said the WARN on the capped path "prints resume_offset every time k grows". It does not:
//     it is logged ONCE per capped run, after the loop, and only when the run hit its cap. What
//     that actually buys is that every run which STORES a nonzero offset also logs the offset it
//     stored, so growth is observable across consecutive capped boots — not at each increment, and
//     not at all on a run that finishes its scan.
//
// TestBackfillAccounts_StoredOffsetResetsAtATimestampBoundary pins the reset;
// TestBackfillAccounts_StoredOffsetAccumulatesAcrossRuns pins the accumulation.
//
// ⚠ WHAT IT STILL DOES NOT GUARANTEE: nothing bounds a tie group's size a priori. A single
// transaction inserting a million accounts would make k a million and the resumed boot would sort
// the candidate set on disk. This is accepted as implausible for the write path that produces these
// rows, not as impossible.
//
// ⚠ AND "IMPLAUSIBLE FOR THE WRITE PATH" IS NOT A PROPERTY OF THIS CODE — #1569 round 17 task 8
// corrects the note that read as though it were. The write path is AccountRepository.CreateBatch
// (repository/account_repository.go): it opens ONE transaction, binds ONE `now := time.Now()`, and
// stamps it as created_at on every element of an `accounts` slice whose length it never checks. So
// a tie group is exactly one CreateBatch call's slice, and its size is whatever Plaid returned for
// that Item — bounded by the provider, by nothing here. The judgement that a >1000-account
// institution does not occur is EMPIRICAL, about the outside world; it is not enforced, not
// asserted, and must not be quoted as "the write path cannot produce one".
//
// What keeps that from being a data-loss risk is a different mechanism, and it is the one to rely
// on: this scan resumes INSIDE a tie group via ts1_offset (migration 000183), so an over-cap group
// is walked through across boots rather than sacrificed. The cost of a large group is a sort, not a
// skipped row.
//
// ⚠ WHAT THIS DOES **NOT** CHANGE: failure semantics. An error still propagates and main.go still
// exits — bounding the work per boot and degrading instead of exiting are two different decisions,
// and only the first was taken.
//
// ⚠ WHAT THIS DOES **NOT** GUARANTEE: freedom from starvation, and it must not be described as
// though it does (the retention sweep's own comment makes the same disclaimer about its cursor).
// Within a run the cursor advances past rows that were SKIPPED (no institution_id, undecryptable),
// so a run always terminates. Across runs, rows that are repairable leave the predicate — the scan
// is self-consuming — but rows that are permanently unrepairable do NOT.
//
// ⚠ #1569 ROUND 14 TASK 9 CLOSED THE CONSEQUENCE OF THAT, and this paragraph replaces the
// prediction that used to stand here. Through task 8 the cursor was per-RUN: every boot restarted
// at the beginning, so more than backfillAccountsMaxRowsPerRun permanently-unrepairable rows
// sorting ahead of a repairable one consumed every future boot's budget and the repairable row was
// reached NEVER — a liveness regression against the unbounded original, which reached row 1001.
// The cursor is now PERSISTED across boots in sweep_cursors, following #1477's mechanism rather
// than a new one, and it WRAPS to the beginning when the scan is exhausted so a row arriving behind
// the watermark is not starved at the other end. backfill_accounts_cursor.go states that design,
// what a race between two simultaneous boots does to the shared row, and the one residual it keeps
// (an over-cap tie group on a single created_at). The WARN at the cap remains: it is what makes a
// persistent backlog visible, and it now names where the next boot will resume.
var (
	backfillAccountsPageSize      = 500
	backfillAccountsMaxRowsPerRun = 1000
)

// backfillAccountRow is one page row of the accounts scan.
type backfillAccountRow struct {
	id          string
	userID      string
	mask        *string
	instID      *string // nullable on linked_items
	acctType    *string // #1569: carried so a synthesised identity is not typeless
	acctSubtype *string
	// isHidden and status carry the account's LEGACY hide and liveness up to the chain row
	// (#1609). Migrations run before the server boots (backend/scripts/docker-entrypoint.sh), so
	// an account whose user_account_id was still NULL when 000185 ran MISSED that migration's
	// is_hidden lift entirely; the chain row this repair gives it would otherwise be born visible
	// (user_accounts.is_hidden is NOT NULL DEFAULT FALSE) and the chain-authoritative read overlay
	// would silently un-hide an account the user hid. Fixing it HERE rather than by re-ordering
	// the migration is what makes it durable: the repair runs on every boot, the migration once.
	isHidden bool
	status   string
	// createdAt is the scan's leading sort key (#1569 task 9) and the only part of the position
	// that is ever persisted — see backfill_accounts_cursor.go on why the stored position may not
	// be a row identity.
	createdAt time.Time
}

// fetchBackfillAccountPage reads one page of accounts still missing user_account_id, at the
// position `cursor`, ordered by (created_at, id) so the caller can advance deterministically.
//
// THE THREE STATES THE PREDICATE HAS TO EXPRESS, and why it is spelled with two clauses rather than
// one row-comparison (#1569 round 15 task 9 fix 1):
//
//	cold start        cursor == nil            no position predicate at all, offset 0
//	resume position   cursor.id == ""          created_at >= T, skipping the first `offset` rows
//	in-run position   cursor.id != ""          the exact keyset (created_at, id) > (T, id)
//
// ⚠ THE COLD START CARRIES NO PREDICATE, AND THE RESUME CARRIES NO ID. Both are the same defect
// avoided twice. The original spelling started at the all-zero uuid and filtered `a.id > $1`, so an
// account whose id IS the all-zero uuid — which PostgreSQL permits as an explicitly inserted PK, and
// which AccountRepository.CreateBatch preserves — could never satisfy `>` and was invisible on every
// run. Round 14 fixed that for the cold start; the resume then reintroduced it by pairing the stored
// timestamp with the same all-zero uuid as an "inclusive" sentinel, which excluded exactly that one
// id at the boundary while including every other id at the same timestamp. Naming no id on the
// resume path is what makes the asymmetry unrepresentable rather than merely fixed again.
//
// `$1::timestamptz IS NULL OR …` is evaluated per row rather than in Go so the query text stays
// single-form; the planner folds the constant-null test away. The `a.created_at >= $1` clause is
// also what gives the partial index idx_accounts_user_account_backfill (migration 000182) its range
// bound; the id clause only trims the equal-timestamp head.
func fetchBackfillAccountPage(ctx context.Context, db backfillQuerier, cursor *backfillAccountsCursor, limit int) ([]backfillAccountRow, error) {
	var (
		cursorTS *time.Time
		cursorID *string
		offset   int
	)
	if cursor != nil {
		ts := cursor.createdAt
		cursorTS = &ts
		if cursor.id != "" {
			id := cursor.id
			cursorID = &id
		} else {
			offset = cursor.offset
		}
	}
	rows, err := db.Query(ctx, `
		SELECT a.id, a.user_id, a.mask, li.institution_id, a.type, a.subtype,
		       a.is_hidden, a.status, a.created_at
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE a.user_account_id IS NULL
		  AND ($1::timestamptz IS NULL OR a.created_at >= $1::timestamptz)
		  AND ($2::uuid IS NULL OR (a.created_at, a.id) > ($1::timestamptz, $2::uuid))
		ORDER BY a.created_at, a.id
		OFFSET $3
		LIMIT $4
	`, cursorTS, cursorID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("query accounts with null user_account_id: %w", err)
	}
	defer rows.Close()

	var page []backfillAccountRow
	for rows.Next() {
		var r backfillAccountRow
		if err := rows.Scan(&r.id, &r.userID, &r.mask, &r.instID, &r.acctType, &r.acctSubtype,
			&r.isHidden, &r.status, &r.createdAt); err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}
		page = append(page, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account rows: %w", err)
	}
	return page, nil
}

func backfillAccountsUserAccountIDsWithDB(ctx context.Context, db backfillQuerier, enc *appcrypto.FieldEncryptor) error {
	// start is where this run RESUMED from (nil = the beginning); cursor is the position the next
	// page query uses, which after the first page is the exact (created_at, id) of the last row
	// processed and is therefore always strictly forward, so the loop terminates regardless of what
	// was stored. tail is the RESUME position this run would persist if it stops at its cap — see
	// advanceBackfillTail.
	start := loadBackfillAccountsCursor(ctx, db)
	cursor := start
	var tail *backfillAccountsCursor
	if start != nil {
		t := *start
		tail = &t
	}
	wrapped := false
	processed := 0
	repaired, skippedNoMatch, skippedDecrypt, synthesised := 0, 0, 0, 0
	cappedEarly := false

	for {
		accs, err := fetchBackfillAccountPage(ctx, db, cursor, backfillAccountsPageSize)
		if err != nil {
			return err
		}
		if len(accs) == 0 {
			// A stored watermark that returns nothing is a STALE watermark, not "no work to do":
			// the rows it pointed past have since been repaired or deleted. Clear it and re-ask
			// from the beginning WITHIN THIS RUN — the wrap.
			//
			// ⚠ WHAT THIS BRANCH IS AND IS NOT WORTH, stated precisely because it is easy to
			// over-credit. It is NOT what prevents a forward-only cursor from sitting past the end
			// forever — the clear on the exhausted path below already does that, and without this
			// branch the same clear would run one line later. What it buys is ONE BOOT of latency:
			// rows sorting behind the stale watermark are repaired on THIS boot instead of the
			// next, which matters because this scan's clock is restarts, not a periodic schedule.
			//
			// ⚠ THE GATE IS `start`, NOT `cursor`, AND `processed == 0` IS HALF OF IT (#1569 round
			// 15, defect C1). `cursor` is reassigned after EVERY non-empty page, so gating on it
			// wrapped a run that began at the BEGINNING and merely happened to end on an exact
			// page boundary — and that wrap is not free: it nils `start`, replays the rows just
			// processed, reaches the per-run cap on the replay and therefore STORES a watermark for
			// a scan it had already exhausted, which every later boot then resumes, replays, wraps
			// and re-stores forever. The condition below is deliberately identical to
			// jobs.sweepResume.WrapNow's (`r.start == nil || r.wrapped || processed > 0`): wrap
			// only when this run RESUMED FROM A STORED POSITION and has processed ZERO rows.
			// Sharing the primitive itself is not possible — sweepResume lives in
			// internal/queue/jobs, which imports internal/repository, whose own integration tests
			// import internal/database (see backfill_accounts_cursor.go on that cycle) — so the
			// condition is mirrored here and pinned by
			// TestBackfillAccountsCursor_ExhaustionOnAPageBoundaryNeitherWrapsNorPingPongs.
			// Bounded to once per run by `wrapped`, so it cannot loop.
			if start != nil && !wrapped && processed == 0 {
				wrapped = true
				slog.InfoContext(ctx, "#1306 P2: accounts backfill resume position is past the end of the scan; wrapping to the start")
				clearBackfillAccountsCursor(ctx, db)
				// tail goes with start: after the wrap this run is scanning from the beginning, so
				// the abandoned watermark's offset describes rows this run has not consumed.
				start, cursor, tail = nil, nil, nil
				continue
			}
			break // the scan is genuinely exhausted
		}

		slog.WarnContext(ctx, "#1306 P2: found accounts with null user_account_id, backfilling at startup",
			"page_count", len(accs), "processed_so_far", processed)

		if err := repairBackfillAccountPage(ctx, db, enc, accs,
			&repaired, &synthesised, &skippedNoMatch, &skippedDecrypt); err != nil {
			return err
		}

		last := accs[len(accs)-1]
		cursor = &backfillAccountsCursor{createdAt: last.createdAt, id: last.id}
		tail = advanceBackfillTail(tail, accs)
		processed += len(accs)

		if processed >= backfillAccountsMaxRowsPerRun {
			cappedEarly = true
			break
		}
		if len(accs) < backfillAccountsPageSize {
			break // short page: the scan is exhausted
		}
	}

	// Persist ONLY on the capped path; a run that exhausted its scan clears instead. That asymmetry
	// is what makes "we stopped mid-scan at T, k rows into it" the only state ever stored, so a
	// past-the-end watermark is transient (the wrap above) rather than a permanent silent stall.
	if cappedEarly {
		saveBackfillAccountsCursor(ctx, db, *tail)
		slog.WarnContext(ctx, "#1306 P2: accounts backfill stopped at its per-run cap; the next boot resumes from where this run stopped",
			"processed", processed, "cap", backfillAccountsMaxRowsPerRun,
			"resume_at", tail.createdAt, "resume_offset", tail.offset)
	} else if start != nil || processed > 0 {
		// Nothing stored and nothing processed means the steady state (zero rows, no cursor row):
		// skip the DELETE so a healthy boot costs two queries, not three.
		clearBackfillAccountsCursor(ctx, db)
	}

	if processed == 0 {
		return nil
	}

	slog.InfoContext(ctx, "#1306 P2: accounts backfill complete",
		"repaired", repaired, "synthesised", synthesised,
		"skipped_no_match", skippedNoMatch, "skipped_decrypt", skippedDecrypt)
	return nil
}

// repairBackfillAccountPage runs the per-row repair for one page. The counters are pointers so
// they accumulate across pages and the single completion log stays truthful about the whole run.
func repairBackfillAccountPage(
	ctx context.Context,
	db backfillQuerier,
	enc *appcrypto.FieldEncryptor,
	accs []backfillAccountRow,
	repaired, synthesised, skippedNoMatch, skippedDecrypt *int,
) error {
	for _, r := range accs {
		if r.instID == nil {
			// linked_item has no institution_id — cannot compute blind index.
			slog.WarnContext(ctx, "#1306 P2: skipping account — linked_item has no institution_id",
				"account_id", r.id)
			*skippedNoMatch++
			continue
		}

		// Decrypt institution_id; handles both ENC$ ciphertext and legacy plaintext.
		plainInstID, err := enc.DecryptString(*r.instID)
		if err != nil {
			slog.WarnContext(ctx, "#1306 P2: skipping account — cannot decrypt institution_id",
				"account_id", r.id, "error", err)
			*skippedDecrypt++
			continue
		}

		// Decrypt mask (nullable).
		var plainMask *string
		if r.mask != nil {
			m, err := enc.DecryptString(*r.mask)
			if err != nil {
				slog.WarnContext(ctx, "#1306 P2: skipping account — cannot decrypt mask",
					"account_id", r.id, "error", err)
				*skippedDecrypt++
				continue
			}
			plainMask = &m // handles both ENC$ ciphertext and legacy plaintext (handled by DecryptString)
		}

		// Compute blind indexes and look up the user_account row.
		// #1334: Use Query + LIMIT 2 (instead of QueryRow) so we can detect and WARN
		// when multiple user_accounts rows share the same blind-index triple (possible
		// before migration 000100 enforces uniqueness). ORDER BY created_at ASC, id ASC
		// picks the deterministic earliest-created survivor.
		instBlind := enc.BlindIndex(plainInstID)
		maskBlind := enc.BlindIndexNullable(plainMask)

		uaID, found, err := lookupUserAccountID(ctx, db, r.id, r.userID, instBlind, maskBlind, r.acctType, r.acctSubtype)
		if err != nil {
			return err
		}
		if !found {
			// Issue #1569: previously this incremented skippedNoMatch and moved on, leaving the
			// account with a NULL user_account_id "until the next re-link". That was survivable
			// while identity-less accounts were eventually cascade-deleted. Under
			// history-retained-by-default they are retained forever, so the NULL becomes a
			// permanent re-link double-count seed (dashboard_service.go:35-73) and blocks the
			// accounts.user_account_id NOT NULL constraint the retention model depends on.
			//
			// Mint the identity instead. This has to happen here rather than in a SQL migration:
			// user_accounts.institution_id/mask are encrypted with blind-index uniqueness
			// (000053, 000100) and only the application holds the HMAC key.
			uaID, err = synthesiseUserAccountID(ctx, db, enc, r.userID, plainInstID, plainMask,
				instBlind, maskBlind, r.acctType, r.acctSubtype)
			if err != nil {
				return fmt.Errorf("synthesise user_account for account %s: %w", r.id, err)
			}
			*synthesised++
			slog.WarnContext(ctx, "#1569: minted a stable identity for an account that had none",
				"account_id", r.id, "user_account_id", uaID)
		}

		if _, err := db.Exec(ctx,
			"UPDATE accounts SET user_account_id = $1 WHERE id = $2 AND user_account_id IS NULL",
			uaID, r.id,
		); err != nil {
			return fmt.Errorf("update account %s: %w", r.id, err)
		}

		if err := carryLegacyHideForward(ctx, db, r, uaID); err != nil {
			return err
		}
		*repaired++
	}
	return nil
}

// carryLegacyHideForward lifts an account's LEGACY accounts.is_hidden onto the chain row the
// repair just attached it to — the SAME chain-OR rule migration 000185 applies, applied on the
// path that migration structurally cannot reach.
//
// WHY IT HAS TO LIVE HERE (#1609, the ordering hazard):
//
//	container migrations run BEFORE the server boots (backend/scripts/docker-entrypoint.sh), and
//	the server THEN calls BackfillUserAccountIDs (cmd/server/main.go). An account whose
//	user_account_id was still NULL when 000185 ran was invisible to that migration's
//	`EXISTS (SELECT 1 FROM accounts WHERE user_account_id = ua.id ...)` lift — it belonged to no
//	chain yet. Moments later this repair gives it one, and user_accounts.is_hidden is
//	NOT NULL DEFAULT FALSE (000031), so the chain is born VISIBLE. Under the chain-authoritative
//	read overlay that FALSE wins, and an account the user deliberately hid quietly reappears.
//
// Re-ordering the migration would not fix it: the NULL user_account_id can be created at any time
// (a fresh link between two boots), whereas this repair runs on EVERY boot.
//
// Both outcomes of the repair need it, not just synthesis. A hidden account that MATCHES an
// existing visible chain reopens exactly the same hole, which is why this runs after uaID is
// resolved rather than inside synthesiseUserAccountID.
//
// The hidden_source it stamps is 000185's classification applied to the account joining the chain:
// only a human could have hidden a LIVE account ('user'); anything else is genuinely ambiguous
// ('unknown'), and 'unknown' is never auto-un-hidden. It is deliberately NOT 'system' — labelling
// an ambiguous hide 'system' is what would let the next re-link destroy a real user hide.
//
// COALESCE on hidden_source (and the `hidden_source IS NULL` leg of the WHERE) means a cause the
// application already owns is never overwritten by a re-derivation from state.
func carryLegacyHideForward(ctx context.Context, db backfillQuerier, r backfillAccountRow, uaID string) error {
	if !r.isHidden {
		// Nothing to carry. Notably this does NOT un-hide anything: a visible account joining a
		// hidden chain says nothing about why the chain was hidden.
		return nil
	}
	source := "unknown"
	if r.status == "active" {
		source = "user"
	}
	if _, err := db.Exec(ctx, `
		UPDATE user_accounts
		SET is_hidden     = TRUE,
		    hidden_source = COALESCE(hidden_source, $1),
		    updated_at    = now()
		WHERE id = $2
		  AND (is_hidden = FALSE OR hidden_source IS NULL)
	`, source, uaID); err != nil {
		return fmt.Errorf("carry legacy hide forward for account %s: %w", r.id, err)
	}
	slog.WarnContext(ctx, "#1609: carried a legacy account hide onto its chain row",
		"account_id", r.id, "user_account_id", uaID, "hidden_source", source)
	return nil
}

// synthesiseUserAccountID mints a user_accounts row for an account that has no matching identity,
// and returns its id. Issue #1569.
//
// Two properties matter and are pinned by integration tests:
//
//   - It is IDEMPOTENT and chain-convergent. The INSERT carries ON CONFLICT DO NOTHING against
//     unique_user_accounts_inst_mask_identity_blind (user_id, institution_id_blind_idx,
//     mask_blind_idx, identity_type, identity_subtype, NULLS NOT DISTINCT — #1569 round 11 task 4
//     replaced the three-column unique_user_accounts_inst_mask_blind with this five-column index)
//     and re-reads on conflict. So a second backfill run mints nothing, and two members of the
//     SAME real account — sharing institution, mask, AND identity — converge on one identity
//     instead of each getting its own. Minting per account would manufacture exactly the split
//     chain this work exists to eliminate. Two accounts sharing only institution+mask but
//     DISAGREEING on identity now correctly mint DIFFERENT rows — that is the point of the
//     narrowing, not a regression of this convergence guarantee.
//   - The minted row is genuinely MATCHABLE: it stores the same blind indexes lookupUserAccountID
//     computes, so the next real re-link finds it rather than minting a second one.
//
// institution_id and mask are written encrypted, matching how UserAccountService writes them.
func synthesiseUserAccountID(
	ctx context.Context,
	db backfillQuerier,
	enc *appcrypto.FieldEncryptor,
	userID, plainInstID string,
	plainMask *string,
	instBlind string,
	maskBlind *string,
	acctType, acctSubtype *string,
) (string, error) {
	encInstID, err := enc.EncryptString(plainInstID)
	if err != nil {
		return "", fmt.Errorf("encrypt institution_id: %w", err)
	}
	var encMask *string
	if plainMask != nil {
		m, err := enc.EncryptString(*plainMask)
		if err != nil {
			return "", fmt.Errorf("encrypt mask: %w", err)
		}
		encMask = &m
	}

	// identity_type/identity_subtype are the FROZEN identity of this chain (#1569 round 11 task 3),
	// written HERE — at mint — from the SAME founding account's acctType/acctSubtype used for
	// primary_type/primary_subtype below, and never again. Unlike primary_type/primary_subtype,
	// which get overwritten on every re-link, nothing UPDATEs these columns; see
	// domain.UserAccount for the full reasoning and the guards that enforce it.
	newID := uuid.New().String()
	if _, err := db.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx,
			 primary_type, primary_subtype, identity_type, identity_subtype)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7, $8)
		-- unique_user_accounts_inst_mask_identity_blind (#1569 round 11 task 4, replacing 000100's
		-- unique_user_accounts_inst_mask_blind) is a PARTIAL index
		-- (WHERE institution_id_blind_idx IS NOT NULL), so the inference clause must repeat
		-- that predicate or Postgres cannot match it (SQLSTATE 42P10). identity_type/identity_subtype
		-- are now part of the arbiter — see the task 4 migration header for why the match key was
		-- narrowed.
		ON CONFLICT (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
			WHERE institution_id_blind_idx IS NOT NULL
			DO NOTHING
	`, newID, userID, encInstID, encMask, instBlind, maskBlind, acctType, acctSubtype); err != nil {
		return "", fmt.Errorf("insert user_account: %w", err)
	}

	// Re-read rather than trusting newID: on conflict the INSERT was a no-op and the winner is
	// whichever row already held the triple (possibly one this same loop minted a moment ago).
	existingID, found, err := lookupUserAccountID(ctx, db, "synthesised", userID, instBlind, maskBlind, acctType, acctSubtype)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("user_account not found immediately after synthesis for user %s", userID)
	}
	return existingID, nil
}

// lookupUserAccountID fetches up to 2 user_accounts rows matching the given blind-index triple,
// ordered deterministically (created_at ASC, id ASC). Returns the first row's ID when found.
// When >1 row matches, emits a slog WARN (possible pre-migration duplicate) but continues
// with the deterministic survivor — never returns an error in the multi-match case.
// identityType/identitySubtype narrow the match to the #1569 round 11 task 4 key: two accounts
// sharing (user_id, institution, mask) but disagreeing on identity_type/identity_subtype (the
// brokerage/IRA shape) are DIFFERENT chains and must not be matched together. A row with a
// NON-NULL identity_type must match exactly (IS NOT DISTINCT FROM on subtype, so a genuinely
// subtype-less identity still matches). A row with a NULL identity_type is an ORPHAN — see
// 000177's header, a user_accounts row with no physical accounts ever pointing at it, so nothing
// could derive an identity for it — and matches ANY incoming identity, reproducing pre-task-4
// behavior for that narrow legacy shape (mirrors user_account_repository.go's
// FindByInstitutionAndMask; keep both in sync).
func lookupUserAccountID(
	ctx context.Context,
	db backfillQuerier,
	accountID, userID string,
	instBlind string,
	maskBlind *string,
	identityType, identitySubtype *string,
) (uaID string, found bool, err error) {
	var (
		query string
		args  []any
	)
	if maskBlind == nil {
		query = `
			SELECT id FROM user_accounts
			WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx IS NULL
			  AND (identity_type IS NULL
			       OR (identity_type = $3 AND identity_subtype IS NOT DISTINCT FROM $4))
			ORDER BY created_at ASC, id ASC
			LIMIT 2`
		args = []any{userID, instBlind, identityType, identitySubtype}
	} else {
		query = `
			SELECT id FROM user_accounts
			WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
			  AND (identity_type IS NULL
			       OR (identity_type = $4 AND identity_subtype IS NOT DISTINCT FROM $5))
			ORDER BY created_at ASC, id ASC
			LIMIT 2`
		args = []any{userID, instBlind, *maskBlind, identityType, identitySubtype}
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return "", false, fmt.Errorf("error looking up user_account for account %s: %w", accountID, err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		if count == 1 {
			if err := rows.Scan(&uaID); err != nil {
				return "", false, fmt.Errorf("error scanning user_account_id for account %s: %w", accountID, err)
			}
		} else {
			// Discard subsequent rows — we already have the deterministic first.
			var discarded string
			_ = rows.Scan(&discarded)
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, fmt.Errorf("error iterating user_accounts for account %s: %w", accountID, err)
	}

	if count == 0 {
		slog.WarnContext(ctx, "#1306 P2: no matching user_account for blind-index triple (#1569: caller mints one)",
			"account_id", accountID)
		return "", false, nil
	}
	if count > 1 {
		// #1334: multiple rows match the blind-index triple — possible pre-migration state.
		// Pick the deterministic first (ORDER BY created_at ASC, id ASC) and warn.
		slog.WarnContext(ctx, "#1334: multiple user_accounts match account; picking deterministic survivor",
			"account_id", accountID,
			"user_account_id", uaID)
	}

	return uaID, true, nil
}

// backfillDownstreamUserAccountIDs cascades accounts.user_account_id to all downstream
// tables that derive it from accounts via account_id. Pure SQL — no crypto needed.
// Mirrors the one-time backfills in migrations 000071, 000038, 000068, 000069.
func backfillDownstreamUserAccountIDs(ctx context.Context, db *pgxpool.Pool) error {
	type cascade struct {
		query string
		table string
	}
	// ONE-TIME BACKFILL of a denormalised FK. Every step fills user_account_id where it is NULL and
	// nowhere else. A tombstoned row needs it as much as a live one — more so, because nothing will
	// ever run again to fill it, and #1569's whole point is that the tombstoned row stays
	// queryable. The two steps whose table is tombstoned carry the exemption inside their SQL, where
	// the guard reads it (#1569 T8 write-target audit).
	steps := []cascade{
		{
			table: "transactions",
			query: `-- tombstone-exempt(write:transactions t): one-time backfill; filtering deleted_at would
				-- leave exactly the retained history unrepairable (#1569 T8).
				UPDATE transactions t SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE t.account_id = a.id AND t.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "investment_transactions",
			query: `UPDATE investment_transactions it SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE it.account_id = a.id AND it.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "holdings",
			query: `UPDATE holdings h SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE h.account_id = a.id AND h.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "balance_snapshots",
			query: `UPDATE balance_snapshots bs SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE bs.account_id = a.id AND bs.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			// Issue #1569 residual E7. The history rows written BEFORE an account's identity was
			// minted carry NULL, and nothing else can ever fill them: the record_account_status_change
			// trigger fires on status changes only, so assigning accounts.user_account_id later does
			// not rewrite the intervals already recorded. Without this step the column is not merely
			// unread, it is unrepairable — on exactly the pre-mint accounts whose retained history is
			// the point of #1569.
			table: "account_status_history",
			query: `UPDATE account_status_history ash SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE ash.account_id = a.id AND ash.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "savings_goals",
			query: `-- tombstone-exempt(write:savings_goals sg): one-time backfill; same reason as the
				-- transactions step above (#1569 T8).
				UPDATE savings_goals sg SET user_account_id = a.user_account_id
				FROM accounts a
				WHERE sg.account_id = a.id AND sg.user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "transfer_suggestions (from)",
			query: `UPDATE transfer_suggestions ts SET from_user_account_id = a.user_account_id
				FROM accounts a
				WHERE ts.from_account_id = a.id AND ts.from_user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
		{
			table: "transfer_suggestions (to)",
			query: `UPDATE transfer_suggestions ts SET to_user_account_id = a.user_account_id
				FROM accounts a
				WHERE ts.to_account_id = a.id AND ts.to_user_account_id IS NULL AND a.user_account_id IS NOT NULL`,
		},
	}

	for _, s := range steps {
		tag, err := db.Exec(ctx, s.query)
		if err != nil {
			return fmt.Errorf("repair %s: %w", s.table, err)
		}
		if n := tag.RowsAffected(); n > 0 {
			slog.InfoContext(ctx, "#1306 P2: repaired user_account_id", "table", s.table, "count", n)
		}
	}
	return nil
}

// insertPrefixIndicesTx is the package-level helper used by the backfill (database package).
// Profile repository has its own identical private method — same logic, different package scope.
func insertPrefixIndicesTx(ctx context.Context, tx pgx.Tx, userID string, blindIndices []string) error {
	if len(blindIndices) == 0 {
		return nil
	}
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}
