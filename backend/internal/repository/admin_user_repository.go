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

// removeUserTierOverrideQuery clears a user's manual tier override and, in the same
// statement, conditionally re-arms the retention clock.
//
// The "prior" CTE locks (FOR UPDATE) and reads the pre-UPDATE snapshot needed for two
// purposes: (1) PriorManualTierOverride, so the caller can tell whether this call actually
// removed anything, and (2) the read-only-status re-arm condition below, evaluated against
// the state as it was *before* this statement touches anything.
//
// Retention re-arm rationale (Issue #1413): a comp *pauses* the retention clock — Task 4
// excludes comped users from retention queries rather than clearing their dates, so the
// database keeps the truth of when read-only status actually began. The hazard: comp someone
// for 8 months against a 6-month clock, and at removal retention_expires_at is already in the
// past — they'd be hard-deleted on the next daily run with no 30d/7d warning. So: if a
// retention clock was running (retention_expires_at IS NOT NULL) AND the reverted raw status
// is read-only ('inactive'/'cancelled'/'past_due' — the SQL-side twin of
// domain.SubscriptionStatus.IsReadOnlySubscription()), restart the clock and clear both
// notice-sent flags so the warnings fire again. If the reverted status is NOT read-only (the
// user resubscribed while comped), retention is left untouched — the normal webhook path
// already clears it. If the clock was never running (retention_expires_at IS NULL), it stays
// NULL regardless of status.
const removeUserTierOverrideQuery = `
	WITH prior AS (
		SELECT manual_tier_override, subscription_status, retention_expires_at
		FROM users
		WHERE id = $1 AND is_deleted = FALSE
		FOR UPDATE
	)
	UPDATE users u
	SET manual_tier_override = NULL,
	    manual_tier_ends_at = NULL,
	    updated_at = NOW(),
	    retention_started_at = CASE
	        WHEN prior.retention_expires_at IS NOT NULL
	             AND prior.subscription_status IN ('inactive', 'cancelled', 'past_due')
	        THEN NOW()
	        ELSE u.retention_started_at
	    END,
	    retention_expires_at = CASE
	        WHEN prior.retention_expires_at IS NOT NULL
	             AND prior.subscription_status IN ('inactive', 'cancelled', 'past_due')
	        THEN NOW() + make_interval(months => $2::int)
	        ELSE u.retention_expires_at
	    END,
	    retention_notice_30d_sent_at = CASE
	        WHEN prior.retention_expires_at IS NOT NULL
	             AND prior.subscription_status IN ('inactive', 'cancelled', 'past_due')
	        THEN NULL
	        ELSE u.retention_notice_30d_sent_at
	    END,
	    retention_notice_7d_sent_at = CASE
	        WHEN prior.retention_expires_at IS NOT NULL
	             AND prior.subscription_status IN ('inactive', 'cancelled', 'past_due')
	        THEN NULL
	        ELSE u.retention_notice_7d_sent_at
	    END
	FROM prior
	WHERE u.id = $1
	RETURNING u.subscription_tier, u.subscription_status, u.clerk_user_id, prior.manual_tier_override
`

// RemoveUserTierOverride clears a user's manual tier override, re-arms the retention clock
// when the reverted status is read-only, and writes an audit log row — in one transaction.
// See the AdminUserRepository interface doc for the contract.
func (r *adminUserRepository) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (RemoveTierOverrideRow, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// If the target is missing or is_deleted, the "prior" CTE returns no rows, the implicit
	// join in "FROM prior" yields zero rows, and RETURNING is empty — surfaced below as
	// pgx.ErrNoRows -> ErrUserNotFound.
	var row RemoveTierOverrideRow
	var clerkUserIDEnc string
	err = tx.QueryRow(ctx, removeUserTierOverrideQuery, targetUserID, retentionMonths).Scan(
		&row.RevertedTier, &row.RevertedStatus, &clerkUserIDEnc, &row.PriorManualTierOverride,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RemoveTierOverrideRow{}, ErrUserNotFound
		}
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to remove user tier override: %w", err)
	}
	row.Removed = row.PriorManualTierOverride != nil && *row.PriorManualTierOverride != ""

	// Decrypt the returned clerk_user_id (it is encrypted in the DB)
	row.ClerkUserID, err = r.enc.DecryptString(clerkUserIDEnc)
	if err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}

	// Audit row inside the same transaction — even when Removed is false (idempotent no-op),
	// this call is the natural repair hook for metadata #1413 may have corrupted, so it always
	// audits and (in the service layer) always re-syncs Clerk.
	// actor_user_id -> audit_logs.user_id (acting admin); targetUserID -> resource_id.
	var actorParam interface{}
	if actorUserID != "" {
		actorParam = actorUserID
	} // else: leave nil -> SQL NULL. Column is nullable with FK ON DELETE SET NULL; a missing
	// actor must never abort a legitimate admin action.

	detailsMap := map[string]interface{}{
		"reason": reason,
	}
	if row.PriorManualTierOverride != nil {
		detailsMap["prior_manual_tier_override"] = *row.PriorManualTierOverride
	}
	detailsBytes, err := json.Marshal(detailsMap)
	if err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to marshal audit details: %w", err)
	}

	encryptedChanges, err := r.enc.EncryptJSON(json.RawMessage(detailsBytes))
	if err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to encrypt audit changes: %w", err)
	}

	auditQuery := `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'remove_manual_tier', 'user', $2, $3)
	`
	_, err = tx.Exec(ctx, auditQuery, actorParam, targetUserID, encryptedChanges)
	if err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to create audit log: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return RemoveTierOverrideRow{}, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return row, nil
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
