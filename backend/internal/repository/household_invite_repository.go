package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

const householdInviteSelectFields = `id, household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at, accepted_user_id, accepted_at, created_at, updated_at`

// HouseholdInvitePreview is the unauthenticated, metadata-only projection returned by
// GET /households/invites/{token}. It deliberately excludes the invitee's account list —
// see #1367 decision #1. InviteeEmail is decrypted for display.
type HouseholdInvitePreview struct {
	HouseholdName string
	HouseholdID   string
	InviterUserID string
	InviteeEmail  string
	ExpiresAt     time.Time
}

// HouseholdInviteRepository manages household_invites rows: encrypted-email writes,
// token-hash lookups (including the FOR UPDATE lock used by accept/decline), and
// lifecycle transitions. Issue #1367 — Phase 3.
type HouseholdInviteRepository interface {
	// Create inserts a pending invite. inv.InviteeEmail must be plaintext; it is
	// encrypted and blind-indexed before persisting. inv.TokenHash must already be the
	// sha256 hex digest of the raw token (never persist the raw token). Populates
	// inv.ID/CreatedAt/UpdatedAt. Returns domain.ErrInvitePendingExists if a pending
	// invite already exists for this household+email.
	Create(ctx context.Context, inv *domain.HouseholdInvite) error

	// GetByTokenHashForUpdate locks and returns the invite row for tokenHash, with
	// InviteeEmail decrypted. Returns domain.ErrInviteNotFound if no such invite exists.
	// Locking this row serializes concurrent accept/decline of the same token.
	GetByTokenHashForUpdate(ctx context.Context, tokenHash string) (*domain.HouseholdInvite, error)

	// GetHouseholdIDByTokenHash is a deliberately NON-LOCKING lookup used only to learn
	// which household to lock BEFORE acquiring any row lock (#1367 Gap Analysis Round 4B,
	// GT-6: AcceptInvite must lock the household before the invite row, matching
	// DeleteHousehold/CreateInvite's order, to avoid a lock-order-inversion deadlock — see
	// DeleteHousehold's godoc). The authoritative, race-safe read is still
	// GetByTokenHashForUpdate, called AFTER the household lock is held; never trust this
	// method's result for anything beyond "which household to lock first". Returns
	// domain.ErrInviteNotFound if no such invite exists.
	GetHouseholdIDByTokenHash(ctx context.Context, tokenHash string) (string, error)

	// GetPreviewByTokenHash returns the unauthenticated preview projection. Returns
	// domain.ErrInviteNotFound uniformly for unknown, expired, and non-pending
	// (accepted/declined/revoked) tokens — callers must not distinguish these to avoid
	// a status oracle.
	GetPreviewByTokenHash(ctx context.Context, tokenHash string) (*HouseholdInvitePreview, error)

	// ListPendingByHousehold returns pending invites for the owner's invite-management
	// view, newest first, with InviteeEmail decrypted.
	ListPendingByHousehold(ctx context.Context, householdID string) ([]domain.HouseholdInvite, error)

	// MarkAccepted conditionally flips a pending invite to accepted. Returns
	// domain.ErrInviteNotPending if the invite is not currently pending (already
	// accepted/declined/revoked/expired, or a concurrent transition won the race).
	MarkAccepted(ctx context.Context, id, acceptedUserID string) error

	// MarkDeclined conditionally flips a pending invite to declined. Returns
	// domain.ErrInviteNotPending if not currently pending.
	MarkDeclined(ctx context.Context, id string) error

	// Revoke conditionally flips a pending invite owned by householdID to revoked.
	// Returns domain.ErrInviteNotFound if no matching pending invite exists (wrong
	// household or already transitioned).
	Revoke(ctx context.Context, householdID, id string) error

	// RevokeAllPendingForHousehold revokes every pending invite for a household. Used
	// by the owner-deletion guard so a grace-period accept cannot join a doomed
	// household (Codex #1367 review, High finding 5). Idempotent.
	RevokeAllPendingForHousehold(ctx context.Context, householdID string) error
}

type householdInviteRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewHouseholdInviteRepository creates a new HouseholdInviteRepository instance.
func NewHouseholdInviteRepository(db DB, enc *appcrypto.FieldEncryptor) HouseholdInviteRepository {
	return &householdInviteRepository{db: db, enc: enc}
}

func (r *householdInviteRepository) Create(ctx context.Context, inv *domain.HouseholdInvite) error {
	encEmail, err := r.enc.EncryptString(inv.InviteeEmail)
	if err != nil {
		return err
	}
	blindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(inv.InviteeEmail))

	err = r.db.QueryRow(ctx, `
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		RETURNING id, created_at, updated_at
	`, inv.HouseholdID, inv.InviterUserID, encEmail, blindIdx, inv.TokenHash, inv.ExpiresAt).
		Scan(&inv.ID, &inv.CreatedAt, &inv.UpdatedAt)
	if isUniqueViolation(err, constraintInvitesPendingEmail) {
		return domain.ErrInvitePendingExists
	}
	return err
}

func (r *householdInviteRepository) scanInvite(row pgx.Row) (*domain.HouseholdInvite, error) {
	var inv domain.HouseholdInvite
	var encEmail string
	err := row.Scan(&inv.ID, &inv.HouseholdID, &inv.InviterUserID, &encEmail, &inv.InviteeEmailBlindIdx,
		&inv.TokenHash, &inv.Status, &inv.ExpiresAt, &inv.AcceptedUserID, &inv.AcceptedAt, &inv.CreatedAt, &inv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	plainEmail, err := r.enc.DecryptString(encEmail)
	if err != nil {
		return nil, err
	}
	inv.InviteeEmail = plainEmail
	return &inv, nil
}

func (r *householdInviteRepository) GetByTokenHashForUpdate(ctx context.Context, tokenHash string) (*domain.HouseholdInvite, error) {
	row := r.db.QueryRow(ctx, `
		SELECT `+householdInviteSelectFields+`
		FROM household_invites WHERE token_hash = $1 FOR UPDATE
	`, tokenHash)
	inv, err := r.scanInvite(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInviteNotFound
	}
	return inv, err
}

func (r *householdInviteRepository) GetHouseholdIDByTokenHash(ctx context.Context, tokenHash string) (string, error) {
	var householdID string
	err := r.db.QueryRow(ctx, `
		SELECT household_id FROM household_invites WHERE token_hash = $1
	`, tokenHash).Scan(&householdID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrInviteNotFound
	}
	return householdID, err
}

func (r *householdInviteRepository) GetPreviewByTokenHash(ctx context.Context, tokenHash string) (*HouseholdInvitePreview, error) {
	var p HouseholdInvitePreview
	var encEmail string
	err := r.db.QueryRow(ctx, `
		SELECT h.name, i.household_id, i.inviter_user_id, i.invitee_email, i.expires_at
		FROM household_invites i JOIN households h ON h.id = i.household_id
		WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()
	`, tokenHash).Scan(&p.HouseholdName, &p.HouseholdID, &p.InviterUserID, &encEmail, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	plainEmail, err := r.enc.DecryptString(encEmail)
	if err != nil {
		return nil, err
	}
	p.InviteeEmail = plainEmail
	return &p, nil
}

func (r *householdInviteRepository) ListPendingByHousehold(ctx context.Context, householdID string) ([]domain.HouseholdInvite, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+householdInviteSelectFields+`
		FROM household_invites WHERE household_id = $1 AND status = 'pending' ORDER BY created_at DESC
	`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []domain.HouseholdInvite
	for rows.Next() {
		inv, err := r.scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, *inv)
	}
	return invites, rows.Err()
}

func (r *householdInviteRepository) MarkAccepted(ctx context.Context, id, acceptedUserID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE household_invites SET status = 'accepted', accepted_user_id = $1, accepted_at = now(), updated_at = now()
		WHERE id = $2 AND status = 'pending'
	`, acceptedUserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInviteNotPending
	}
	return nil
}

func (r *householdInviteRepository) MarkDeclined(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE household_invites SET status = 'declined', updated_at = now()
		WHERE id = $1 AND status = 'pending'
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInviteNotPending
	}
	return nil
}

func (r *householdInviteRepository) Revoke(ctx context.Context, householdID, id string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE household_invites SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND household_id = $2 AND status = 'pending'
	`, id, householdID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInviteNotFound
	}
	return nil
}

func (r *householdInviteRepository) RevokeAllPendingForHousehold(ctx context.Context, householdID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE household_invites SET status = 'revoked', updated_at = now()
		WHERE household_id = $1 AND status = 'pending'
	`, householdID)
	return err
}
