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
	// PriorManualTierEndsAt is the manual_tier_ends_at value as it was immediately before this
	// UPDATE (nil if no override was set, OR if a set override was permanent/non-expiring).
	// Issue #1413 Task NB: threaded through purely so the service layer can compute had_expiry
	// (PriorManualTierEndsAt != nil) for the Removed analytics event, matching the frontend's
	// {tier, had_expiry} payload shape. Not otherwise used by this struct's existing consumers.
	PriorManualTierEndsAt *time.Time
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
			WHERE deleted_at IS NULL
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
			WHERE deleted_at IS NULL
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
			WHERE deleted_at IS NULL
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
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

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
// 'trial', so a silent normalization would make the legacy trial-expiry job (deleted in #1652) never reselect
// this user again, and its retention-clock side effect (SetRetentionDates) would never run — the
// only side effect that survives this reasoning today. (A pre-round-7 version of this comment
// also cited a Plaid disconnect ExpireTrial performed directly; that no longer exists — the
// disconnect moved entirely to the repair pass in round-7, and that repair pass is separately
// gated on plaid_cleanup_pending_at, which should_mark_plaid_cleanup_pending below already
// excludes for a reverted 'trial' status, so this pin has no bearing on Plaid disconnect
// eligibility either way.) See internal/services/trial_expiry_comp_revert_integration_test.go for
// the pin and full reasoning.
// should_mark_plaid_cleanup_pending (Issue #1413 round-10) is should_rearm_retention's narrower
// sibling for the Plaid-disconnect-repair marker: a removed/expired comp (re-)starts
// plaid_cleanup_pending_at when the reverted raw status is one the repair pass actually targets —
// see removeUserTierOverrideUpdate's own doc comment, and ROUND 18/19/20 further below, for the
// CURRENT write (anchored on the lapse instant or on clock_timestamp(), never COALESCE — COALESCE
// was removed in ROUND 18 and must not be reintroduced). Deliberately narrower than
// should_rearm_retention's ('inactive', 'cancelled', 'past_due') — 'past_due' is excluded here for
// the SAME reason plaidDisconnectRepairEligibleStatusPredicate excludes it: past_due had its own
// dunning timeline (the deleted past_due_service.go / PastDueConfig.PlaidDisconnectDays, #1652) and
// must never be immediately Plaid-disconnected by this repair pass. A comp removed while the reverted status is
// 'past_due' leaves plaid_cleanup_pending_at untouched — it should already be NULL (past_due is
// never a state that sets this marker), and past_due's own dunning flow owns that user's Plaid
// fate instead.
//
// Issue #1413 round-13: as of round-13, updateUserTierQuery (comp grant) no longer nulls this
// marker at all, so a comp granted over an already inactive/cancelled user whose marker was already
// set by the transition that made them ineligible carries that marker, untouched, through the
// entire life of the comp — this CASE, on removal, always OVERWRITES it (per ROUND 18/19/20 below),
// it never merely preserves whatever was already there.
//
// Round 14 -> 15 -> 16 — read all three in order; the third is the one currently in force. The
// next reader should see deliberate movement toward a better fix, not unexplained churn.
//
// ROUND 14 (finding M1, Codex review): the "purely a defense-in-depth backstop for an
// imported/backfilled row" claim this comment made through round-13 was WRONG, and the wrongness
// was the bug: the ORDINARY post-cleanup path reaches this CASE too. Sequence: (1) cancellation
// sets the marker; (2) the repair pass actually completes cleanup and clears it
// (ClearPlaidCleanupPendingIfMatches); (3) an admin grants a comp over this already-cleaned-up
// user — round-13 correctly leaves the (already-NULL) marker alone; (4) the admin later removes
// that comp while raw subscription_status is STILL 'inactive'/'cancelled' (nothing in this flow
// changes it). Before this fix, should_mark_plaid_cleanup_pending was true for ANY prior override
// on an inactive/cancelled user regardless of whether cleanup was still outstanding, so step (4)'s
// COALESCE(NULL, NOW()) stamped a FRESH marker for an episode whose cleanup had already finished —
// a phantom the live-item-less repair query's EXISTS clause would then never select, leaving it
// non-NULL forever and making the stated invariant (domain.User.PlaidCleanupPendingAt's doc
// comment: non-NULL exactly when an episode has begun AND cleanup has not completed) false.
// Round 14's fix: gate should_mark_plaid_cleanup_pending on the SAME live-linked-item existence
// check the repair query itself uses — liveLinkedItemExistsClause (profile_repository.go, shared
// by GetUsersNeedingPlaidDisconnectRepair) — instead of a second, independently-worded copy that
// could drift. No live item, no re-arm.
//
// ROUND 15 (Finding 3, High — a later Codex review round on this same PR): round 14's gate itself
// races a concurrent LinkAccounts INSERT:
//
//   - This CTE's EXISTS(...) is evaluated as a snapshot at SELECT time, and the query's
//     FOR UPDATE locks the users row — it does not, and cannot, serialize against a concurrent
//     INSERT into linked_items. AccountService.LinkAccounts (internal/services/account_service.go)
//     creates a linked item via a single, independent, autocommit statement
//     (LinkedItemRepository.Create) with no lock or shared transaction of its own — it never
//     contends for the users row at all, so no lock strength taken here can make it wait for an
//     event (its own INSERT) that has not been issued to Postgres yet. Concretely: a user with a
//     LIVE comp is not read-only and CAN link (the read-only allow-list contains only
//     /webhooks/plaid, so this is the only reachable window); if LinkAccounts starts before
//     an admin removes that comp, the removal's EXISTS can see "no live item" (the INSERT hasn't
//     landed yet), commit with the marker left NULL, and only THEN does LinkAccounts' insert land —
//     a cancelled user with a live bank connection and a NULL marker, invisible to
//     GetUsersNeedingPlaidDisconnectRepair (non-NULL marker required). At the time, this was
//     PERMANENT: nothing else in the codebase ever revisited the decision.
//   - Round 15 weighed the two failure directions as asymmetric — a missed stamp (this race) was,
//     at the time, an unrecoverable data-retention leak, while a spurious stamp (round 14's own M1
//     phantom) is merely an untidy non-NULL timestamp the repair query's own live-item EXISTS
//     clause filters out forever (an accepted, separately-tracked residue class, #1453) — and erred
//     toward stamping: dropped the liveLinkedItemExistsClause guard again and went back to the
//     round-13 predicate, unconditional. This deliberately reintroduced round 14's M1 phantom, and,
//     as a forced consequence, contradicted the WRITTEN invariant
//     (domain.User.PlaidCleanupPendingAt's doc comment had to be weakened to match — see that
//     comment's own round-15 revision note, and assertPlaidMarkerInvariant's).
//
// ROUND 16 (this comment, Issue #1413): a third option closes what rounds 14 and 15 each had to
// trade away, because it does not depend on either writer's ordering at all.
// profileRepository.StampMissingPlaidCleanupMarkers (profile_repository.go) now runs once at the
// start of EVERY repairPlaidDisconnects pass (retention_cleanup_check.go) and atomically stamps any
// ineligible, live-linked-item, NULL-marker user it finds — a missed stamp from round 15's exact
// race now SELF-HEALS on the next pass instead of leaking forever. With that backstop in place,
// round 14's gate is RESTORED here (liveLinkedItemExistsClause is back in the CASE below), so round
// 14's phantom-marker bug stays fixed too — the one gap the gate itself can still open (round 15's
// race) is no longer a permanent leak, just a delayed heal (see StampMissingPlaidCleanupMarkers's
// own doc comment for the accepted consequence: up to one EXTRA grace window before disconnect, not
// "never"). The two round-14 M1 regression tests
// (TestRemoveUserTierOverride_Integration_M1_NoPhantomMarker_WhenCleanupAlreadyComplete and
// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NoLiveItems_MarkerNotArmed,
// admin_user_repository_remove_tier_override_integration_test.go) are flipped back to their
// original names and Nil assertions. Round 15's genuine two-connection concurrency proof that the
// race is real — TestRemoveUserTierOverride_Integration_LiveItemInsertedDuringRemoval_MarkerNotLost —
// is KEPT as the backstop's own regression test, with its assertion updated: the race still occurs
// (the marker is still left NULL immediately after the concurrent insert lands), but the user is
// now healed by StampMissingPlaidCleanupMarkers on the next pass instead of leaking permanently.
//
// A correction to round 15's own wording, while here: round 15's comment (and
// domain.User.PlaidCleanupPendingAt's) both stated flatly that no lock available to this repository
// could close this race. That is too absolute. A shared PostgreSQL advisory lock taken by BOTH this
// removal path and LinkAccounts' insert path, or running both under SERIALIZABLE isolation with
// retry-on-conflict, would each genuinely close it too. What is true, more narrowly, is that no lock
// taken UNILATERALLY by this repository, acting alone, can close it — a real fix by locking requires
// coordinating with LinkAccounts, a wholly separate, unrelated code path
// (internal/services/account_service.go). That is exactly why round 16 chose the backstop instead:
// it needs no coordination between the two unrelated paths at all, closing the gap from a third,
// independent location rather than asking two unrelated writers to agree on a lock.
//
// A second correction, this one to round 15's OTHER claim (Issue #1413 Finding 1, Codex review, PR
// #1443 follow-up, headline finding): round 15 called a spurious round-14 M1 stamp "merely an untidy
// non-NULL timestamp the repair query's own live-item EXISTS clause filters out forever (an accepted,
// separately-tracked residue class, #1453)". That is WRONG, and so is every other place on this
// branch that called this state inert or harmless. The COALESCE that USED to sit just above (see
// ROUND 18 below — it is gone now) was exactly the mechanism that turned a stale-but-non-NULL marker
// into live harm: if a PRIOR episode's cleanup completed but the marker was never cleared (e.g.
// StampMissingPlaidCleanupMarkers's clearing counterpart, ClearCompletedPlaidCleanupMarkers, has not
// yet run — see its own doc comment, profile_repository.go), that COALESCE(u.plaid_cleanup_pending_at,
// NOW()) preserved that stale timestamp instead of starting a fresh one, even though the user had
// since relinked a bank and the removed comp was handing them straight back to
// repairPlaidDisconnects. The next pass would see a live item paired with an already-expired grace
// window and disconnect the newly-linked bank immediately — the grace window this whole mechanism
// exists to provide, silently skipped. Not filtered out forever; reachable, and harmful, the moment a
// comp is granted and removed over it. ROUND 18 (below) is the fix for exactly this.
//
// ROUND 18 (Issue #1413 High finding, Codex review, PR #1443 follow-up): the immediately preceding
// paragraph, and round 16's ClearCompletedPlaidCleanupMarkers (profile_repository.go), each treated
// the hazard above as something Clear could fully close given enough time. It cannot, on its own —
// Clear is only a PARTIAL mitigation, because it can act only if a repair pass happens to RUN while
// the user genuinely has no live item. Two sequences skip that window entirely, and both end with a
// freshly-relinked bank disconnected immediately, with ZERO grace:
//
//   - Path A (an all-disconnected user): a stale marker T sits on a user whose items are all
//     disconnected (Clear's own WHERE clause — anyLinkedItemExistsClause AND NOT
//     liveLinkedItemExistsClause — WOULD match this row). An admin grants a comp; the user relinks a
//     bank (permitted — the comp makes their EFFECTIVE status active, internal/huma/middleware/
//     read_only.go); the admin removes the comp BEFORE the next repair pass gets a turn to run Clear
//     over this exact row. Clear never observed the no-live-item state in time. The live item now
//     makes the row ineligible for Clear anyway (NOT liveLinkedItemExistsClause is false), and the old
//     COALESCE preserved T — already past grace the instant it's read.
//   - Path B (the #1453 zero-row population): same sequence, but the user has ZERO linked_items rows
//     when T is set (never linked Plaid, or migration residue) — anyLinkedItemExistsClause is false,
//     so Clear's WHERE clause can NEVER match this row, not before the relink and not after. This path
//     is structurally unreachable for Clear, not merely unlucky timing like Path A.
//
// See TestRemoveUserTierOverride_Integration_Finding1PathA_AllDisconnectedWithStaleMarker_FreshGraceOnRelink
// and TestRemoveUserTierOverride_Integration_Finding1PathB_ZeroLinkedItemsPopulation_FreshGraceOnRelink
// (admin_user_repository_remove_tier_override_integration_test.go) for both reproduced end to end
// through production writers.
//
// Root cause: plaid_cleanup_pending_at is supposed to record when the CURRENT episode became
// observable (domain.User.PlaidCleanupPendingAt's doc comment). The COALESCE kept whatever value was
// already sitting in the column — which, in both paths above, belongs to a PREVIOUS, already-completed
// episode, not the one this removal is (re-)arming. No amount of clearing logic anywhere else in the
// codebase fixes a WRITER that resurrects a stale value; the writer itself has to stop doing that.
//
// The fix, in removeUserTierOverrideUpdate below: should_mark_plaid_cleanup_pending's CASE now writes
// NOW() UNCONDITIONALLY, never COALESCE. should_mark_plaid_cleanup_pending is already gated on
// liveLinkedItemExistsClause (round 16 restored it — see above) — a LIVE item genuinely exists every
// single time this CASE fires. That is exactly when an episode SHOULD be open, by the invariant's own
// definition, so its start time is properly "now", never whatever a prior, unrelated episode happened
// to leave behind.
//
// This reads like a regression, and deliberately is not one — record why, because the next reviewer
// will see a COALESCE replaced by NOW() and reach for the revert:
//
//  1. For a user whose episode was genuinely ONGOING (the marker already reflects a live, still-open
//     episode when this CASE fires), writing NOW() instead of keeping the existing value RESTARTS the
//     72-hour grace window rather than continuing it. That is not merely safe — it is MORE principled.
//     The user held a live comp for some or all of the interim (should_mark_plaid_cleanup_pending only
//     fires on a removal, i.e. a prior override existed), so the window in which they can react to
//     actually losing access should run from the moment they actually lose it — comp removal — not from
//     whatever moment, possibly long before the comp was even granted, first opened the episode. A comp
//     is exactly the kind of intervening fact that should extend, not be silently ignored by, the grace
//     window computed on top of it.
//  2. The two failure directions are asymmetric, and that asymmetry is what settles the direction of
//     this fix. Restarting a grace window on an ongoing episode DELAYS a disconnect — recoverable: the
//     worst outcome is a Plaid connection that stays live a little longer than the strict minimum before
//     the next repair pass would have caught it anyway once the (now fresh) window elapses. Preserving a
//     stale marker instead SKIPS the grace window ENTIRELY for a bank connection that may be only
//     moments old — not recoverable: the user's bank is already gone the instant the next repair pass
//     runs, with zero chance to react. When one direction of getting it wrong costs a delay and the
//     other costs an unrecoverable, notice-free disconnection, the fix must err toward the delay.
//
// State the accepted cost explicitly, rather than leaving it implicit: an admin who repeatedly grants
// and removes a comp over the SAME user postpones that user's eventual disconnect by a fresh 72 hours
// EVERY time. That is entirely admin-driven — it requires an admin action on every single postponement,
// not something that happens on its own — and an admin choosing to remove a comp is, definitionally,
// signalling that they want this user's ineligibility to actually take effect. Seventy-two hours of
// grace per deliberate removal is proportionate to that signal, not a loophole.
//
// Reassessing ClearCompletedPlaidCleanupMarkers (profile_repository.go) now that this writer no longer
// needs it to prevent the harm above: KEEP IT. It still repairs the case that originally motivated it,
// entirely independent of any comp ever being involved — a user whose repair pass disconnected every
// item successfully but whose marker-clear retries all failed (a genuine, if rare, DB blip outlasting
// the retry loop's backoff window) is left with a stale non-NULL marker on a row with zero live items,
// and NOTHING else in this codebase clears it if that user simply stays in their ineligible status
// forever with no comp ever granted over them — Stamp cannot help either, since Stamp's own WHERE
// clause requires the marker to be NULL, and this marker is not. Without Clear, that specific violation
// of the invariant (domain.User.PlaidCleanupPendingAt's doc comment, case (b)) would sit unrepaired
// indefinitely for exactly that population. This fix closes the ONE additional way a stale marker could
// turn into unrecoverable harm (a comp cycle over it); it does not make the marker's own steady-state
// correctness unconditionally self-maintaining without Clear's help. Do NOT remove Clear on the
// strength of this fix alone — it closes a different hole and still has a live, unrelated job to do.
//
// ROUND 19 (Finding 1, Medium, Codex review, PR #1443 follow-up — a later review round on this same
// CASE): ROUND 18's unconditional NOW() fixed the resurrected-stale-marker bug above, but its own
// justification — "the window in which they can react to actually losing access should run from the
// moment they actually lose it" — exposes a gap it did not close: should_mark_plaid_cleanup_pending
// only checks that a PRIOR override was non-empty, never whether that override had already LAPSED by
// removal time. Concretely: a comp expires at T; the user is read-only (HasActiveManualOverride()
// false) from T onward; the expiry job runs late, or an admin clicks Remove Override, at T+24h;
// ROUND 18's unconditional NOW() stamps T+24h — a FULL FRESH 72 hours measured from 24 hours AFTER
// the user actually lost access, contradicting ROUND 18's own rationale for the user whose comp had
// already lapsed rather than one still genuinely live at removal.
//
// The fix: anchor on manual_tier_ends_at (now selected below, alongside manual_tier_override) when
// it is in the past AT removal time — that IS the instant access was actually lost. Only when the
// override was STILL LIVE at removal (an admin revoking an active comp; access is lost AT that
// removal, not earlier) does "now" remain the correct anchor.
//
// State the consequence plainly, because out of context it reads like a new bug: if the comp lapsed
// LONGER ago than the 72-hour grace period, the resulting marker already sits outside the window, and
// the very next repair pass can disconnect the user with no additional grace at the point of removal.
// That is CORRECT, not a regression — the user already spent their full 72 hours as a read-only user
// once access genuinely lapsed at manual_tier_ends_at; a comp-removal call arriving late must not
// manufacture a SECOND grace window on top of the one that already elapsed. See
// TestRemoveUserTierOverride_Integration_LapsedCompAnchorsMarkerAtLapseTime_NotRemovalTime and
// TestRemoveUserTierOverride_Integration_LapsedCompLongerThanGrace_ImmediatelyEligible
// (admin_user_repository_remove_tier_override_integration_test.go).
//
// ROUND 20 (Finding 2, Medium, same review round): PostgreSQL NOW() is transaction_timestamp() —
// fixed at the enclosing transaction's BEGIN, not at the statement that actually executes.
// removeTierOverride (below) opens an explicit transaction (r.db.Begin(ctx)) and only then runs this
// CTE's FOR UPDATE inside it — if that lock acquisition waits a long time (a concurrent writer
// already holding the row), NOW() still reflects the pre-wait BEGIN instant, not the moment the row
// is actually written. Concretely: BEGIN at T0, block 73 hours on the row lock, then write
// plaid_cleanup_pending_at = NOW() = T0 — the very next repair pass sees a marker already 73 hours
// stale and disconnects a connection with no meaningful grace at all. Same harm class ROUND 18 set
// out to fix, caused by a different clock. Fix: use clock_timestamp() — statement/wall time,
// re-evaluated fresh at actual execution — for the grace ANCHOR only (both branches in ROUND 19
// above).
//
// ROUND 21 correction (Issue #1413 Finding 2, Medium, PR #1443 follow-up round): ROUND 20 itself drew
// the "left alone" line in the wrong place, on a reason that does not hold up. It reasoned that
// StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers (profile_repository.go) could
// keep NOW() because "each is a single autocommit statement with no earlier lock-acquiring statement
// in the same transaction, so their own transaction start and their own statement execution
// coincide." That is FALSE: NOW()/transaction_timestamp() is fixed at the transaction's BEGIN, which
// for a bare autocommit statement is essentially "when the statement was submitted" — BEFORE Postgres
// acquires the row locks the statement itself needs in order to execute, not after. An autocommit
// UPDATE that must WAIT for a row lock a concurrent transaction already holds (exactly
// StampMissingPlaidCleanupMarkers's own situation if its WHERE clause matches a row THIS removal is
// mid-transaction on) still writes a pre-wait NOW() once it finally acquires the lock — "no earlier
// statement in the same transaction" was never the relevant fact; "can this statement block on a
// lock at all" is. See StampMissingPlaidCleanupMarkers's own doc comment (profile_repository.go) for
// that fix, now corrected there too. ClearCompletedPlaidCleanupMarkers only ever writes NULL, never a
// timestamp value, so it was never actually at risk — the ROUND 20 comment lumped it in with its
// sibling for symmetry, not because it shared the defect.
//
// The SAME wrong "autocommit is exempt" reasoning also left retention_expires_at on NOW() just below,
// on the DIFFERENT (also invalid) ground that it is "an ordinary audit/business timestamp... not a
// grace-window anchor." That one was simply wrong on the facts: retention_expires_at is not audit-only
// — it IS a deadline, read directly by GetUsersNeedingRetention30dNotice/7dNotice and
// GetUsersNeedingHardDeletion (profile_repository.go) to decide when to warn a user and when to
// actually hard-delete their account under GDPR. removeTierOverride opens the SAME explicit
// transaction this whole ROUND 20 paragraph is about, so a long FOR UPDATE wait backdates
// retention_expires_at exactly as it would have backdated plaid_cleanup_pending_at: BEGIN at T0,
// block on the row lock, then write retention_expires_at = NOW() + N months = T0 + N months —
// shortening the user's actual GDPR deletion deadline by however long the wait lasted. Fixed the same
// way: clock_timestamp() instead of NOW().
//
// What DOES stay on NOW(), correctly, and why each one is different in kind from the two fixed above:
// `updated_at` (plain last-modified ordering, never read as an age or a deadline by anything);
// retention_started_at (a paired audit field for retention_expires_at — recorded for when the
// retention clock was armed, but nothing in this codebase computes an age FROM retention_started_at
// or compares it to NOW()/clock_timestamp() anywhere; only retention_expires_at is ever read back as
// a deadline). A tiny inconsistency this leaves on the record — retention_started_at can be a few
// moments earlier than "retention_expires_at minus N months" whenever a lock wait actually occurs — is
// accepted: it affects only what the started-at audit trail SAYS, never what any query DOES with it,
// which is the same audit-vs-deadline line this whole correction draws everywhere else.
//
// Lock contention is inherently hard to force deterministically without either a real multi-second
// wait or a genuine race. TestRemoveUserTierOverride_Integration_ClockTimestampReflectsWallTimeAcrossLockWait
// (admin_user_repository_remove_tier_override_integration_test.go) holds a real FOR UPDATE lock on a
// second connection, launches the real RemoveUserTierOverride call on a third, and proves BOTH
// plaid_cleanup_pending_at and retention_expires_at land AFTER the lock is released rather than at the
// moment the removal's own transaction began — a bounded, deterministic (not statistical) proof of the
// clock_timestamp()-vs-transaction_timestamp() distinction ROUND 19/21's fixes depend on. Round 21
// also replaced that test's fixed time.Sleep-based "hope the goroutine is blocked by now" wait with a
// real barrier (Issue #1413 Finding 3, Codex review, PR #1443 follow-up) — see waitUntilBlockedBy's
// own doc comment (same test file) for why a sleep alone cannot prove what this test claims.
const removeUserTierOverrideSelectPrior = `
	SELECT manual_tier_override, manual_tier_ends_at, subscription_status, retention_expires_at,
	       (manual_tier_override IS NOT NULL AND manual_tier_override <> ''
	        AND subscription_status IN ('inactive', 'cancelled', 'past_due')) AS should_rearm_retention,
	       (manual_tier_override IS NOT NULL AND manual_tier_override <> ''
	        AND subscription_status IN ('inactive', 'cancelled')
	        AND ` + liveLinkedItemExistsClause + `) AS should_mark_plaid_cleanup_pending
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
//
// plaid_cleanup_pending_at's CASE (Issue #1413 Findings 1 and 2, Codex review, PR #1443 follow-up —
// see ROUND 19/ROUND 20 in removeUserTierOverrideSelectPrior's doc comment just above for the full
// reasoning) fires only when should_mark_plaid_cleanup_pending is true, and then anchors on:
//   - prior.manual_tier_ends_at, when the override had ALREADY lapsed by removal time — access was
//     lost THEN, not now, so a late removal must not manufacture a fresh window on top of one that
//     already elapsed (ROUND 19);
//   - clock_timestamp() otherwise — real statement-execution wall time, not NOW()'s transaction-start
//     time, which a long FOR UPDATE lock wait inside removeTierOverride's explicit transaction could
//     otherwise silently backdate (ROUND 20).
//
// Never COALESCE(u.plaid_cleanup_pending_at, ...) — that resurrects a stale PRIOR episode's
// timestamp instead of anchoring the CURRENT one; see ROUND 18 above for why that was itself a bug.
//
// retention_expires_at's CASE (ROUND 21, Issue #1413 Finding 2, Codex review, PR #1443 follow-up)
// also uses clock_timestamp(), not NOW(), for the SAME reason as the plaid_cleanup_pending_at CASE
// just above: this whole UPDATE runs inside removeTierOverride's explicit transaction, behind the
// SAME potentially-long FOR UPDATE wait, and retention_expires_at is a real GDPR deletion deadline
// (read by GetUsersNeedingRetention30dNotice/7dNotice/HardDeletion, profile_repository.go), not mere
// audit — see removeUserTierOverrideSelectPrior's ROUND 21 note just above for the full reasoning
// and for why retention_started_at, immediately above this line, correctly keeps NOW() instead.
const removeUserTierOverrideUpdate = `
	UPDATE users u
	SET manual_tier_override = NULL,
	    manual_tier_ends_at = NULL,
	    updated_at = NOW(),
	    retention_started_at = CASE WHEN prior.should_rearm_retention THEN NOW() ELSE u.retention_started_at END,
	    retention_expires_at = CASE WHEN prior.should_rearm_retention THEN clock_timestamp() + make_interval(months => $2::int) ELSE u.retention_expires_at END,
	    retention_notice_30d_sent_at = CASE WHEN prior.should_rearm_retention THEN NULL ELSE u.retention_notice_30d_sent_at END,
	    retention_notice_7d_sent_at = CASE WHEN prior.should_rearm_retention THEN NULL ELSE u.retention_notice_7d_sent_at END,
	    plaid_cleanup_pending_at = CASE
	        WHEN prior.should_mark_plaid_cleanup_pending
	             AND prior.manual_tier_ends_at IS NOT NULL
	             AND prior.manual_tier_ends_at < clock_timestamp()
	        THEN prior.manual_tier_ends_at
	        WHEN prior.should_mark_plaid_cleanup_pending
	        THEN clock_timestamp()
	        ELSE u.plaid_cleanup_pending_at
	    END
	FROM prior
	WHERE u.id = $1
	RETURNING u.subscription_tier, u.subscription_status, u.clerk_user_id, prior.manual_tier_override,
	          prior.manual_tier_ends_at
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
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	var row RemoveTierOverrideRow
	var clerkUserIDEnc string
	err = tx.QueryRow(ctx, query, args...).Scan(
		&row.RevertedTier, &row.RevertedStatus, &clerkUserIDEnc, &row.PriorManualTierOverride,
		&row.PriorManualTierEndsAt,
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
