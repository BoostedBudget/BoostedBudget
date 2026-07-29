package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AdminUserRepository defines the interface for admin user data access
type AdminUserRepository interface {
	// ListUsers returns a paginated list of users with counts for admin view
	ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error)

	// GetUserDetail returns detailed user information with full statistics
	GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error)

	// UpdateUserTier manually overrides a user's subscription tier, with an optional expiration,
	// and writes an audit log row — all inside a single transaction.
	//
	// actorUserID is the acting admin's internal UUID (audit_logs.user_id; may be "" — coerced
	// to SQL NULL, mirroring RemoveUserTierOverride). userID is the target being modified.
	//
	// Returns the target's decrypted clerk_user_id plus its raw (base) subscription_tier/
	// subscription_status — unaffected by this write — so the caller can build the correct
	// EFFECTIVE Clerk metadata payload without a second DB read.
	UpdateUserTier(ctx context.Context, actorUserID, userID, tier, reason string, expiresAt *time.Time) (UpdateTierRow, error)

	// GetOwnedHouseholdActiveMemberCount returns the number of active members in the household
	// owned by userID (0 if userID does not own a household). Used by the service to warn an
	// admin when a tier override would drop a household owner below their current active
	// member count (Issue #1413 Defect 5).
	GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error)

	// RemoveUserTierOverride clears a user's manual tier override (manual_tier_override,
	// manual_tier_ends_at), re-arms the retention clock if the reverted subscription status
	// is read-only, and writes an audit log row — all inside a single transaction.
	//
	// actorUserID is the acting admin's internal UUID (may be "" — coerced to SQL NULL).
	// targetUserID is the user being modified. retentionMonths sizes the re-armed retention
	// window when applicable.
	//
	// Returns ErrUserNotFound if targetUserID does not exist or is soft-deleted.
	RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (RemoveTierOverrideRow, error)

	// RemoveUserTierOverrideIfMatches is the CAS-guarded sibling of RemoveUserTierOverride,
	// used by the daily manual-tier-override-expiry worker (Issue #1413 High finding H4): it
	// only clears the override if manual_tier_override/manual_tier_ends_at still equal
	// expectedOverride/expectedEndsAt (the values the worker observed when it selected this
	// user as an expiry candidate). Returns claimed=false (never an error) if the row no longer
	// matches — e.g. an admin extended or replaced the override between the worker's select and
	// this call — so the worker's stale decision cannot clobber a fresh admin action.
	//
	// The admin-initiated RemoveUserTierOverride above is unaffected: it keeps its unconditional
	// behaviour and signature.
	RemoveUserTierOverrideIfMatches(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int, expectedOverride *string, expectedEndsAt *time.Time) (row RemoveTierOverrideRow, claimed bool, err error)
}

// UpdateTierRow is the result of setting a user's manual tier override in a single
// UPDATE ... RETURNING statement: the target's decrypted Clerk user ID, its raw (base)
// subscription_tier/subscription_status (unaffected by this write — these plus the
// just-applied override are what the caller needs to build the correct EFFECTIVE Clerk
// metadata payload without a second DB read), and the prior manual_tier_override value (for
// the audit log).
type UpdateTierRow struct {
	// ClerkUserID is the decrypted clerk_user_id, for the caller's Clerk metadata sync.
	ClerkUserID string
	// SubscriptionTier is the user's raw (base) subscription_tier, unaffected by this write.
	SubscriptionTier string
	// SubscriptionStatus is the user's raw (base) subscription_status, unaffected by this write.
	SubscriptionStatus string
	// PriorManualTierOverride is the manual_tier_override value as it was immediately before
	// this UPDATE (nil if no override was previously set).
	PriorManualTierOverride *string
}

// RemoveTierOverrideRow is the result of clearing a user's manual tier override in a single
// UPDATE ... RETURNING statement: the prior override value (read from the same pre-update
// snapshot the CTE locked via FOR UPDATE) plus the reverted subscription state.
//
// Removed is computed here in Go — non-nil AND non-empty prior override — deliberately NOT
// reusing domain.User.HasActiveManualOverride()'s expiry check: a present-but-expired override
// still counts as "removed" because the columns are cleared either way.
type RemoveTierOverrideRow struct {
	// PriorManualTierOverride is the manual_tier_override value as it was immediately before
	// this UPDATE (nil if no override was set).
	PriorManualTierOverride *string
	// Removed is true iff PriorManualTierOverride was non-nil and non-empty.
	Removed bool
	// RevertedTier is the user's base subscription_tier (unaffected by this UPDATE — this is
	// what becomes "effective" now that the override is cleared).
	RevertedTier string
	// RevertedStatus is the user's raw subscription_status (unaffected by this UPDATE).
	RevertedStatus string
	// ClerkUserID is the decrypted clerk_user_id, for the caller's Clerk metadata sync.
	ClerkUserID string
}

// adminUserRepository implements AdminUserRepository using PostgreSQL
type adminUserRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewAdminUserRepository creates a new AdminUserRepository instance
func NewAdminUserRepository(db DB, enc *appcrypto.FieldEncryptor) AdminUserRepository {
	return &adminUserRepository{db: db, enc: enc}
}

// usersOrderByAllowlist maps frontend sort_by values to safe SQL expressions.
// The u. prefix is required because of the LEFT JOINs in the query.
// NULLS LAST is applied to nullable columns so nulls always sort to the bottom
// regardless of direction (must appear AFTER the sort direction in PostgreSQL).
var usersOrderByAllowlist = map[string]orderByEntry{
	"created_at":          {"u.created_at", false},
	"last_login_at":       {"u.last_login_at", true},
	"subscription_tier":   {"u.subscription_tier", false},
	"subscription_status": {"u.subscription_status", false},
}

// ListUsers returns a paginated list of users with aggregated counts
// Supports filtering by email search and subscription tier
func (r *adminUserRepository) ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error) {
	// Build WHERE clause and args once to avoid duplication
	whereClause, args, argIndex := r.buildFilterClause(filters)

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE%s", whereClause)
	var totalCount int
	err := r.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	// Resolve ORDER BY from allowlist; fallback to created_at if not provided or invalid
	entry := usersOrderByAllowlist[filters.SortBy]
	if entry.col == "" {
		entry = orderByEntry{"u.created_at", false} // fallback (Huma enum prevents this in normal operation)
	}
	orderDir := strings.ToUpper(filters.SortDir)
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}
	nullsClause := ""
	if entry.nullsLast {
		nullsClause = " NULLS LAST"
	}

	// Main query with counts via subqueries
	query := fmt.Sprintf(`
		SELECT u.id, u.email, u.subscription_tier, u.subscription_status,
		       u.manual_tier_override, u.manual_tier_ends_at,
		       u.deletion_requested_at, u.deletion_scheduled_at, u.primary_region,
		       u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as boosts_count,
		       COALESCE(ref.count, 0) as referrals_count
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM linked_items
			WHERE status = 'active'
			GROUP BY user_id
		) la ON la.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM boosts
			GROUP BY user_id
		) r ON r.user_id = u.id
		LEFT JOIN (
			SELECT referrer_id, COUNT(*) as count
			FROM referrals
			WHERE status = 'completed'
			GROUP BY referrer_id
		) ref ON ref.referrer_id = u.id
		WHERE u.is_deleted = FALSE%s
		ORDER BY %s %s%s LIMIT $%d OFFSET $%d`, whereClause, entry.col, orderDir, nullsClause, argIndex, argIndex+1)
	args = append(args, limit, offset)

	// Execute main query
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []domain.AdminUserListItem
	for rows.Next() {
		var user domain.AdminUserListItem
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.SubscriptionTier,
			&user.SubscriptionStatus,
			&user.ManualTierOverride,
			&user.ManualTierEndsAt,
			&user.DeletionRequestedAt,
			&user.DeletionScheduledAt,
			&user.PrimaryRegion,
			&user.CreatedAt,
			&user.LastLoginAt,
			&user.IsActive,
			&user.LinkedAccountsCount,
			&user.BoostsCount,
			&user.ReferralsCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan user: %w", err)
		}

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iteration error: %w", err)
	}

	return users, totalCount, nil
}

// GetUserDetail returns detailed user information with full statistics
func (r *adminUserRepository) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	query := `
		SELECT u.id, u.email, u.subscription_tier, u.subscription_status,
		       u.manual_tier_override, u.manual_tier_ends_at,
		       u.deletion_requested_at, u.deletion_scheduled_at, u.primary_region,
		       u.created_at, u.last_login_at, u.is_active,
		       COALESCE(la.count, 0) as linked_accounts_count,
		       COALESCE(r.count, 0) as boosts_count,
		       COALESCE(ref.count, 0) as referrals_count,
		       COALESCE(t.count, 0) as transactions_count,
		       COALESCE(b.count, 0) as budgets_count
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM linked_items
			WHERE status = 'active'
			GROUP BY user_id
		) la ON la.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM boosts
			GROUP BY user_id
		) r ON r.user_id = u.id
		LEFT JOIN (
			SELECT referrer_id, COUNT(*) as count
			FROM referrals
			WHERE status = 'completed'
			GROUP BY referrer_id
		) ref ON ref.referrer_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM transactions
			GROUP BY user_id
		) t ON t.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) as count
			FROM budgets
			GROUP BY user_id
		) b ON b.user_id = u.id
		WHERE u.id = $1 AND u.is_deleted = FALSE
	`

	var detail domain.AdminUserDetail
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&detail.ID,
		&detail.Email,
		&detail.SubscriptionTier,
		&detail.SubscriptionStatus,
		&detail.ManualTierOverride,
		&detail.ManualTierEndsAt,
		&detail.DeletionRequestedAt,
		&detail.DeletionScheduledAt,
		&detail.PrimaryRegion,
		&detail.CreatedAt,
		&detail.LastLoginAt,
		&detail.IsActive,
		&detail.LinkedAccountsCount,
		&detail.BoostsCount,
		&detail.ReferralsCount,
		&detail.TransactionsCount,
		&detail.BudgetsCount,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user detail: %w", err)
	}

	decryptedEmail, err := r.enc.DecryptString(detail.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	detail.Email = decryptedEmail

	return &detail, nil
}

// updateUserTierQuery sets a user's manual tier override and, in the same statement, locks
// and reads the PRE-UPDATE manual_tier_override (via the "prior" CTE) so the caller can audit
// the previous value without a second round trip. Mirrors removeUserTierOverrideQuery's
// CTE + FOR UPDATE shape. The "prior" CTE's own is_deleted=FALSE guard is what causes a
// missing/soft-deleted target to return zero rows from the join below (surfaced as
// pgx.ErrNoRows -> ErrUserNotFound), so the outer UPDATE does not need to repeat that guard.
//
// plaid_cleanup_pending_at is deliberately left UNTOUCHED here — Issue #1413 round-13 (the P0
// this comment replaces round-10's reasoning for). Round-10 through round-12 cleared this column
// unconditionally on every grant, reasoning that a live comp makes the user Plaid-eligible again
// (HasActiveManualOverride() forces EFFECTIVE status to active). That reasoning is true of
// EFFECTIVE eligibility, but the marker (per domain.User.PlaidCleanupPendingAt's doc comment) no
// longer records effective eligibility — it records a durable, write-time fact: "an ineligibility
// EPISODE (raw subscription_status transition) began, cleanup outstanding". Granting a comp never
// touches raw subscription_status, so it must not touch this column either. The bug the old
// clear caused: comp an already inactive/cancelled user (a plausible real action — comping a
// churned account) — the marker (correctly set by the earlier cancellation) was erased on grant;
// if the comp later lapses and the daily comp-expiry job degrades or an admin never clicks
// "Remove Override", NOTHING ever re-stamps the marker, because a comp lapsing is a read-time
// fact (HasActiveManualOverride() vs. wall-clock time) with no write of its own. The user's live
// Plaid connections then stay connected — and billable — forever, invisible to
// GetUsersNeedingPlaidDisconnectRepair, which requires a non-NULL marker. Leaving the column
// untouched here means: if the user was already ineligible with an outstanding marker before this
// grant, that marker (and its original episode-start timestamp) survives the comp's entire
// lifetime, live or lapsed — CURRENT eligibility is evaluated separately, at query time, by that
// query's own noActiveManualOverridePredicate, which still correctly exempts a LIVE comp. If the
// user was NOT already ineligible (raw status is 'active'/'trial'), no marker exists to touch in
// the first place, so this is a genuine no-op — nothing to clear, nothing to preserve.
const updateUserTierQuery = `
	WITH prior AS (
		SELECT manual_tier_override
		FROM users
		WHERE id = $3 AND is_deleted = FALSE
		FOR UPDATE
	)
	UPDATE users u
	SET manual_tier_override = $1, manual_tier_ends_at = $2, updated_at = NOW()
	FROM prior
	WHERE u.id = $3
	RETURNING u.clerk_user_id, u.subscription_tier, u.subscription_status, prior.manual_tier_override
`

// UpdateUserTier sets a user's manual tier override, restarting the audit trail with the
// ACTING ADMIN (not the target) recorded as the audit actor, and returns the target's raw
// subscription state so the caller can push the correct EFFECTIVE tier to Clerk (Issue #1413
// Defects 1 and 2). See the AdminUserRepository interface doc for the full contract.
func (r *adminUserRepository) UpdateUserTier(ctx context.Context, actorUserID, userID, tier, reason string, expiresAt *time.Time) (UpdateTierRow, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var row UpdateTierRow
	var clerkUserIDEnc string
	err = tx.QueryRow(ctx, updateUserTierQuery, tier, expiresAt, userID).Scan(
		&clerkUserIDEnc, &row.SubscriptionTier, &row.SubscriptionStatus, &row.PriorManualTierOverride,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateTierRow{}, ErrUserNotFound
		}
		return UpdateTierRow{}, fmt.Errorf("failed to update user tier: %w", err)
	}

	// Decrypt the returned clerk_user_id (it is encrypted in the DB)
	row.ClerkUserID, err = r.enc.DecryptString(clerkUserIDEnc)
	if err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}

	// audit_logs.user_id -> the ACTING ADMIN; resource_id -> the TARGET (userID). Coerce an
	// empty actor to SQL NULL — the column is nullable with FK ON DELETE SET NULL, and a
	// missing actor must never abort a legitimate admin action (mirrors
	// RemoveUserTierOverride).
	var actorParam interface{}
	if actorUserID != "" {
		actorParam = actorUserID
	}

	detailsMap := map[string]interface{}{
		"tier":           tier,
		"reason":         reason,
		"target_user_id": userID,
	}
	if row.PriorManualTierOverride != nil {
		detailsMap["prior_manual_tier_override"] = *row.PriorManualTierOverride
	}
	if expiresAt != nil {
		detailsMap["expires_at"] = expiresAt.Format("2006-01-02T15:04:05Z")
	}
	detailsBytes, err := json.Marshal(detailsMap)
	if err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to marshal audit details: %w", err)
	}

	encryptedChanges, err := r.enc.EncryptJSON(json.RawMessage(detailsBytes))
	if err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to encrypt audit changes: %w", err)
	}

	auditQuery := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'update_manual_tier', 'user', $2, $3)
	`
	_, err = tx.Exec(ctx, auditQuery, actorParam, userID, encryptedChanges)
	if err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to create audit log: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return UpdateTierRow{}, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return row, nil
}

// GetOwnedHouseholdActiveMemberCount returns the number of active members in the household
// owned by userID. A user who does not own a household always yields 0 — ownership implies
// membership (the owner is always inserted as an active member at household creation), so 0
// unambiguously means "not an owner" to callers.
func (r *adminUserRepository) GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM household_members hm
		JOIN households h ON h.id = hm.household_id
		WHERE h.owner_user_id = $1 AND hm.status = 'active'
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count owned household active members: %w", err)
	}
	return count, nil
}

// removeUserTierOverrideSelectPrior is the shared "prior" CTE body for both the unconditional
// admin-initiated removal (removeUserTierOverrideQuery) and the CAS-guarded expiry-worker
// removal (removeUserTierOverrideIfMatchesQuery, Issue #1413 High finding H4). It locks
// (FOR UPDATE) and reads the pre-UPDATE snapshot needed for three purposes: (1)
// PriorManualTierOverride, so the caller can tell whether this call actually removed anything,
// (2) should_rearm_retention, the single named boolean the retention CASEs below all key off —
// see its own comment for why it looks the way it does, and (3), for the CAS variant only, the
// override values the caller can still condition the WHERE clause on.
//
// should_rearm_retention combines the H3 and H8 fixes (Issue #1413 High findings, Codex review)
// into ONE condition, per the review's own note that they interact:
//   - H3: retention must be (re-)armed whenever a prior override existed AND the reverted raw
//     status is read-only — including INITIALISING it when it was never running before (NULL),
//     not only re-arming an already-running clock. Before this fix, a user who was comped
//     before ever going read-only, then had their subscription cancelled while comped (the
//     cancellation webhook skips SetRetentionDates for a comped user), then had the comp
//     removed, would see the CASE's old `retention_expires_at IS NOT NULL` guard fail and the
//     clock would stay NULL forever — read-only with no deletion warning ever sent.
//   - H8: a "clear" call where NO override actually existed (a no-op — Removed=false) must be
//     completely side-effect-free on retention. Before this fix, the CASE never checked whether
//     an override had existed at all, so a stale/repeated/double-clicked clear on an
//     already-cancelled, no-override user would silently reset retention_expires_at to
//     now+retentionMonths and null both notice flags every time — indefinitely postponing
//     deletion and suppressing warnings.
//
// If the reverted status is NOT read-only (the user resubscribed while comped), or no prior
// override existed, retention is left completely untouched — the normal webhook path (or
// nothing, for the no-op case) is responsible for it.
// #1413 PinnedRaw (G8): should_rearm_retention deliberately excludes 'trial' from the
// read-only-status list, and the UPDATE below (removeUserTierOverrideUpdate) never touches
// subscription_status at all — so removing a comp from a 'trial' user whose trial_ends_at has
// already passed leaves them at raw 'trial', not normalized to 'inactive'. Do NOT "fix" this by
// normalizing status here: GetUsersWithExpiredTrials selects on raw subscription_status =
// 'trial', so a silent normalization would make TrialService.ExpireTrial's job never reselect
// this user again, and its Plaid disconnect + retention-clock side effects would never run. See
// internal/services/trial_expiry_comp_revert_integration_test.go for the pin and full reasoning.
// should_mark_plaid_cleanup_pending (Issue #1413 round-10) is should_rearm_retention's narrower
// sibling for the Plaid-disconnect-repair marker: a removed/expired comp (re-)starts
// plaid_cleanup_pending_at when the reverted raw status is one the repair pass actually targets,
// via COALESCE(u.plaid_cleanup_pending_at, NOW()) below. Deliberately narrower than
// should_rearm_retention's ('inactive', 'cancelled', 'past_due') — 'past_due' is excluded here for
// the SAME reason plaidDisconnectRepairEligibleStatusPredicate excludes it: past_due has its own
// dunning timeline (past_due_service.go, PastDueConfig.PlaidDisconnectDays) and must never be
// immediately Plaid-disconnected by this repair pass. A comp removed while the reverted status is
// 'past_due' leaves plaid_cleanup_pending_at untouched — it should already be NULL (past_due is
// never a state that sets this marker), and past_due's own dunning flow owns that user's Plaid
// fate instead.
//
// Issue #1413 round-13: as of round-13, updateUserTierQuery (comp grant) no longer nulls this
// marker, so in the ordinary case — a comp granted over an already inactive/cancelled user whose
// marker was already set by the transition that made them ineligible — this COALESCE is a no-op:
// the marker already survived the comp's entire lifetime untouched, and COALESCE(existing, NOW())
// just preserves that existing (earlier, correct) value. This CASE now exists purely as a
// defense-in-depth backstop for a row that reached an ineligible raw status WITHOUT ever passing
// through one of the SET-site writers (e.g. an imported/backfilled row created directly with
// subscription_status = 'inactive'/'cancelled') — the one case where the marker could still be
// legitimately NULL despite the status already being repair-eligible.
const removeUserTierOverrideSelectPrior = `
	SELECT manual_tier_override, subscription_status, retention_expires_at,
	       (manual_tier_override IS NOT NULL AND manual_tier_override <> ''
	        AND subscription_status IN ('inactive', 'cancelled', 'past_due')) AS should_rearm_retention,
	       (manual_tier_override IS NOT NULL AND manual_tier_override <> ''
	        AND subscription_status IN ('inactive', 'cancelled')) AS should_mark_plaid_cleanup_pending
	FROM users
	WHERE id = $1 AND is_deleted = FALSE
`

// removeUserTierOverrideCASClause restricts the CAS-guarded variant (H4) to rows whose current
// manual_tier_override/manual_tier_ends_at still match what the caller last observed. Uses
// IS NOT DISTINCT FROM rather than = because both columns are nullable and SQL's = is never
// true when either side is NULL (e.g. a permanent override has NULL manual_tier_ends_at).
const removeUserTierOverrideCASClause = `
	  AND manual_tier_override IS NOT DISTINCT FROM $3
	  AND manual_tier_ends_at IS NOT DISTINCT FROM $4
`

// removeUserTierOverrideUpdate is the shared UPDATE body for both variants: clear the override
// columns, and conditionally re-arm/initialise retention per should_rearm_retention (see
// removeUserTierOverrideSelectPrior's comment).
const removeUserTierOverrideUpdate = `
	UPDATE users u
	SET manual_tier_override = NULL,
	    manual_tier_ends_at = NULL,
	    updated_at = NOW(),
	    retention_started_at = CASE WHEN prior.should_rearm_retention THEN NOW() ELSE u.retention_started_at END,
	    retention_expires_at = CASE WHEN prior.should_rearm_retention THEN NOW() + make_interval(months => $2::int) ELSE u.retention_expires_at END,
	    retention_notice_30d_sent_at = CASE WHEN prior.should_rearm_retention THEN NULL ELSE u.retention_notice_30d_sent_at END,
	    retention_notice_7d_sent_at = CASE WHEN prior.should_rearm_retention THEN NULL ELSE u.retention_notice_7d_sent_at END,
	    plaid_cleanup_pending_at = CASE WHEN prior.should_mark_plaid_cleanup_pending THEN COALESCE(u.plaid_cleanup_pending_at, NOW()) ELSE u.plaid_cleanup_pending_at END
	FROM prior
	WHERE u.id = $1
	RETURNING u.subscription_tier, u.subscription_status, u.clerk_user_id, prior.manual_tier_override
`

// removeUserTierOverrideQuery is the unconditional, admin-initiated removal: clicking "Remove
// Override" means "clear whatever is there right now", regardless of what the admin last saw.
const removeUserTierOverrideQuery = `
	WITH prior AS (
	` + removeUserTierOverrideSelectPrior + `
	FOR UPDATE
	)
	` + removeUserTierOverrideUpdate

// removeUserTierOverrideIfMatchesQuery is the CAS-guarded removal used by the manual tier
// override expiry worker (Issue #1413 High finding H4, Codex review): the worker selects expired
// candidates via GetUsersWithExpiredManualTierOverride, then calls
// RemoveUserTierOverrideIfMatches with the override values it observed at selection time. If an
// admin has since extended or replaced the override, the CAS clause below fails to match, the
// "prior" CTE returns zero rows, and the clear does not happen — the worker's stale decision
// never overwrites the admin's fresh one.
const removeUserTierOverrideIfMatchesQuery = `
	WITH prior AS (
	` + removeUserTierOverrideSelectPrior + removeUserTierOverrideCASClause + `
	FOR UPDATE
	)
	` + removeUserTierOverrideUpdate

// removeTierOverride is the shared transaction body for both RemoveUserTierOverride
// (unconditional, admin path) and RemoveUserTierOverrideIfMatches (CAS-guarded, expiry-worker
// path — Issue #1413 High finding H4): begin a transaction, run the given query, decrypt
// clerk_user_id, compute Removed, and write the audit row.
//
// Zero rows returned by the query (pgx.ErrNoRows) is reported as claimed=false with a NIL
// error — never translated to ErrUserNotFound here — because for the CAS variant that outcome
// is the expected, non-error "someone changed the override under us" case (Issue #1413's
// claim-then-act pattern: this is a legitimate skip, not a failure). It is only the caller
// (RemoveUserTierOverride, for the unconditional query which has no CAS clause and therefore
// no way to return zero rows except a missing/soft-deleted target) that translates
// claimed=false into ErrUserNotFound.
func (r *adminUserRepository) removeTierOverride(
	ctx context.Context, query string, args []interface{}, actorUserID, targetUserID, reason string,
) (RemoveTierOverrideRow, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var row RemoveTierOverrideRow
	var clerkUserIDEnc string
	err = tx.QueryRow(ctx, query, args...).Scan(
		&row.RevertedTier, &row.RevertedStatus, &clerkUserIDEnc, &row.PriorManualTierOverride,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RemoveTierOverrideRow{}, false, nil
		}
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to remove user tier override: %w", err)
	}
	row.Removed = row.PriorManualTierOverride != nil && *row.PriorManualTierOverride != ""

	// Decrypt the returned clerk_user_id (it is encrypted in the DB)
	row.ClerkUserID, err = r.enc.DecryptString(clerkUserIDEnc)
	if err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}

	// Audit row inside the same transaction — even when Removed is false (idempotent no-op),
	// this call is the natural repair hook for metadata #1413 may have corrupted, so it always
	// audits and (in the service layer) always re-syncs Clerk.
	// actor_user_id -> audit_logs.user_id (acting admin); targetUserID -> resource_id.
	// If actorUserID is empty, leave as nil -> SQL NULL (column is nullable with FK ON DELETE
	// SET NULL; a missing actor must never abort a legitimate admin action).
	var actorParam interface{}
	if actorUserID != "" {
		actorParam = actorUserID
	}

	detailsMap := map[string]interface{}{
		"reason": reason,
	}
	if row.PriorManualTierOverride != nil {
		detailsMap["prior_manual_tier_override"] = *row.PriorManualTierOverride
	}
	detailsBytes, err := json.Marshal(detailsMap)
	if err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to marshal audit details: %w", err)
	}

	encryptedChanges, err := r.enc.EncryptJSON(json.RawMessage(detailsBytes))
	if err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to encrypt audit changes: %w", err)
	}

	auditQuery := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'remove_manual_tier', 'user', $2, $3)
	`
	_, err = tx.Exec(ctx, auditQuery, actorParam, targetUserID, encryptedChanges)
	if err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to create audit log: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return RemoveTierOverrideRow{}, false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return row, true, nil
}

// RemoveUserTierOverride clears a user's manual tier override, re-arms/initialises the
// retention clock when a prior override existed and the reverted status is read-only, and
// writes an audit log row — in one transaction. Unconditional: an admin clicking "Remove
// Override" always clears whatever is currently there. See the AdminUserRepository interface
// doc for the full contract.
func (r *adminUserRepository) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (RemoveTierOverrideRow, error) {
	// This query has no CAS clause, so zero rows can only mean the target is missing or
	// soft-deleted — surfaced as ErrUserNotFound (the same sentinel UpdateUserTier uses).
	row, claimed, err := r.removeTierOverride(ctx, removeUserTierOverrideQuery,
		[]interface{}{targetUserID, retentionMonths}, actorUserID, targetUserID, reason)
	if err != nil {
		return RemoveTierOverrideRow{}, err
	}
	if !claimed {
		return RemoveTierOverrideRow{}, ErrUserNotFound
	}
	return row, nil
}

// RemoveUserTierOverrideIfMatches clears a user's manual tier override ONLY IF its current
// manual_tier_override and manual_tier_ends_at still match expectedOverride/expectedEndsAt —
// the values the caller observed when it decided to remove the override (Issue #1413 High
// finding H4, Codex review). Used by the daily manual-tier-override-expiry job: it selects
// expired candidates via GetUsersWithExpiredManualTierOverride, then calls this method rather
// than the unconditional RemoveUserTierOverride, so an admin who extends or replaces the
// override in the window between the select and this call is not overwritten by the worker's
// now-stale decision.
//
// Returns claimed=false with a nil error — never an error — when the CAS predicate does not
// match (the target is missing/soft-deleted, or an admin changed the override under us). Per
// the claim-then-act pattern, this is a normal, expected outcome: the caller should log and
// skip, not retry or alert — the expiry job runs again the next day and will pick the user up
// then if they are still genuinely eligible.
//
// The admin-initiated path (RemoveUserTierOverride, above) keeps its existing unconditional
// behaviour and signature untouched — this is a distinct method precisely so the CAS is opt-in
// for the one caller (the expiry worker) that needs it.
func (r *adminUserRepository) RemoveUserTierOverrideIfMatches(
	ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int,
	expectedOverride *string, expectedEndsAt *time.Time,
) (RemoveTierOverrideRow, bool, error) {
	return r.removeTierOverride(ctx, removeUserTierOverrideIfMatchesQuery,
		[]interface{}{targetUserID, retentionMonths, expectedOverride, expectedEndsAt},
		actorUserID, targetUserID, reason)
}

// buildFilterClause constructs the WHERE clause and arguments for admin user filters.
// Returns the WHERE clause (with leading space), args, and the next arg index.
func (r *adminUserRepository) buildFilterClause(filters domain.AdminUserFilters) (string, []any, int) {
	var clause string
	args := []any{}
	argIndex := 1

	if filters.Search != "" {
		clause += fmt.Sprintf(
			" AND EXISTS (SELECT 1 FROM user_email_prefix_indices p"+
				" WHERE p.user_id = u.id AND p.prefix_blind_idx = $%d)", argIndex)
		args = append(args, r.enc.BlindIndexWithLabel(appcrypto.LabelEmailPrefix,
			appcrypto.NormalizeAndCapSearchPrefix(filters.Search)))
		argIndex++
	}

	if filters.Tier != "" {
		clause += fmt.Sprintf(" AND u.subscription_tier = $%d", argIndex)
		args = append(args, filters.Tier)
		argIndex++
	}

	return clause, args, argIndex
}
