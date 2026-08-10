package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ErrEntitlementNotFound indicates the user has no currently active (effective_until IS NULL)
// subscription_entitlements row — the normal case for solo users. Callers should treat this
// as "fall back to the user's own tier", not as a real error.
var ErrEntitlementNotFound = errors.New("active entitlement not found")

// EntitlementRepository reads and writes subscription_entitlements rows. Phase 2
// (#1366) shipped the read; Phase 3 (#1367) adds the write paths used by invite-accept
// and member removal.
type EntitlementRepository interface {
	// GetActiveEntitlement returns the user's currently open-ended entitlement row
	// (effective_until IS NULL, effective_from <= asOf). Returns ErrEntitlementNotFound
	// if none exists. The 000112 partial unique index (WHERE effective_until IS NULL)
	// guarantees at most one such row per user, so LIMIT 1 is deterministic.
	//
	// asOf only bounds effective_from, replacing the database's own NOW() so the sole
	// caller (HouseholdEntitlementService.ResolveEffectiveEntitlementAt) can pass an
	// instant it captured moments earlier rather than the query's transaction-start clock.
	// The effective_until IS NULL half of the WHERE clause is deliberately NOT bound by
	// asOf — it is a current-state predicate, evaluated at query time, not at asOf. That
	// means an entitlement that was open at asOf but has since been closed (effective_until
	// set between asOf being captured and this query running) is NOT returned; there is no
	// asOf-consistent "as it existed then" read of a closed epoch here. This is acceptable
	// for the one caller this has today: HouseholdEntitlementService resolves household
	// coverage live from the owner's current user row by design, so returning a historical
	// entitlement epoch while the rest of the resolution is live would itself be incoherent.
	// Do NOT change this to also match closed epochs within [effective_from, effective_until)
	// — 000112's uniqueness only holds for the open-ended row, so matching closed epochs too
	// makes LIMIT 1 pick an arbitrary one among however many closed epochs overlap asOf.
	GetActiveEntitlement(ctx context.Context, userID string, asOf time.Time) (*domain.SubscriptionEntitlement, error)

	// CreateHouseholdEntitlement inserts an open-ended (effective_until=NULL) entitlement
	// row for userID, source='household', payer=payerUserID (must equal the household's
	// owner_user_id — enforced by the 000112 composite FK). Returns
	// domain.ErrAlreadyInHousehold if the user already has an open entitlement
	// (uq_subscription_entitlements_active_user) — callers should call
	// ExpireActiveEntitlement defensively first.
	CreateHouseholdEntitlement(ctx context.Context, userID, tier, status, payerUserID, householdID string) error

	// ExpireActiveEntitlement sets effective_until=now() on the user's open-ended
	// entitlement row, if any. Idempotent — a no-op (nil error) when none exists.
	ExpireActiveEntitlement(ctx context.Context, userID string) error

	// ExpireActiveHouseholdEntitlement sets effective_until=now() on userID's open-ended
	// entitlement row, scoped to householdID AND source='household'. Unlike the broad
	// ExpireActiveEntitlement (user_id only), this cannot accidentally close out an
	// unrelated entitlement the user has since acquired (e.g. their own new subscription)
	// — see
	// TestEntitlementRepository_ExpireActiveHouseholdEntitlement_DoesNotTouchOwnSubscription.
	// Idempotent — a no-op (nil error) when no matching row exists. #1371 — Phase 7
	// foundation (PurgeRemovedMembership).
	ExpireActiveHouseholdEntitlement(ctx context.Context, userID, householdID string) error
}

type entitlementRepository struct {
	db DB
}

// NewEntitlementRepository creates a new EntitlementRepository instance.
func NewEntitlementRepository(db DB) EntitlementRepository {
	return &entitlementRepository{db: db}
}

func (r *entitlementRepository) GetActiveEntitlement(ctx context.Context, userID string, asOf time.Time) (*domain.SubscriptionEntitlement, error) {
	var e domain.SubscriptionEntitlement
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2
		LIMIT 1
	`, userID, asOf).Scan(
		&e.ID, &e.UserID, &e.Source, &e.Tier, &e.Status, &e.PayerUserID, &e.HouseholdID,
		&e.EffectiveFrom, &e.EffectiveUntil, &e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEntitlementNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *entitlementRepository) CreateHouseholdEntitlement(ctx context.Context, userID, tier, status, payerUserID, householdID string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, payer_user_id, household_id, effective_from, effective_until)
		VALUES ($1, 'household', $2, $3, $4, $5, now(), NULL)
	`, userID, tier, status, payerUserID, householdID)
	if isUniqueViolation(err, constraintEntitlementsActiveUser) {
		return domain.ErrAlreadyInHousehold
	}
	return err
}

func (r *entitlementRepository) ExpireActiveEntitlement(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND effective_until IS NULL
	`, userID)
	return err
}

func (r *entitlementRepository) ExpireActiveHouseholdEntitlement(ctx context.Context, userID, householdID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE subscription_entitlements SET effective_until = now(), updated_at = now()
		WHERE user_id = $1 AND household_id = $2 AND source = 'household' AND effective_until IS NULL
	`, userID, householdID)
	return err
}
