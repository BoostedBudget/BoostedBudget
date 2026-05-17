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
	"fmt"
	"log/slog"
	"strings"

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
