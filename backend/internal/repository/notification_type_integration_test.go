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
