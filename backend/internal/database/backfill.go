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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

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
	defer tx.Rollback(ctx)

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
	rows, err := db.Query(ctx, `
		SELECT a.id, a.user_id, a.mask, li.institution_id
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE a.user_account_id IS NULL
	`)
	if err != nil {
		return fmt.Errorf("query accounts with null user_account_id: %w", err)
	}
	defer rows.Close()

	type accountRow struct {
		id     string
		userID string
		mask   *string
		instID *string // nullable on linked_items
	}
	var accs []accountRow
	for rows.Next() {
		var r accountRow
		if err := rows.Scan(&r.id, &r.userID, &r.mask, &r.instID); err != nil {
			return fmt.Errorf("scan account row: %w", err)
		}
		accs = append(accs, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate account rows: %w", err)
	}
	if len(accs) == 0 {
		return nil // fast path — nothing to repair
	}

	slog.WarnContext(ctx, "#1306 P2: found accounts with null user_account_id, backfilling at startup",
		"count", len(accs))

	repaired, skippedNoMatch, skippedDecrypt := 0, 0, 0
	for _, r := range accs {
		if r.instID == nil {
			// linked_item has no institution_id — cannot compute blind index.
			slog.WarnContext(ctx, "#1306 P2: skipping account — linked_item has no institution_id",
				"account_id", r.id)
			skippedNoMatch++
			continue
		}

		// Decrypt institution_id; handles both ENC$ ciphertext and legacy plaintext.
		plainInstID, err := enc.DecryptString(*r.instID)
		if err != nil {
			slog.WarnContext(ctx, "#1306 P2: skipping account — cannot decrypt institution_id",
				"account_id", r.id, "error", err)
			skippedDecrypt++
			continue
		}

		// Decrypt mask (nullable).
		var plainMask *string
		if r.mask != nil {
			m, err := enc.DecryptString(*r.mask)
			if err != nil {
				slog.WarnContext(ctx, "#1306 P2: skipping account — cannot decrypt mask",
					"account_id", r.id, "error", err)
				skippedDecrypt++
				continue
			}
			plainMask = &m // handles both ENC$ ciphertext and legacy plaintext (handled by DecryptString)
		}

		// Compute blind indexes and look up the user_account row.
		instBlind := enc.BlindIndex(plainInstID)
		maskBlind := enc.BlindIndexNullable(plainMask)

		var uaID string
		var lookupErr error
		if maskBlind == nil {
			lookupErr = db.QueryRow(ctx, `
				SELECT id FROM user_accounts
				WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx IS NULL
			`, r.userID, instBlind).Scan(&uaID)
		} else {
			lookupErr = db.QueryRow(ctx, `
				SELECT id FROM user_accounts
				WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
			`, r.userID, instBlind, *maskBlind).Scan(&uaID)
		}

		if errors.Is(lookupErr, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "#1306 P2: no matching user_account — skipping (self-heals on next re-link)",
				"account_id", r.id)
			skippedNoMatch++
			continue
		}
		if lookupErr != nil {
			return fmt.Errorf("lookup user_account for account %s: %w", r.id, lookupErr)
		}

		if _, err := db.Exec(ctx,
			"UPDATE accounts SET user_account_id = $1 WHERE id = $2 AND user_account_id IS NULL",
			uaID, r.id,
		); err != nil {
			return fmt.Errorf("update account %s: %w", r.id, err)
		}
		repaired++
	}

	slog.InfoContext(ctx, "#1306 P2: accounts backfill complete",
		"repaired", repaired, "skipped_no_match", skippedNoMatch, "skipped_decrypt", skippedDecrypt)
	return nil
}

// backfillDownstreamUserAccountIDs cascades accounts.user_account_id to all downstream
// tables that derive it from accounts via account_id. Pure SQL — no crypto needed.
// Mirrors the one-time backfills in migrations 000071, 000038, 000068, 000069.
func backfillDownstreamUserAccountIDs(ctx context.Context, db *pgxpool.Pool) error {
	type cascade struct {
		query string
		table string
	}
	steps := []cascade{
		{
			table: "transactions",
			query: `UPDATE transactions t SET user_account_id = a.user_account_id
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
			table: "savings_goals",
			query: `UPDATE savings_goals sg SET user_account_id = a.user_account_id
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
