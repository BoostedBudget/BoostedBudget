package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// HouseholdRepository provides the minimal household reads Phase 2 needs for the
// /households/me/entitlement endpoint. Write paths (create household, invite-accept,
// member removal) are Phase 3 — see #1367.
type HouseholdRepository interface {
	// GetHouseholdForUser returns the household the user is an active member of, whether
	// they are its owner, and whether any active membership was found at all. A user with
	// no active membership (found=false) is not in any household — the normal solo case.
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)

	// CountActiveMembers returns the number of active (status='active') members,
	// including the owner's own row.
	CountActiveMembers(ctx context.Context, householdID string) (int, error)

	// CountPendingInvites returns the number of pending invites that have not yet
	// expired. Expired-but-unswept invites are excluded (#1366 Codex MAJOR 8).
	CountPendingInvites(ctx context.Context, householdID string) (int, error)
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
