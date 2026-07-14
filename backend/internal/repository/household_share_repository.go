package repository

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AccountFingerprint is one of the accepting user's own user_accounts, projected for
// the accept-transaction default-share fan-out. PhysicalBlindIdx is the concatenation
// of institution_id_blind_idx + mask_blind_idx (nil when the account has no mask) —
// this concat is a cross-phase contract with the Phase 5 (#1369) joint-account dedup
// and must not change without updating that phase too.
type AccountFingerprint struct {
	UserAccountID    string
	PhysicalBlindIdx *string
}

// HouseholdShareRepository manages household_account_shares and household_share_consents
// rows, plus the in-transaction read of the accepting user's own accounts used to build
// the default-share fan-out. Issue #1367 — Phase 3.
type HouseholdShareRepository interface {
	// ListActiveUserAccountFingerprints returns every user_accounts row owned by userID,
	// projected to (id, physical fingerprint). This is always scoped to the caller's own
	// accounts — callers must never pass a foreign user ID here (see accept-tx IDOR guard).
	ListActiveUserAccountFingerprints(ctx context.Context, userID string) ([]AccountFingerprint, error)

	// InsertShare inserts one household_account_shares grant.
	InsertShare(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) error

	// InsertConsent inserts an append-only household_share_consents row, populating
	// c.ID/ConsentedAt/CreatedAt.
	InsertConsent(ctx context.Context, c *domain.HouseholdShareConsent) error

	// RevokeSharesForUser sets revoked_at=now() on every active share grant for userID's
	// accounts within householdID. Called on member removal/self-leave so a departed
	// member's data stops appearing in the household aggregate (GDPR revocability).
	RevokeSharesForUser(ctx context.Context, householdID, userID string) error

	// DeleteSharesForUser permanently deletes (hard delete, not revoke) every
	// household_account_shares row for userID's accounts within householdID. Unlike
	// RevokeSharesForUser (soft-revoke at removal time), this runs at purge time — 30
	// days after removal — via PurgeRemovedMembership, once the share grants no longer
	// need to be retained. #1371 — Phase 7 foundation.
	DeleteSharesForUser(ctx context.Context, householdID, userID string) error

	// SetShareVisibility upserts the single active grant row (revoked_at IS NULL) for
	// (householdID, userAccountID) to the given visibility. It NEVER writes revoked_at —
	// revocation is reserved for RevokeSharesForUser (member removal). Hiding an account
	// keeps its row active (visibility='hidden', revoked_at=NULL) so a later member
	// removal still finds and revokes it. Race-safe under concurrent callers via
	// INSERT ... ON CONFLICT targeting the partial unique index
	// uq_household_account_shares_active. #1368 — Phase 4.
	SetShareVisibility(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error)

	// ListMyAccountShares returns every user_accounts row owned by userID, left-joined to
	// its current active grant (if any) within householdID. A nil Visibility means the
	// account has no explicit grant yet (e.g. linked after the member joined) — callers
	// must not assume a default. #1368 — Phase 4.
	ListMyAccountShares(ctx context.Context, householdID, userID string) ([]domain.UserAccountShareView, error)
}

type householdShareRepository struct {
	db DB
}

// NewHouseholdShareRepository creates a new HouseholdShareRepository instance.
func NewHouseholdShareRepository(db DB) HouseholdShareRepository {
	return &householdShareRepository{db: db}
}

func (r *householdShareRepository) ListActiveUserAccountFingerprints(ctx context.Context, userID string) ([]AccountFingerprint, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, institution_id_blind_idx, mask_blind_idx FROM user_accounts WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fps []AccountFingerprint
	for rows.Next() {
		var fp AccountFingerprint
		var instIdx string
		var maskIdx *string
		if err := rows.Scan(&fp.UserAccountID, &instIdx, &maskIdx); err != nil {
			return nil, err
		}
		if maskIdx != nil {
			combined := instIdx + *maskIdx
			fp.PhysicalBlindIdx = &combined
		}
		fps = append(fps, fp)
	}
	return fps, rows.Err()
}

func (r *householdShareRepository) InsertShare(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
	`, householdID, userAccountID, physicalBlindIdx, visibility)
	return err
}

func (r *householdShareRepository) InsertConsent(ctx context.Context, c *domain.HouseholdShareConsent) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO household_share_consents (household_id, user_id, consent_version, snapshot)
		VALUES ($1, $2, $3, $4)
		RETURNING id, consented_at, created_at
	`, c.HouseholdID, c.UserID, c.ConsentVersion, []byte(c.Snapshot)).Scan(&c.ID, &c.ConsentedAt, &c.CreatedAt)
}

func (r *householdShareRepository) RevokeSharesForUser(ctx context.Context, householdID, userID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE household_account_shares SET revoked_at = now(), updated_at = now()
		WHERE household_id = $1 AND revoked_at IS NULL
		  AND user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $2)
	`, householdID, userID)
	return err
}

func (r *householdShareRepository) DeleteSharesForUser(ctx context.Context, householdID, userID string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM household_account_shares
		WHERE household_id = $1
		  AND user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $2)
	`, householdID, userID)
	return err
}

func (r *householdShareRepository) SetShareVisibility(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error) {
	var s domain.HouseholdAccountShare
	err := r.db.QueryRow(ctx, `
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (household_id, user_account_id) WHERE revoked_at IS NULL
		DO UPDATE SET visibility = EXCLUDED.visibility,
		              physical_account_blind_idx = EXCLUDED.physical_account_blind_idx,
		              updated_at = now()
		RETURNING id, household_id, user_account_id, visibility, shared_at, revoked_at,
		          physical_account_blind_idx, created_at, updated_at
	`, householdID, userAccountID, physicalBlindIdx, visibility).Scan(
		&s.ID, &s.HouseholdID, &s.UserAccountID, &s.Visibility, &s.SharedAt, &s.RevokedAt,
		&s.PhysicalAccountBlindIdx, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *householdShareRepository) ListMyAccountShares(ctx context.Context, householdID, userID string) ([]domain.UserAccountShareView, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ua.id AS user_account_id, has.visibility, has.revoked_at
		FROM user_accounts ua
		LEFT JOIN household_account_shares has
			ON has.user_account_id = ua.id AND has.household_id = $1 AND has.revoked_at IS NULL
		WHERE ua.user_id = $2
		ORDER BY ua.id
	`, householdID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []domain.UserAccountShareView
	for rows.Next() {
		var v domain.UserAccountShareView
		var visibility *string
		if err := rows.Scan(&v.UserAccountID, &visibility, &v.RevokedAt); err != nil {
			return nil, err
		}
		if visibility != nil {
			sv := domain.ShareVisibility(*visibility)
			v.Visibility = &sv
		}
		views = append(views, v)
	}
	return views, rows.Err()
}
