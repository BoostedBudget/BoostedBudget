package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHouseholdInviteMockDB(t *testing.T) (pgxmock.PgxPoolIface, HouseholdInviteRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	enc := newTestFieldEncryptor(t)
	return mock, NewHouseholdInviteRepository(mock, enc)
}

func TestHouseholdInviteRepository_Create(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	expires := time.Now().Add(48 * time.Hour)
	inv := &domain.HouseholdInvite{
		HouseholdID:   "hh-1",
		InviterUserID: "owner-1",
		InviteeEmail:  "Friend@Example.com",
		TokenHash:     "abc123",
		Status:        domain.HouseholdInviteStatusPending,
		ExpiresAt:     expires,
	}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		RETURNING id, created_at, updated_at
	`)).
		WithArgs("hh-1", "owner-1", pgxmock.AnyArg(), pgxmock.AnyArg(), "abc123", expires).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("inv-1", now, now))

	err := repo.Create(context.Background(), inv)
	require.NoError(t, err)
	assert.Equal(t, "inv-1", inv.ID)
}

func TestHouseholdInviteRepository_Create_DuplicatePending(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	inv := &domain.HouseholdInvite{
		HouseholdID: "hh-1", InviterUserID: "owner-1", InviteeEmail: "friend@example.com",
		TokenHash: "abc123", Status: domain.HouseholdInviteStatusPending, ExpiresAt: time.Now().Add(48 * time.Hour),
	}
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		RETURNING id, created_at, updated_at
	`)).
		WithArgs("hh-1", "owner-1", pgxmock.AnyArg(), pgxmock.AnyArg(), "abc123", inv.ExpiresAt).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_household_invites_pending_email"})

	err := repo.Create(context.Background(), inv)
	assert.ErrorIs(t, err, domain.ErrInvitePendingExists)
}

func TestHouseholdInviteRepository_GetByTokenHashForUpdate(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	now := time.Now()
	expires := now.Add(48 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at, accepted_user_id, accepted_at, created_at, updated_at
		FROM household_invites WHERE token_hash = $1 FOR UPDATE
	`)).
		WithArgs("abc123").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
			"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
		}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "abc123", "pending", expires, nil, nil, now, now))

	inv, err := repo.GetByTokenHashForUpdate(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "friend@example.com", inv.InviteeEmail, "email should be decrypted")
	assert.Equal(t, domain.HouseholdInviteStatusPending, inv.Status)
}

func TestHouseholdInviteRepository_GetByTokenHashForUpdate_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at, accepted_user_id, accepted_at, created_at, updated_at
		FROM household_invites WHERE token_hash = $1 FOR UPDATE
	`)).
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetByTokenHashForUpdate(context.Background(), "missing")
	assert.ErrorIs(t, err, domain.ErrInviteNotFound)
}

// GT-6 (Gap Analysis Round 4B): AcceptInvite needs to know which household to lock
// BEFORE locking anything, to avoid a lock-order inversion against DeleteHousehold (which
// locks the household first, then cascade-deletes household_invites rows). This is a
// deliberately non-locking lookup — the authoritative, race-safe read is still
// GetByTokenHashForUpdate, called after the household lock is held.
func TestHouseholdInviteRepository_GetHouseholdIDByTokenHash(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT household_id FROM household_invites WHERE token_hash = $1
	`)).
		WithArgs("abc123").
		WillReturnRows(pgxmock.NewRows([]string{"household_id"}).AddRow("hh-1"))

	householdID, err := repo.GetHouseholdIDByTokenHash(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "hh-1", householdID)
}

func TestHouseholdInviteRepository_GetHouseholdIDByTokenHash_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT household_id FROM household_invites WHERE token_hash = $1
	`)).
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetHouseholdIDByTokenHash(context.Background(), "missing")
	assert.ErrorIs(t, err, domain.ErrInviteNotFound)
}

func TestHouseholdInviteRepository_GetPreviewByTokenHash(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	expires := time.Now().Add(48 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.name, i.household_id, i.inviter_user_id, i.invitee_email, i.expires_at
		FROM household_invites i JOIN households h ON h.id = i.household_id
		WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()
	`)).
		WithArgs("abc123").
		WillReturnRows(pgxmock.NewRows([]string{"name", "household_id", "inviter_user_id", "invitee_email", "expires_at"}).
			AddRow("The Smiths", "hh-1", "owner-1", "friend@example.com", expires))

	preview, err := repo.GetPreviewByTokenHash(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "The Smiths", preview.HouseholdName)
	assert.Equal(t, "friend@example.com", preview.InviteeEmail)
}

func TestHouseholdInviteRepository_GetPreviewByTokenHash_NotFoundIndistinguishable(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT h.name, i.household_id, i.inviter_user_id, i.invitee_email, i.expires_at
		FROM household_invites i JOIN households h ON h.id = i.household_id
		WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()
	`)).
		WithArgs("expired-or-unknown").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetPreviewByTokenHash(context.Background(), "expired-or-unknown")
	assert.ErrorIs(t, err, domain.ErrInviteNotFound)
}

func TestHouseholdInviteRepository_MarkAccepted(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'accepted', accepted_user_id = $1, accepted_at = now(), updated_at = now()
		WHERE id = $2 AND status = 'pending'
	`)).
		WithArgs("user-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkAccepted(context.Background(), "inv-1", "user-1")
	require.NoError(t, err)
}

func TestHouseholdInviteRepository_MarkAccepted_NotPending(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'accepted', accepted_user_id = $1, accepted_at = now(), updated_at = now()
		WHERE id = $2 AND status = 'pending'
	`)).
		WithArgs("user-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.MarkAccepted(context.Background(), "inv-1", "user-1")
	assert.ErrorIs(t, err, domain.ErrInviteNotPending)
}

func TestHouseholdInviteRepository_MarkDeclined(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'declined', updated_at = now()
		WHERE id = $1 AND status = 'pending'
	`)).
		WithArgs("inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkDeclined(context.Background(), "inv-1")
	require.NoError(t, err)
}

func TestHouseholdInviteRepository_Revoke(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND household_id = $2 AND status = 'pending'
	`)).
		WithArgs("inv-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Revoke(context.Background(), "hh-1", "inv-1")
	require.NoError(t, err)
}

func TestHouseholdInviteRepository_Revoke_NotFound(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND household_id = $2 AND status = 'pending'
	`)).
		WithArgs("inv-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Revoke(context.Background(), "hh-1", "inv-1")
	assert.ErrorIs(t, err, domain.ErrInviteNotFound)
}

func TestHouseholdInviteRepository_RevokeAllPendingForHousehold(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE household_invites SET status = 'revoked', updated_at = now()
		WHERE household_id = $1 AND status = 'pending'
	`)).
		WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := repo.RevokeAllPendingForHousehold(context.Background(), "hh-1")
	require.NoError(t, err)
}

func TestHouseholdInviteRepository_ListPendingByHousehold(t *testing.T) {
	t.Parallel()
	mock, repo := setupHouseholdInviteMockDB(t)
	defer mock.Close()

	now := time.Now()
	expires := now.Add(48 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at, accepted_user_id, accepted_at, created_at, updated_at
		FROM household_invites WHERE household_id = $1 AND status = 'pending' ORDER BY created_at DESC
	`)).
		WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "inviter_user_id", "invitee_email", "invitee_email_blind_idx", "token_hash",
			"status", "expires_at", "accepted_user_id", "accepted_at", "created_at", "updated_at",
		}).AddRow("inv-1", "hh-1", "owner-1", "friend@example.com", "blind-1", "abc123", "pending", expires, nil, nil, now, now))

	invites, err := repo.ListPendingByHousehold(context.Background(), "hh-1")
	require.NoError(t, err)
	require.Len(t, invites, 1)
	assert.Equal(t, "friend@example.com", invites[0].InviteeEmail)
}
