//go:build integration

package repository

// Integration test for the notifications_type_check DB constraint.
// Proves that a notification with Type="transfer_suggestion_match" can be
// inserted after migration 000094 adds the value to the CHECK constraint.
// pgxmock cannot catch this because it never sends SQL to a real planner.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestTransferSuggestionMatchNotificationType -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestTransferSuggestionMatchNotificationType_Integration proves that a notification
// with Type="transfer_suggestion_match" (the fuzzy-match prompt type introduced by
// GAP R) can be inserted into the notifications table without a CHECK violation.
// Without migration 000094 this insert fails with SQLSTATE 23514.
func TestTransferSuggestionMatchNotificationType_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed the user required by the notifications user_id FK.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-notif-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed a transfer_suggestion the notification can reference.
	suggID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'rule', 'pending', NOW(), NOW())
	`, suggID, userID)
	require.NoError(t, err, "seed transfer_suggestion")

	// Insert a notification with the fuzzy-match type. This is the insert that
	// fails without migration 000094 (notifications_type_check constraint violation).
	suggIDStr := suggID.String()
	notifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, false, false, 'accept_reject', $6, NOW(), NOW())
	`,
		notifID,
		userID,
		domain.NotificationTypeTransferSuggestionMatch,
		"Did you transfer a different amount?",
		"We suggested $100.00 but detected $95.00 — was this your transfer?",
		suggIDStr,
	)
	require.NoError(t, err,
		"INSERT with type='transfer_suggestion_match' must succeed after migration 000094 "+
			"adds the value to notifications_type_check (SQLSTATE 23514 without the migration)")

	// Verify the row was written.
	var gotType string
	err = tx.QueryRow(ctx,
		`SELECT type FROM notifications WHERE id = $1`, notifID,
	).Scan(&gotType)
	require.NoError(t, err)
	require.Equal(t, domain.NotificationTypeTransferSuggestionMatch, gotType)
}

// TestGetOpenByTransferSuggestionID_TypeScoping_Integration proves that type scoping
// correctly isolates queries by type. This test calls the real repo method (not raw SQL),
// exercising the full scan + decrypt + no-row→nil path.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetOpenByTransferSuggestionID_TypeScoping -tags integration -v
func TestGetOpenByTransferSuggestionID_TypeScoping_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Build the real notification repository backed by the transaction.
	// This exercises the full scan + field-decrypt + no-row→nil path,
	// not just the raw SQL query string.
	repo := NewNotificationRepository(tx, newTestFieldEncryptor(t))

	// Seed user and transfer_suggestion.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-typescope-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	suggID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'rule', 'pending', NOW(), NOW())
	`, suggID, userID)
	require.NoError(t, err, "seed transfer_suggestion")

	// Seed a 'transfer_suggestion' (original prompt) — unread, accept_reject.
	origNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion', 'Accept or reject?', 'Original prompt', false, false, 'accept_reject', $3, NOW(), NOW())
	`, origNotifID, userID, suggID)
	require.NoError(t, err, "seed original transfer_suggestion notification")

	// Seed a 'transfer_suggestion_match' (fuzzy-match prompt) — unread, accept_reject.
	matchNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion_match', 'Confirm amount?', 'Fuzzy-match prompt', false, false, 'accept_reject', $3, NOW() + interval '1 second', NOW())
	`, matchNotifID, userID, suggID)
	require.NoError(t, err, "seed transfer_suggestion_match notification")

	suggIDStr := suggID.String()

	// Querying for 'transfer_suggestion' must return the original prompt (NOT the match prompt).
	// This call goes through the full repo scan + decrypt path — not just a raw SQL string check.
	gotOrig, err := repo.GetOpenByTransferSuggestionID(ctx, suggIDStr, domain.NotificationTypeTransferSuggestion)
	require.NoError(t, err)
	require.NotNil(t, gotOrig, "must find unread transfer_suggestion row")
	require.Equal(t, origNotifID.String(), gotOrig.ID,
		"transfer_suggestion query must return the original prompt, not the match prompt")

	// Querying for 'transfer_suggestion_match' must return ONLY the match prompt.
	gotMatch, err := repo.GetOpenByTransferSuggestionID(ctx, suggIDStr, domain.NotificationTypeTransferSuggestionMatch)
	require.NoError(t, err)
	require.NotNil(t, gotMatch, "must find unread transfer_suggestion_match row")
	require.Equal(t, matchNotifID.String(), gotMatch.ID,
		"transfer_suggestion_match query must return the fuzzy-match prompt, not the original")

	// Mark the original prompt as read; querying for it should now return nil.
	_, err = tx.Exec(ctx, `UPDATE notifications SET read = true WHERE id = $1`, origNotifID)
	require.NoError(t, err, "mark original as read")
	gotOrigAfterRead, err := repo.GetOpenByTransferSuggestionID(ctx, suggIDStr, domain.NotificationTypeTransferSuggestion)
	require.NoError(t, err)
	require.Nil(t, gotOrigAfterRead,
		"once read, transfer_suggestion query must return nil (exercises the no-row→nil path of the real method)")

	// The match prompt must still be returned (unaffected by the original being read).
	gotMatchAfterRead, err := repo.GetOpenByTransferSuggestionID(ctx, suggIDStr, domain.NotificationTypeTransferSuggestionMatch)
	require.NoError(t, err)
	require.NotNil(t, gotMatchAfterRead, "match prompt must still be unread")
}

// TestMigration000094Down_DeletesMatchRowsAndRestoresConstraint_Integration exercises
// the destructive down-migration path for migration 000094. The down migration does three
// things in order:
//  1. DELETE FROM notifications WHERE type = 'transfer_suggestion_match'
//  2. DROP CONSTRAINT IF EXISTS notifications_type_check
//  3. ADD CONSTRAINT notifications_type_check CHECK (type IN (<original 14 values>))
//
// This test verifies:
//   - The DELETE removes existing transfer_suggestion_match rows (not other types).
//   - The restored CHECK constraint rejects a new transfer_suggestion_match INSERT (23514).
//   - The restored CHECK still accepts the original 14 types (e.g. 'transfer_suggestion').
//
// In Postgres, DDL is transactional: DROP/ADD CONSTRAINT are rolled back with the
// surrounding tx.Rollback(), so the real schema is left unchanged after the test.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestMigration000094Down -tags integration -v
func TestMigration000094Down_DeletesMatchRowsAndRestoresConstraint_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed a user and a transfer_suggestion needed by FK constraints.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-down-mig-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	suggID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 75.00, 'rule', 'pending', NOW(), NOW())
	`, suggID, userID)
	require.NoError(t, err, "seed transfer_suggestion")

	// Seed a notification with the new type (the kind that the down migration deletes).
	matchNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion_match', 'Fuzzy prompt', 'Msg', false, false, 'accept_reject', $3, NOW(), NOW())
	`, matchNotifID, userID, suggID)
	require.NoError(t, err, "seed transfer_suggestion_match notification (should exist before down migration)")

	// Also seed one notification with a type that must survive the DELETE.
	origNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion', 'Original prompt', 'Msg', false, false, 'accept_reject', $3, NOW() + interval '1s', NOW())
	`, origNotifID, userID, suggID)
	require.NoError(t, err, "seed transfer_suggestion notification (must not be deleted by down migration)")

	// Simulate the down-migration SQL (000094.down.sql):
	// Step 1: DELETE rows with the new type.
	tag, err := tx.Exec(ctx, `DELETE FROM notifications WHERE type = 'transfer_suggestion_match'`)
	require.NoError(t, err, "down-migration DELETE must succeed")
	require.Equal(t, int64(1), tag.RowsAffected(),
		"exactly one transfer_suggestion_match row must be deleted")

	// Step 2 + 3: Restore the original CHECK constraint (original 14 values only).
	_, err = tx.Exec(ctx, `ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check`)
	require.NoError(t, err, "drop constraint must succeed")
	_, err = tx.Exec(ctx, `
		ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
		CHECK (type IN (
			'item_error', 'login_required', 'pending_expiration', 'permission_revoked',
			'transaction_sync', 'budget_alert', 'payment_failed', 'subscription_past_due',
			'subscription_cancelled', 'subscription_resumed', 'subscription_activated',
			'subscription_trialing', 'transaction_past_due', 'transfer_suggestion'
		))
	`)
	require.NoError(t, err, "restore original CHECK constraint must succeed")

	// Verify the original-type notification still exists (DELETE did not over-delete).
	var origCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE id = $1`, origNotifID,
	).Scan(&origCount)
	require.NoError(t, err)
	require.Equal(t, 1, origCount,
		"transfer_suggestion notification must NOT have been deleted by the down-migration DELETE")

	// Verify the match-type notification is gone.
	var matchCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE id = $1`, matchNotifID,
	).Scan(&matchCount)
	require.NoError(t, err)
	require.Equal(t, 0, matchCount,
		"transfer_suggestion_match notification must have been deleted by the down-migration DELETE")

	// Verify the restored CHECK still accepts the original type (no regression).
	// IMPORTANT: this check must run BEFORE the intentionally-failing insert below,
	// because a Postgres CHECK violation (23514) aborts the entire transaction —
	// any subsequent statement on the same tx would fail with "current transaction is
	// aborted, commands ignored until end of transaction block", not the constraint check.
	newOrigID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion', 'Accepted type', 'Msg', false, false, 'accept_reject', $3, NOW() + interval '2s', NOW())
	`, newOrigID, userID, suggID)
	require.NoError(t, err,
		"INSERT with type='transfer_suggestion' must still succeed after the original constraint is restored")

	// Verify the restored CHECK rejects a new transfer_suggestion_match INSERT with SQLSTATE 23514.
	// This intentionally aborts the transaction — no more tx use after this point.
	newMatchID := uuid.New()
	_, insertErr := tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, 'transfer_suggestion_match', 'New fuzzy', 'Msg', false, false, 'accept_reject', $3, NOW() + interval '3s', NOW())
	`, newMatchID, userID, suggID)
	require.Error(t, insertErr,
		"INSERT with type='transfer_suggestion_match' must fail after the original constraint is restored")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, insertErr, &pgErr,
		"the error must be a pgconn.PgError (postgres-level constraint violation)")
	require.Equal(t, "23514", pgErr.Code,
		"SQLSTATE must be 23514 (check_violation), not some other error class")
	// tx is now in aborted state; defer tx.Rollback(ctx) at the top will clean up.
}
