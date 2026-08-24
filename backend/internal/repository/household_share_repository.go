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

	// RevokeSharesForUser CLOSES the grant interval — sets revoked_at=now() on every open
	// share grant for userID's accounts within householdID. Called on member
	// removal/self-leave so a departed member's data stops appearing in the household
	// aggregate from that instant onward (GDPR revocability), and again by the 30-day
	// PurgeRemovedMembership job as an idempotent belt-and-braces close.
	//
	// #1569 T7: closing is the ONLY thing that ever happens to a grant row. There is no
	// delete counterpart any more — see PurgeRemovedMembership for the ruling and
	// householdAccountsCTEForBalancesAsOf for what reads the closed rows. Erasure of the
	// row itself belongs to GDPR alone, and reaches it by cascade from user_accounts.
	RevokeSharesForUser(ctx context.Context, householdID, userID string) error

	// SetShareVisibility makes `visibility` the CURRENT state of (householdID,
	// userAccountID), leaving exactly one open grant row (revoked_at IS NULL) behind.
	// Hiding an account keeps an open row (visibility='hidden', revoked_at=NULL) so a later
	// member removal still finds and revokes it. Race-safe under concurrent callers via
	// INSERT ... ON CONFLICT targeting the partial unique index
	// uq_household_account_shares_active. #1368 — Phase 4.
	//
	// #1569 T7: a visibility CHANGE now closes the outgoing state's interval and opens a new
	// one, instead of overwriting `visibility` on the single row. It used to overwrite, which
	// meant the row's one [shared_at, revoked_at) interval carried whatever the member last
	// chose — so hiding an account rewrote its entire history, and an as-of read for a date
	// when it demonstrably WAS in the household aggregate reported that it never had been.
	// Re-asserting the visibility a row already has is still a no-op update on that same row:
	// only a genuine state change closes an interval, so idempotent PATCHes cannot accumulate
	// zero-length ones. See householdAccountsCTEForBalancesAsOf for the read side.
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

// setShareVisibilityReturning is the column list every SetShareVisibility path returns, in
// domain.HouseholdAccountShare's scan order.
const setShareVisibilityReturning = `id, household_id, user_account_id, visibility, shared_at, revoked_at,
	          physical_account_blind_idx, created_at, updated_at`

func (r *householdShareRepository) SetShareVisibility(ctx context.Context, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error) {
	// Two statements, so they need a transaction: closing the outgoing interval without
	// opening the replacement would leave the account with NO open grant — silently absent
	// from the household aggregate, and invisible to RevokeSharesForUser at removal time.
	// db.Begin() opens a SAVEPOINT when r.db is already a pgx.Tx (the production path —
	// SetAccountShareVisibility runs this inside its own membership-locked transaction), so
	// this composes rather than nesting. Same idiom as budgetRepository.CreateWithCategories.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Close the open interval ONLY if it is a different state. `visibility <> $3` is what
	// keeps a repeated no-op PATCH from churning out zero-length intervals, and it also takes
	// the row lock that serializes concurrent togglers on this pair.
	if _, err = tx.Exec(ctx, `
		UPDATE household_account_shares SET revoked_at = now(), updated_at = now()
		WHERE household_id = $1 AND user_account_id = $2
		  AND revoked_at IS NULL AND visibility <> $3
	`, householdID, userAccountID, visibility); err != nil {
		return nil, err
	}

	// Open the new interval. ON CONFLICT is kept for a race that is NOT reachable today: a
	// contender blocked on the UPDATE above would wake with a snapshot predating our INSERT,
	// close nothing, and land here, where DO UPDATE would overwrite an interval instead of
	// closing it — the one path on which a visibility state could leave no trace. The only
	// non-test caller is SetAccountShareVisibility, which holds LockHouseholdByID for the whole
	// transaction, so concurrent toggles within a household are fully serialized and different
	// households cannot collide on this index at all. The residual is a FUTURE caller reaching
	// this method without that lock; ON CONFLICT keeps that case from surfacing a 23505 to
	// someone toggling a switch twice, at the cost of one microsecond-lived interval.
	var s domain.HouseholdAccountShare
	err = tx.QueryRow(ctx, `
		INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (household_id, user_account_id) WHERE revoked_at IS NULL
		DO UPDATE SET visibility = EXCLUDED.visibility,
		              physical_account_blind_idx = EXCLUDED.physical_account_blind_idx,
		              updated_at = now()
		RETURNING `+setShareVisibilityReturning+`
	`, householdID, userAccountID, physicalBlindIdx, visibility).Scan(
		&s.ID, &s.HouseholdID, &s.UserAccountID, &s.Visibility, &s.SharedAt, &s.RevokedAt,
		&s.PhysicalAccountBlindIdx, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
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
