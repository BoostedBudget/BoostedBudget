package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Constraint names from migrations 000106/000107/000109/000112 — pinned here so
// 23505 mapping stays in sync with the DB. A rename must update both the migration
// and this list (see household_repository_integration_test.go for a regression check).
const (
	constraintHouseholdsOwnerUnique     = "uq_households_owner_user_id"
	constraintHouseholdMembersActive    = "uq_household_members_active_user"
	constraintHouseholdMembersOwnerOnce = "uq_household_members_active_owner"
	constraintInvitesPendingEmail       = "uq_household_invites_pending_email"
	constraintInvitesTokenHash          = "uq_household_invites_token_hash"
	constraintEntitlementsActiveUser    = "uq_subscription_entitlements_active_user"
)

// HouseholdRepository provides household reads and writes. Phase 2 (#1366) shipped the
// reads; Phase 3 (#1367) adds all write paths: create household, member management,
// and the FOR UPDATE lock used by the invite-accept seat re-check.
type HouseholdRepository interface {
	// GetHouseholdForUser returns the household the user is an active member of, whether
	// they are its owner, and whether any active membership was found at all. A user with
	// no active membership (found=false) is not in any household — the normal solo case.
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)

	// LockActiveMembershipForUser takes a row lock on the caller's own active
	// household_members row (FOR UPDATE OF hm) and returns the household it belongs to.
	// This serializes against RemoveMember, which updates that same row — so a caller
	// holding this lock is guaranteed not to be concurrently removed for the remainder
	// of its transaction. Returns domain.ErrHouseholdNotFound if the user has no active
	// membership. #1368 — Phase 4 (share-visibility TOCTOU guard).
	LockActiveMembershipForUser(ctx context.Context, userID string) (household *domain.Household, err error)

	// CountActiveMembers returns the number of active (status='active') members,
	// including the owner's own row.
	CountActiveMembers(ctx context.Context, householdID string) (int, error)

	// CountPendingInvites returns the number of pending invites that have not yet
	// expired. Expired-but-unswept invites are excluded (#1366 Codex MAJOR 8).
	CountPendingInvites(ctx context.Context, householdID string) (int, error)

	// CreateHousehold inserts a new households row, populating h.ID/CreatedAt/UpdatedAt.
	// Returns domain.ErrAlreadyOwnsHousehold if the caller already owns a household
	// (uq_households_owner_user_id).
	CreateHousehold(ctx context.Context, h *domain.Household) error

	// LockHouseholdForUpdate takes a row lock on the household and returns its
	// owner_user_id. This is the serialization point for the invite-accept seat
	// re-check and invite-create seat gate. Returns domain.ErrHouseholdNotFound if
	// no such household exists.
	LockHouseholdForUpdate(ctx context.Context, householdID string) (ownerUserID string, err error)

	// UpdateHouseholdName renames the household. Returns domain.ErrNotHouseholdOwner
	// if the caller does not own householdID (or it doesn't exist).
	UpdateHouseholdName(ctx context.Context, householdID, ownerUserID, name string) error

	// DeleteHousehold deletes the household row (cascades members/invites/shares).
	// Returns domain.ErrNotHouseholdOwner if the caller does not own householdID.
	DeleteHousehold(ctx context.Context, householdID, ownerUserID string) error

	// InsertMember inserts a household_members row, populating m.ID/JoinedAt/CreatedAt/
	// UpdatedAt. Returns domain.ErrAlreadyInHousehold if the user already has an active
	// membership elsewhere, or domain.ErrAlreadyOwnsHousehold if inserting a second
	// active owner row for the household.
	InsertMember(ctx context.Context, m *domain.HouseholdMember) error

	// ListMembers returns all active members of a household, ordered by joined_at.
	ListMembers(ctx context.Context, householdID string) ([]domain.HouseholdMember, error)

	// GetActiveMember returns the caller's active membership row in householdID.
	// Returns domain.ErrMemberNotFound if none exists.
	GetActiveMember(ctx context.Context, householdID, userID string) (*domain.HouseholdMember, error)

	// RemoveMember marks a member's row status='removed', removed_at=now(). Only
	// affects role='member' rows — the owner cannot be removed this way (returns
	// domain.ErrMemberNotFound, same as any other not-found case, to avoid an
	// owner-vs-missing oracle). Never touches Plaid Items.
	RemoveMember(ctx context.Context, householdID, userID string) error

	// LockHouseholdByID takes a row lock (FOR UPDATE) on the household and returns its
	// full record, keyed directly by householdID rather than by a caller's own active
	// membership (GetHouseholdForUser) or bare owner ID (LockHouseholdForUpdate). Used
	// by household-scoped write paths that start from a householdID with no calling
	// user in context (e.g. a background worker auto-offboarding excess members).
	// Returns domain.ErrHouseholdNotFound if no such household exists. #1371 — Phase 7
	// foundation.
	LockHouseholdByID(ctx context.Context, householdID string) (*domain.Household, error)

	// DeleteRemovedMember permanently deletes a household_members row identified by its
	// own id, but ONLY if its status is 'removed' — it will never delete an 'active' row,
	// which would bypass RemoveMember's authorization/owner-protection checks. Returns
	// the household_id and user_id the deleted row belonged to, so the caller can clean
	// up dependent household_account_shares/subscription_entitlements rows. Returns
	// domain.ErrMemberNotFound if no such removed row exists (already purged, still
	// active, or unknown id). #1371 — Phase 7 foundation (purge-after-30-days worker).
	DeleteRemovedMember(ctx context.Context, membershipID string) (householdID, userID string, err error)
}

type householdRepository struct {
	db DB
}

// NewHouseholdRepository creates a new HouseholdRepository instance.
func NewHouseholdRepository(db DB) HouseholdRepository {
	return &householdRepository{db: db}
}

func (r *householdRepository) GetHouseholdForUser(ctx context.Context, userID string) (*domain.Household, bool, bool, error) {
	var h domain.Household
	var role string
	err := r.db.QueryRow(ctx, `
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at, hm.role
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		LIMIT 1
	`, userID).Scan(&h.ID, &h.OwnerUserID, &h.Name, &h.CreatedAt, &h.UpdatedAt, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	return &h, role == string(domain.HouseholdRoleOwner), true, nil
}

func (r *householdRepository) LockActiveMembershipForUser(ctx context.Context, userID string) (*domain.Household, error) {
	var h domain.Household
	err := r.db.QueryRow(ctx, `
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		FOR UPDATE OF hm
	`, userID).Scan(&h.ID, &h.OwnerUserID, &h.Name, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrHouseholdNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *householdRepository) CountActiveMembers(ctx context.Context, householdID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM household_members WHERE household_id = $1 AND status = 'active'
	`, householdID).Scan(&count)
	return count, err
}

func (r *householdRepository) CountPendingInvites(ctx context.Context, householdID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM household_invites
		WHERE household_id = $1 AND status = 'pending' AND expires_at > now()
	`, householdID).Scan(&count)
	return count, err
}

func (r *householdRepository) CreateHousehold(ctx context.Context, h *domain.Household) error {
	err := r.db.QueryRow(ctx, `
		INSERT INTO households (owner_user_id, name) VALUES ($1, $2)
		RETURNING id, created_at, updated_at
	`, h.OwnerUserID, h.Name).Scan(&h.ID, &h.CreatedAt, &h.UpdatedAt)
	if isUniqueViolation(err, constraintHouseholdsOwnerUnique) {
		return domain.ErrAlreadyOwnsHousehold
	}
	return err
}

func (r *householdRepository) LockHouseholdForUpdate(ctx context.Context, householdID string) (string, error) {
	var ownerUserID string
	err := r.db.QueryRow(ctx, `
		SELECT owner_user_id FROM households WHERE id = $1 FOR UPDATE
	`, householdID).Scan(&ownerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrHouseholdNotFound
	}
	return ownerUserID, err
}

func (r *householdRepository) UpdateHouseholdName(ctx context.Context, householdID, ownerUserID, name string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE households SET name = $1, updated_at = now() WHERE id = $2 AND owner_user_id = $3
	`, name, householdID, ownerUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotHouseholdOwner
	}
	return nil
}

func (r *householdRepository) DeleteHousehold(ctx context.Context, householdID, ownerUserID string) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM households WHERE id = $1 AND owner_user_id = $2
	`, householdID, ownerUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotHouseholdOwner
	}
	return nil
}

func (r *householdRepository) InsertMember(ctx context.Context, m *domain.HouseholdMember) error {
	err := r.db.QueryRow(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status, prior_subscription_tier, prior_subscription_status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, joined_at, created_at, updated_at
	`, m.HouseholdID, m.UserID, m.Role, m.Status, m.PriorSubscriptionTier, m.PriorSubscriptionStatus).
		Scan(&m.ID, &m.JoinedAt, &m.CreatedAt, &m.UpdatedAt)
	switch {
	case isUniqueViolation(err, constraintHouseholdMembersActive):
		return domain.ErrAlreadyInHousehold
	case isUniqueViolation(err, constraintHouseholdMembersOwnerOnce):
		return domain.ErrAlreadyOwnsHousehold
	default:
		return err
	}
}

func (r *householdRepository) ListMembers(ctx context.Context, householdID string) ([]domain.HouseholdMember, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND status = 'active' ORDER BY joined_at ASC
	`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []domain.HouseholdMember
	for rows.Next() {
		var m domain.HouseholdMember
		if err := rows.Scan(&m.ID, &m.HouseholdID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.RemovedAt,
			&m.PriorSubscriptionTier, &m.PriorSubscriptionStatus, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (r *householdRepository) GetActiveMember(ctx context.Context, householdID, userID string) (*domain.HouseholdMember, error) {
	var m domain.HouseholdMember
	err := r.db.QueryRow(ctx, `
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'active'
	`, householdID, userID).Scan(&m.ID, &m.HouseholdID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.RemovedAt,
		&m.PriorSubscriptionTier, &m.PriorSubscriptionStatus, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *householdRepository) RemoveMember(ctx context.Context, householdID, userID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE household_members SET status = 'removed', removed_at = now(), updated_at = now()
		WHERE household_id = $1 AND user_id = $2 AND status = 'active' AND role = 'member'
	`, householdID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func (r *householdRepository) LockHouseholdByID(ctx context.Context, householdID string) (*domain.Household, error) {
	var h domain.Household
	err := r.db.QueryRow(ctx, `
		SELECT id, owner_user_id, name, created_at, updated_at
		FROM households WHERE id = $1 FOR UPDATE
	`, householdID).Scan(&h.ID, &h.OwnerUserID, &h.Name, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrHouseholdNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *householdRepository) DeleteRemovedMember(ctx context.Context, membershipID string) (string, string, error) {
	var householdID, userID string
	err := r.db.QueryRow(ctx, `
		DELETE FROM household_members WHERE id = $1 AND status = 'removed'
		RETURNING household_id, user_id
	`, membershipID).Scan(&householdID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrMemberNotFound
	}
	if err != nil {
		return "", "", err
	}
	return householdID, userID, nil
}

// isUniqueViolation reports whether err is a Postgres 23505 unique_violation on the
// named constraint/index. Used to translate constraint failures into domain sentinels
// so callers (and the error mapper) never see a raw pgconn.PgError.
func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}
