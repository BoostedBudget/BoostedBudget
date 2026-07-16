package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

	// ListMembers returns all active members of a household, ordered by joined_at ASC
	// with a deterministic id ASC tie-breaker (Gap Analysis Round 3, GT-4).
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

	// SetOverCapacity idempotently starts the over-capacity grace clock: sets
	// over_capacity_since = since, but ONLY if it is not already set. A second
	// call while grace is already active is a no-op that leaves the original
	// grace deadline untouched — but, unlike a bare no-op, ALWAYS returns the
	// true effective over_capacity_since in one round-trip (via a CTE that only
	// executes the UPDATE on a real NULL-to-value transition; see the
	// implementation's doc comment for why — households' BEFORE UPDATE trigger
	// would otherwise bump updated_at on every no-op call), whichever writer got
	// there first. This lets a caller that lost a race against a concurrent
	// writer (e.g. the subscription-webhook fast path) detect it and use the
	// real, earlier timestamp instead of blindly trusting its own local `since`
	// — which would otherwise let a fabricated, too-late grace deadline reach
	// the owner in a notice email. Does not touch the notice-sent columns.
	// #1371 — Phase 7 (Task 1; consumed by the Task 6 grace worker).
	SetOverCapacity(ctx context.Context, householdID string, since time.Time) (time.Time, error)

	// ClearOverCapacity nulls all three over-capacity columns (grace anchor +
	// both notice-sent markers), ending the current grace period. Idempotent —
	// safe to call on a household not currently in grace. #1371 — Phase 7.
	ClearOverCapacity(ctx context.Context, householdID string) error

	// MarkOverCapacityStartNoticeSent records that the "you're over capacity"
	// warning email has been sent for the current grace period, so the Task 6
	// worker never sends it twice. #1371 — Phase 7.
	MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time) error

	// MarkOverCapacityWarn3dSent records that the "3 days left" warning email
	// has been sent for the current grace period, so the Task 6 worker never
	// sends it twice. #1371 — Phase 7.
	MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time) error

	// ListHouseholdsWithMembers returns every household that currently has at
	// least one active member, regardless of over-capacity state. This is the
	// Task 6 daily backstop-worker's scan target — it must see every household
	// with members (not just ones already flagged over-capacity) so the worker
	// can detect over-capacity even if an earlier detection step was missed.
	// #1371 — Phase 7.
	ListHouseholdsWithMembers(ctx context.Context) ([]*domain.Household, error)

	// GetRemovedMemberForUpdate looks up a household_members row for (householdID,
	// userID) with status='removed' AND removed_at within the last withinDays days,
	// taking a FOR UPDATE row lock. If more than one historical removed row exists for
	// the pair (within the window), returns the most recently removed one (ORDER BY
	// removed_at DESC, created_at DESC). Returns domain.ErrMemberNotFound if no such
	// removed row exists within the window — including when a removed row DOES exist
	// but is older than withinDays (Gap Analysis Round 3, GT-3: enforces this method's
	// own documented 30-day promise instead of silently restoring an arbitrarily stale
	// removal if the Task 2 purge worker is ever delayed/paused). Used by the Task 4
	// re-invite flow to safely restore a removed member within the 30-day window.
	// #1371 — Phase 7 (Task 1 foundation).
	GetRemovedMemberForUpdate(ctx context.Context, householdID, userID string, withinDays int) (*domain.HouseholdMember, error)

	// RestoreMember reactivates a removed household_members row identified by its
	// own id: sets status='active', removed_at=NULL, updated_at=now(), returning the
	// freshly-written updated_at so callers can keep an in-memory domain.HouseholdMember
	// authoritative after the write (mirroring InsertMember's RETURNING pattern — Codex
	// review, #1371 Task 4: without this, a caller that manually syncs Status/RemovedAt
	// on its in-memory struct would leak a stale updated_at into an API response). Only
	// affects rows currently status='removed' (guarded by WHERE status='removed',
	// mirroring DeleteRemovedMember) — returns domain.ErrMemberNotFound if the row
	// is missing or not currently removed. Returns domain.ErrAlreadyInHousehold if
	// the user has since become active elsewhere (uq_household_members_active_user
	// is global, not household-scoped) or domain.ErrAlreadyOwnsHousehold on the
	// analogous owner-row conflict — same mapping InsertMember uses. #1371 — Phase 7
	// (Task 1 foundation; consumed by Task 4's re-invite flow).
	RestoreMember(ctx context.Context, membershipID string) (time.Time, error)

	// ListMembershipsPastPurgeWindow returns every household_members row with
	// status='removed' whose removed_at is older than purgeAfterDays days ago,
	// ordered oldest-removed-first. This is the Task 2 daily purge-worker's scan
	// target; each candidate is expected to be passed to
	// HouseholdService.PurgeRemovedMembership by the caller. #1371 — Phase 7 (Task 2).
	ListMembershipsPastPurgeWindow(ctx context.Context, purgeAfterDays int) ([]*domain.HouseholdMember, error)
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
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at, h.over_capacity_since, h.over_capacity_start_notice_sent_at, h.over_capacity_warn3d_sent_at, hm.role
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		LIMIT 1
	`, userID).Scan(append(householdScanArgs(&h), &role)...)
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
		SELECT h.id, h.owner_user_id, h.name, h.created_at, h.updated_at, h.over_capacity_since, h.over_capacity_start_notice_sent_at, h.over_capacity_warn3d_sent_at
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE hm.user_id = $1 AND hm.status = 'active'
		FOR UPDATE OF hm
	`, userID).Scan(householdScanArgs(&h)...)
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
	// id ASC is a deterministic tie-breaker (Gap Analysis Round 3, GT-4): without it, two
	// members sharing an identical joined_at timestamp would make OffboardExcessMembers's
	// LIFO (most-recently-joined-first) removal nondeterministic. Mirrors the same
	// "<primary> ASC, id ASC" tie-breaker convention already used by
	// user_account_repository.go.
	rows, err := r.db.Query(ctx, `
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND status = 'active' ORDER BY joined_at ASC, id ASC
	`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []domain.HouseholdMember
	for rows.Next() {
		var m domain.HouseholdMember
		if err := rows.Scan(householdMemberScanArgs(&m)...); err != nil {
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
	`, householdID, userID).Scan(householdMemberScanArgs(&m)...)
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
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households WHERE id = $1 FOR UPDATE
	`, householdID).Scan(householdScanArgs(&h)...)
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

// SetOverCapacity uses a CTE rather than a plain `UPDATE ... SET x = COALESCE(x,
// $2)` so that a race-losing call (grace already active) never issues an UPDATE
// against the row at all. households has a BEFORE UPDATE trigger
// (update_households_updated_at / update_updated_at_column) that unconditionally
// sets NEW.updated_at = NOW() on ANY UPDATE statement matching the row, with no
// `OLD IS DISTINCT FROM NEW` guard — so a plain COALESCE-based UPDATE always fires
// it, even when the COALESCE left the value unchanged. That's purely cosmetic
// metadata churn (the returned/effective over_capacity_since is still correct
// either way), but it's needless: the `upd` CTE's UPDATE only matches when
// over_capacity_since IS NULL (a real state transition), so the trigger only ever
// fires on a genuine change.
//
// The fallback read (`locked_read`) MUST use `FOR UPDATE`, not a plain SELECT —
// this was a real, empirically-confirmed bug in an earlier version of this
// method (Codex review finding, Round 7 follow-up): under READ COMMITTED, a
// bare SELECT sharing the same statement as a data-modifying CTE is bound to
// the statement's ORIGINAL snapshot, even after the CTE's own UPDATE blocks on
// a concurrent writer's row lock and re-checks against the LATEST committed
// row. So when this call loses the race (another transaction committed
// over_capacity_since first, in between this statement's snapshot and its
// UPDATE's post-wait re-check), a plain fallback SELECT can still see the
// PRE-race snapshot — i.e. over_capacity_since still NULL — causing a NULL
// scan into effectiveSince instead of the real, committed value. `FOR UPDATE`
// forces this read through the same wait-then-see-latest-committed-version
// machinery the embedded UPDATE itself relies on, so it's guaranteed to
// observe the true effective value. Verified directly against real Postgres
// with two concurrent sessions racing on the same row (see PR discussion) —
// the plain-SELECT version reliably returned NULL for the race loser; this
// FOR UPDATE version reliably returns the winner's committed value.
//
// `NOT EXISTS (SELECT 1 FROM upd)` makes the two SELECT branches mutually
// exclusive, so — combined with households.id being the primary key — the
// whole statement returns at most one row: the CTE's RETURNING value on a
// real transition, or a FOR-UPDATE-locked read of the current value on a
// no-op. If the household doesn't exist, both branches return zero rows and
// Scan gets pgx.ErrNoRows, matching the not-found signal a plain UPDATE would
// give.
func (r *householdRepository) SetOverCapacity(ctx context.Context, householdID string, since time.Time) (time.Time, error) {
	var effectiveSince time.Time
	err := r.db.QueryRow(ctx, `
		WITH upd AS (
			UPDATE households SET over_capacity_since = $2
			WHERE id = $1 AND over_capacity_since IS NULL
			RETURNING over_capacity_since
		), locked_read AS (
			SELECT over_capacity_since FROM households
			WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM upd)
			FOR UPDATE
		)
		SELECT over_capacity_since FROM upd
		UNION ALL
		SELECT over_capacity_since FROM locked_read
	`, householdID, since).Scan(&effectiveSince)
	return effectiveSince, err
}

func (r *householdRepository) ClearOverCapacity(ctx context.Context, householdID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE households SET over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL, over_capacity_warn3d_sent_at = NULL WHERE id = $1
	`, householdID)
	return err
}

// MarkOverCapacityStartNoticeSent stamps the initial grace notice as sent. Guarded by
// `over_capacity_since IS NOT NULL` — Round 5 Gap Analysis fix (#1371): the daily
// backstop worker can race a concurrent read-only-owner ClearOverCapacity (webhook fast
// path or the worker's own next run), and without this guard a stale notice-sent stamp
// could be written onto a household that's no longer in grace. A 0-row UPDATE is not an
// error, preserving the existing nil-on-success contract.
func (r *householdRepository) MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE households SET over_capacity_start_notice_sent_at = $2 WHERE id = $1 AND over_capacity_since IS NOT NULL
	`, householdID, sentAt)
	return err
}

// MarkOverCapacityWarn3dSent stamps the "3 days left" warning as sent. Guarded by
// `over_capacity_since IS NOT NULL` — see MarkOverCapacityStartNoticeSent's doc comment
// for why. Round 5 Gap Analysis fix (#1371).
func (r *householdRepository) MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE households SET over_capacity_warn3d_sent_at = $2 WHERE id = $1 AND over_capacity_since IS NOT NULL
	`, householdID, sentAt)
	return err
}

func (r *householdRepository) ListHouseholdsWithMembers(ctx context.Context) ([]*domain.Household, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, owner_user_id, name, created_at, updated_at, over_capacity_since, over_capacity_start_notice_sent_at, over_capacity_warn3d_sent_at
		FROM households h
		WHERE EXISTS (SELECT 1 FROM household_members hm WHERE hm.household_id = h.id AND hm.status = 'active')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var households []*domain.Household
	for rows.Next() {
		var h domain.Household
		if err := rows.Scan(householdScanArgs(&h)...); err != nil {
			return nil, err
		}
		households = append(households, &h)
	}
	return households, rows.Err()
}

func (r *householdRepository) GetRemovedMemberForUpdate(ctx context.Context, householdID, userID string, withinDays int) (*domain.HouseholdMember, error) {
	var m domain.HouseholdMember
	// ORDER BY + LIMIT 1: a (household_id, user_id) pair can have multiple
	// historical removed rows (removed rows aren't covered by the partial unique
	// active-user index), so without an explicit order this lookup would be
	// nondeterministic. Prefer the most recently removed row — the one a re-invite
	// within the 30-day window is meant to restore (Codex review, #1371 Task 1).
	// removed_at >= now() - withinDays enforces this method's own documented window
	// (Gap Analysis Round 3, GT-3) — a row removed longer ago than withinDays is treated
	// exactly like "no removed row exists at all" (ErrMemberNotFound), mirroring the
	// interval-parameterization idiom ListMembershipsPastPurgeWindow already uses below.
	err := r.db.QueryRow(ctx, `
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members WHERE household_id = $1 AND user_id = $2 AND status = 'removed' AND removed_at >= now() - $3::interval
		ORDER BY removed_at DESC, created_at DESC
		LIMIT 1
		FOR UPDATE
	`, householdID, userID, strings.TrimSpace(fmt.Sprintf("%d days", withinDays))).Scan(householdMemberScanArgs(&m)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *householdRepository) RestoreMember(ctx context.Context, membershipID string) (time.Time, error) {
	var updatedAt time.Time
	err := r.db.QueryRow(ctx, `
		UPDATE household_members SET status = 'active', removed_at = NULL, updated_at = now() WHERE id = $1 AND status = 'removed'
		RETURNING updated_at
	`, membershipID).Scan(&updatedAt)
	switch {
	case isUniqueViolation(err, constraintHouseholdMembersActive):
		// The restored user has since become active in another household (or
		// rejoined this one under a different row) — that index is global on
		// user_id, not scoped to household_id (migration 000107). Mirrors
		// InsertMember's mapping so Task 4's re-invite flow gets the same error
		// contract as every other write path (Codex review, #1371 Task 1).
		return time.Time{}, domain.ErrAlreadyInHousehold
	case isUniqueViolation(err, constraintHouseholdMembersOwnerOnce):
		return time.Time{}, domain.ErrAlreadyOwnsHousehold
	case errors.Is(err, pgx.ErrNoRows):
		return time.Time{}, domain.ErrMemberNotFound
	case err != nil:
		return time.Time{}, err
	}
	return updatedAt, nil
}

func (r *householdRepository) ListMembershipsPastPurgeWindow(ctx context.Context, purgeAfterDays int) ([]*domain.HouseholdMember, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, household_id, user_id, role, status, joined_at, removed_at, prior_subscription_tier, prior_subscription_status, created_at, updated_at
		FROM household_members
		WHERE status = 'removed' AND removed_at < now() - $1::interval
		ORDER BY removed_at ASC
	`, strings.TrimSpace(fmt.Sprintf("%d days", purgeAfterDays)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*domain.HouseholdMember
	for rows.Next() {
		var m domain.HouseholdMember
		if err := rows.Scan(householdMemberScanArgs(&m)...); err != nil {
			return nil, err
		}
		members = append(members, &m)
	}
	return members, rows.Err()
}

// isUniqueViolation reports whether err is a Postgres 23505 unique_violation on the
// named constraint/index. Used to translate constraint failures into domain sentinels
// so callers (and the error mapper) never see a raw pgconn.PgError.
func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}

// householdScanArgs returns the Scan() destination pointers for a single households
// row, in the exact column order shared by every household SELECT in this file
// (with or without the "h." join alias): id, owner_user_id, name, created_at,
// updated_at, over_capacity_since, over_capacity_start_notice_sent_at,
// over_capacity_warn3d_sent_at. Centralizing this avoids the 8-column Scan() list
// drifting between the four call sites that read a full households row.
// #1371 — Phase 7.
func householdScanArgs(h *domain.Household) []any {
	return []any{
		&h.ID, &h.OwnerUserID, &h.Name, &h.CreatedAt, &h.UpdatedAt,
		&h.OverCapacitySince, &h.OverCapacityStartNoticeSentAt, &h.OverCapacityWarn3dSentAt,
	}
}

// householdMemberScanArgs returns the Scan() destination pointers for a single
// household_members row, in the exact column order shared by every membership
// SELECT in this file: id, household_id, user_id, role, status, joined_at,
// removed_at, prior_subscription_tier, prior_subscription_status, created_at,
// updated_at. Centralizing this avoids the 11-column Scan() list drifting between
// the call sites that read a full household_members row. #1371 — Phase 7.
func householdMemberScanArgs(m *domain.HouseholdMember) []any {
	return []any{
		&m.ID, &m.HouseholdID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.RemovedAt,
		&m.PriorSubscriptionTier, &m.PriorSubscriptionStatus, &m.CreatedAt, &m.UpdatedAt,
	}
}
