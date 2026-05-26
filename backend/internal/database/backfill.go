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
