package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	ErrAccountNotFound           = errors.New("account not found")
	ErrDuplicateAccount          = errors.New("account already exists for this user")
	ErrAccountOwnedByAnotherUser = errors.New("one or more accounts are already linked by another user")

	// ErrAccountBatchTenantMismatch is returned when a batch write is handed a row whose UserID
	// is not the acting user. See CreateBatch (issue #1566).
	ErrAccountBatchTenantMismatch = errors.New("account batch contains a row belonging to another user")
)

// FingerprintOwnership holds information about who owns a fingerprint and the state of their linked item.
// Used during cross-user uniqueness check in LinkAccounts (Issue #1010).
type FingerprintOwnership struct {
	UserID          string
	LinkedItemID    string
	ItemStatus      domain.LinkedItemStatus
	StatusChangedAt *time.Time
}

// AccountFilterOptions configures filtering for GetAccountsWithInstitutionFiltered (Issue #1109).
type AccountFilterOptions struct {
	IncludeHidden   bool // if true, includes hidden accounts
	IncludeReplaced bool // if true, widens the status filter to active + replaced (NOT expired)
	// ReplacedOnly returns only replaced accounts (status='replaced'). It has the HIGHEST
	// precedence of the three status flags — see resolveAccountStatusScope: setting AllStatuses
	// alongside it does not widen the result, and active rows are dropped.
	ReplacedOnly bool

	UserAccountID string // if non-empty, filters to accounts sharing this user_account_id

	// AllStatuses removes the status predicate entirely, returning active, replaced AND
	// expired accounts. IncludeReplaced is NOT a substitute: it produces
	// `status IN ('active','replaced')`, which excludes 'expired' — and 'expired' is exactly
	// what ExpireAccountsByFingerprint writes for a fully-disconnected chain (the three
	// statuses are enumerated in domain/account.go). Callers that need the account universe
	// as it was at some past instant — e.g. the dashboard's 30-day comparison baseline, which
	// must attribute a since-disconnected account's historical snapshot to its real type —
	// need this, not IncludeReplaced. Issue #1537.
	//
	// AllStatuses deliberately does NOT increment the AccountsReplacedFiltered canary: that
	// counter measures active-only calls that DROP replaced accounts, and this option drops
	// nothing. That decision is no longer re-derived from these booleans at the call site — it
	// is taken from the resolved accountStatusScope, so the counter cannot disagree with the
	// predicate the query actually ran (which it did for ReplacedOnly+AllStatuses).
	AllStatuses bool
}

// AccountRepository defines the interface for account data access
type AccountRepository interface {
	// CreateBatch creates multiple accounts in a single transaction for a single tenant.
	// Every element's UserID must equal userID; a mismatch returns
	// ErrAccountBatchTenantMismatch and writes nothing (issue #1566).
	CreateBatch(ctx context.Context, userID string, accounts []domain.Account) error

	// GetByLinkedItemID retrieves all accounts for a specific linked item
	GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error)

	// GetByUserID retrieves all accounts for a specific user
	GetByUserID(ctx context.Context, userID string) ([]domain.Account, error)

	// GetForHouseholdScope retrieves every active account currently shared into the
	// household (visibility='shared', not revoked, owner is an active member), across
	// ALL members — not just requestingUserID. Household balance aggregation must use
	// this instead of GetByUserID, which is single-user and would silently drop every
	// non-requester member's accounts (Codex adversarial review, P0-2). Joint accounts
	// dedup to one row per physical fingerprint (freshest-synced copy AT OR BEFORE asOf
	// wins). requestingUserID is passed through only to satisfy the shared household CTE's
	// parameter typing. asOf is the point-in-time cutoff used to resolve joint-account
	// fingerprint winners; it must match the asOf passed to whichever
	// GetSnapshotsAsOfForHousehold call this account map will be paired with, or the two
	// can disagree on which member's copy wins for a joint account (Issue #1369
	// gap-closure G1). Issue #1369 — Phase 5.
	GetForHouseholdScope(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error)

	// GetForHouseholdScopeAllStatuses is GetForHouseholdScope without the
	// `a.status = 'active'` predicate: it returns every chain member (active, replaced AND
	// expired) of every account shared into the household. Household aggregation of a PAST
	// instant needs it, because GetForHouseholdScope filters to active REGARDLESS of its asOf
	// argument — so a household account disconnected since asOf is missing from its result and
	// its historical snapshot cannot be attributed to an account type. Same asOf contract as
	// GetForHouseholdScope: it must match the asOf of the GetSnapshotsAsOfForHousehold call it
	// is paired with. Callers must NOT use this for a "current" figure — the product rule is
	// that current totals include only linked/active accounts. Issue #1537.
	GetForHouseholdScopeAllStatuses(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error)

	// GetAccountsWithInstitution retrieves all accounts for a user with institution names
	// This method performs a LEFT JOIN with linked_items to include institution_name
	GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error)

	// CountByUserID counts the total number of accounts for a user
	CountByUserID(ctx context.Context, userID string) (int, error)

	// CountActiveByLinkedItemID counts active accounts for a linked item.
	// Only counts accounts with status='active', excludes 'replaced' accounts.
	// Used for zero-account item detection and cleanup.
	// Issue #668: Plaid Item Management for Cost Optimization
	CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error)

	// Delete removes an account from the database with ownership validation.
	// The account is only deleted if both the account ID and user ID match (WHERE id = $1 AND user_id = $2).
	// This prevents users from deleting accounts they don't own.
	//
	// Returns ErrAccountNotFound if:
	//   - The account doesn't exist
	//   - The account exists but belongs to a different user (ownership check fails)
	Delete(ctx context.Context, accountID, userID string) error

	// GetByIDAndUser retrieves an account by ID with ownership validation.
	// Returns ErrAccountNotFound if the account doesn't exist or belongs to a different user.
	GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error)

	// Update updates account customization fields with ownership validation.
	// Returns ErrAccountNotFound if the account doesn't exist or user doesn't own it.
	Update(ctx context.Context, account *domain.Account, userID string) error

	// UpdateDisplayOrders updates the display_order for multiple accounts atomically.
	// All accounts must belong to the specified user.
	// Returns ErrAccountNotFound if any account doesn't exist or user doesn't own it.
	UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error

	// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and flexible filtering.
	// Results are sorted by display_order ASC (nulls last), then created_at ASC.
	GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error)

	// GetByUserAccountAndStatus retrieves accounts by user_account_id and status.
	// This is used for querying accounts across re-links using stable user_account reference.
	// Issue #171: Plaid Re-Link Handling
	//
	// Returns empty slice if no accounts found (not an error).
	GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error)

	// GetByUserAccountIDAndUser retrieves an account by user_account_id with ownership validation.
	// Returns ErrAccountNotFound if no account with that user_account_id belongs to userID.
	// Used for IDOR protection when a TransferAction carries from_user_account_id/to_user_account_id
	// (the stable account reference that survives Plaid re-links). Issue #972: GAP AE.
	GetByUserAccountIDAndUser(ctx context.Context, userAccountID, userID string) (*domain.Account, error)

	// GetByUserAccountID retrieves all accounts for a user_account (any status).
	// This returns both active and replaced accounts for historical view.
	// Issue #171: Plaid Re-Link Handling
	GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error)

	// UpdateStatus updates an account's status and soft-delete fields with ownership validation.
	// Used when marking accounts as replaced during re-link.
	// Issue #171: Plaid Re-Link Handling
	//
	// Returns ErrAccountNotFound if account doesn't exist or user doesn't own it.
	UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error

	// CheckFingerprintOwnership checks if a fingerprint blind index is already held by a different user.
	// Returns ownership info (user, linked item, item status) if another user owns the fingerprint.
	// Returns nil, nil if the fingerprint is unclaimed or only claimed by the requesting user.
	// Issue #1010: Enforce Plaid item uniqueness across all users.
	CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*FingerprintOwnership, error)

	// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired'.
	// This releases the partial UNIQUE index constraint so a new user can link the same account.
	// Called when the 90-day TTL expires for a disconnected/revoked item.
	// Issue #1010: Enforce Plaid item uniqueness across all users.
	ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error
}

// accountRepository implements AccountRepository using PostgreSQL
type accountRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewAccountRepository creates a new AccountRepository instance
func NewAccountRepository(db DB, enc *appcrypto.FieldEncryptor) AccountRepository {
	return &accountRepository{db: db, enc: enc}
}

// mapConstraintError maps database constraint violation errors to domain errors
func mapConstraintError(err error) error {
	errMsg := err.Error()
	if strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique constraint") {
		if strings.Contains(errMsg, "unique_user_account") || strings.Contains(errMsg, "unique_accounts_plaid_id_blind") {
			return ErrDuplicateAccount
		}
		if strings.Contains(errMsg, "unique_persistent_account_id_active") || strings.Contains(errMsg, "unique_fallback_fingerprint_active") {
			return ErrAccountOwnedByAnotherUser
		}
	}
	return err
}

// encryptAccountPIIFields encrypts PII fields on an account before writing to the database.
// Blind indexes must be computed from plaintext BEFORE calling this function.
func (r *accountRepository) encryptAccountPIIFields(a *domain.Account) error {
	encryptedPlaidAccountID, err := r.enc.EncryptString(a.PlaidAccountID)
	if err != nil {
		return err
	}
	a.PlaidAccountID = encryptedPlaidAccountID

	encryptedName, err := r.enc.EncryptString(a.Name)
	if err != nil {
		return err
	}
	a.Name = encryptedName

	encryptedOfficialName, err := r.enc.EncryptNullableString(a.OfficialName)
	if err != nil {
		return err
	}
	a.OfficialName = encryptedOfficialName

	encryptedMask, err := r.enc.EncryptNullableString(a.Mask)
	if err != nil {
		return err
	}
	a.Mask = encryptedMask

	return nil
}

// decryptAccountPIIFields decrypts PII fields on an account after reading from the database.
func (r *accountRepository) decryptAccountPIIFields(a *domain.Account) error {
	decryptedPlaidAccountID, err := r.enc.DecryptString(a.PlaidAccountID)
	if err != nil {
		return err
	}
	a.PlaidAccountID = decryptedPlaidAccountID

	decryptedName, err := r.enc.DecryptString(a.Name)
	if err != nil {
		return err
	}
	a.Name = decryptedName

	decryptedOfficialName, err := r.enc.DecryptNullableString(a.OfficialName)
	if err != nil {
		return err
	}
	a.OfficialName = decryptedOfficialName

	decryptedMask, err := r.enc.DecryptNullableString(a.Mask)
	if err != nil {
		return err
	}
	a.Mask = decryptedMask

	// InstitutionName is only populated by JOIN queries; decrypt when present
	if a.InstitutionName != "" {
		decryptedInstitutionName, err := r.enc.DecryptString(a.InstitutionName)
		if err != nil {
			return err
		}
		a.InstitutionName = decryptedInstitutionName
	}

	return nil
}

// assertSingleTenantAccounts fails if any element of accounts is owned by someone other than
// userID. An empty element UserID is a mismatch too: a zero value is exactly how a
// mixed-tenant slice slips in, and treating it as "inherit the actor" would paper over the bug.
func assertSingleTenantAccounts(userID string, accounts []domain.Account) error {
	// An empty actor is never a valid tenant — see assertSingleTenantOverrides for the vacuous-match
	// case this closes.
	if userID == "" {
		return fmt.Errorf("CreateBatch: %w: no acting user_id was supplied", ErrAccountBatchTenantMismatch)
	}
	for i := range accounts {
		if accounts[i].UserID != userID {
			return fmt.Errorf("CreateBatch: %w: element %d has user_id %q, expected %q",
				ErrAccountBatchTenantMismatch, i, accounts[i].UserID, userID)
		}
	}
	return nil
}

// CreateBatch creates multiple accounts in a single transaction, all owned by userID.
//
// #1566: the batch is single-tenant BY ASSERTION, not by convention. `accounts` is joined by
// every analytics query, so a row inserted for a non-actor user is visible to cached reads of a
// user the cache-invalidation middleware never bumps. A mixed-tenant slice is refused loudly
// (ErrAccountBatchTenantMismatch) before any statement runs — never silently skipped.
func (r *accountRepository) CreateBatch(ctx context.Context, userID string, accounts []domain.Account) error {
	if len(accounts) == 0 {
		return nil
	}

	if err := assertSingleTenantAccounts(userID, accounts); err != nil {
		return err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	query := `
		INSERT INTO accounts
		(id, linked_item_id, user_id, user_account_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	now := time.Now()

	for i := range accounts {
		account := &accounts[i]

		if account.ID == "" {
			account.ID = uuid.New().String()
		}

		account.CreatedAt = now
		account.UpdatedAt = now

		plaidAccountIDBlindIdx := r.enc.BlindIndex(account.PlaidAccountID)

		if err := r.encryptAccountPIIFields(account); err != nil {
			return err
		}

		_, err := tx.Exec(ctx, query,
			account.ID,
			account.LinkedItemID,
			account.UserID,
			account.UserAccountID,
			account.PlaidAccountID,
			plaidAccountIDBlindIdx,
			account.Name,
			account.OfficialName,
			account.Type,
			account.Subtype,
			account.Mask,
			account.CurrentBalance,
			account.AvailableBalance,
			account.CurrencyCode,
			account.PersistentAccountIDBlindIdx,
			account.FallbackFingerprintBlindIdx,
			account.CreatedAt,
			account.UpdatedAt,
		)

		if err != nil {
			return mapConstraintError(err)
		}
	}

	return tx.Commit(ctx)
}

// GetByLinkedItemID retrieves all accounts for a specific linked item
func (r *accountRepository) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, linkedItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetByUserID retrieves active-only accounts for a specific user, including status and re-link fields.
// Returns ONLY accounts with status='active' (excludes replaced and expired).
// Callers that need the active + replaced chain members should use
// GetAccountsWithInstitutionFiltered with IncludeReplaced=true; that still EXCLUDES 'expired', so
// callers that need every status (the point-in-time universe) need AllStatuses=true. There is no
// repository-level "StatusFilter" option — that name belongs to services.AccountFilterOptions,
// where "all" maps to IncludeReplaced and therefore also stops short of 'expired'.
func (r *accountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetForHouseholdScope retrieves every active account shared into the household across
// all its members. Uses the asOf-bounded balance-oriented household CTE (freshest-synced
// copy AT OR BEFORE asOf wins for joint-account fingerprint collisions — see
// householdAccountsCTEForBalancesAsOf) since account metadata backs balance/net-worth
// aggregation, not transaction history. asOf must match whichever
// GetSnapshotsAsOfForHousehold call this account map is paired with (Issue #1369
// gap-closure G1) — see the interface doc for why. Issue #1369 — Phase 5.
func (r *accountRepository) GetForHouseholdScope(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error) {
	return r.getForHouseholdScope(ctx, requestingUserID, scope, asOf, householdActiveOnly)
}

// GetForHouseholdScopeAllStatuses is GetForHouseholdScope with the status predicate removed,
// so a chain that has since been replaced or expired in its entirety still resolves. See the
// interface doc for when that is the correct universe and when it is not. Issue #1537.
func (r *accountRepository) GetForHouseholdScopeAllStatuses(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error) {
	return r.getForHouseholdScope(ctx, requestingUserID, scope, asOf, householdAllStatuses)
}

// householdStatusFilter selects which account statuses a household-scope read returns. It is a
// typed flag rather than a SQL fragment on purpose: this helper used to take the WHERE clause
// itself as a string and splice it in, which was safe only for as long as every call site was
// inspected and stayed a compile-time literal. A closed two-value type makes an injected
// predicate impossible by construction, not merely absent today. Issue #1537.
type householdStatusFilter int

const (
	// householdActiveOnly restricts to `a.status = 'active'` (GetForHouseholdScope).
	householdActiveOnly householdStatusFilter = iota
	// householdAllStatuses applies no status predicate, so active, replaced AND expired chain
	// members resolve (GetForHouseholdScopeAllStatuses).
	householdAllStatuses
)

// statusClause renders the SQL predicate for this filter, appended after `WHERE TRUE`. The two
// strings are the only status SQL either household-scope read can produce.
func (f householdStatusFilter) statusClause() string {
	if f == householdAllStatuses {
		return ""
	}
	return " AND a.status = 'active'"
}

// householdScopeQuery builds the household-scope account SELECT. Pure and separate from the
// read itself so the exact SQL each filter produces can be asserted without a database.
func householdScopeQuery(statuses householdStatusFilter) string {
	return "WITH " + householdAccountsCTEForBalancesAsOf() + `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at
		FROM accounts a
		JOIN household_accounts ha ON ha.user_account_id = a.user_account_id
		WHERE TRUE` + statuses.statusClause() + `
		ORDER BY a.created_at ASC
	`
}

// getForHouseholdScope is the shared body of the two exported household-scope account reads.
// The status predicate is chosen from the typed statuses flag (never from caller-supplied text)
// so the two variants cannot drift apart in their column list, CTE, dedup or decryption handling.
func (r *accountRepository) getForHouseholdScope(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time, statuses householdStatusFilter) ([]domain.Account, error) {
	query := householdScopeQuery(statuses)

	args := householdScopeArgs(requestingUserID, scope)
	args = append(args, asOf)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetAccountsWithInstitution retrieves all accounts for a user with institution names from linked_items.
// Includes status and re-link fields so callers can distinguish active vs. replaced accounts.
//
// Issue #1444: a.currency_code and l.institution_name are both nullable (accounts.currency_code:
// migration 000007:22 — DEFAULT 'USD' but no NOT NULL; linked_items.institution_name: migration
// 000006:14 — no NOT NULL, no default) but were scanned directly into bare (non-pointer) string
// fields — "cannot scan NULL into *string". COALESCE guarantees a non-NULL scan target: 'USD' for
// currency_code (matching the column's own default intent), '' for institution_name (the natural
// zero for this omitempty field, matching GetAccountsWithInstitutionFiltered's established rule
// for the same column).
func (r *accountRepository) GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, COALESCE(l.institution_name, '') AS institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
		ORDER BY a.created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
			&account.InstitutionName,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// CountByUserID counts the total number of accounts for a user.
// Excludes accounts with status='expired' (Issue #1010: TTL-released fingerprints).
func (r *accountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status != 'expired'
	`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CountActiveByLinkedItemID counts active accounts for a linked item.
// Only counts accounts with status='active', excludes 'replaced' accounts.
// Issue #668: Plaid Item Management for Cost Optimization
func (r *accountRepository) CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`

	var count int
	err := r.db.QueryRow(ctx, query, linkedItemID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// Delete removes an account from the database with ownership validation.
//
// Security: This method validates ownership via the WHERE clause (id AND user_id must match).
// A user can only delete accounts they own. If the account doesn't exist or belongs to
// a different user, the DELETE will affect 0 rows and ErrAccountNotFound is returned.
//
// Use case: Called when a user manually deletes an individual account from the UI.
// Note: For institution-level deletion, use LinkedItemRepository.Delete which CASCADE deletes all accounts.
func (r *accountRepository) Delete(ctx context.Context, accountID, userID string) error {
	query := `
		DELETE FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	ct, err := r.db.Exec(ctx, query, accountID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// GetByIDAndUser retrieves an account by ID with ownership validation.
func (r *accountRepository) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       user_account_id,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	var account domain.Account
	err := r.db.QueryRow(ctx, query, accountID, userID).Scan(
		&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
		&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
		&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
		&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
		&account.DisplayOrder, &account.IsHidden,
		&account.UserAccountID,
		&account.CreatedAt, &account.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	if err := r.decryptAccountPIIFields(&account); err != nil {
		return nil, err
	}

	return &account, nil
}

// GetByUserAccountIDAndUser retrieves an account by user_account_id with ownership validation.
// Returns ErrAccountNotFound if no account with that user_account_id belongs to userID.
// Used for IDOR protection when a TransferAction carries from_user_account_id/to_user_account_id
// (the stable account reference that survives Plaid re-links). Issue #972: GAP AE.
//
// Note: user_account_id is not unique — re-linked accounts share the same value (one active
// + N replaced). LIMIT 1 with status='active' first ensures a deterministic result and
// returns the current live account when the caller needs the account data.
func (r *accountRepository) GetByUserAccountIDAndUser(ctx context.Context, userAccountID, userID string) (*domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       user_account_id,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1 AND user_id = $2
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END
		LIMIT 1
	`

	var account domain.Account
	err := r.db.QueryRow(ctx, query, userAccountID, userID).Scan(
		&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
		&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
		&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
		&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
		&account.DisplayOrder, &account.IsHidden,
		&account.UserAccountID,
		&account.CreatedAt, &account.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	if err := r.decryptAccountPIIFields(&account); err != nil {
		return nil, err
	}

	return &account, nil
}

// Update updates account customization fields with ownership validation.
func (r *accountRepository) Update(ctx context.Context, account *domain.Account, userID string) error {
	account.UpdatedAt = time.Now()

	query := `
		UPDATE accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	ct, err := r.db.Exec(ctx, query,
		account.Nickname, account.CustomColor, account.CustomIcon,
		account.CustomEmoji, account.DisplayOrder, account.IsHidden, account.UpdatedAt,
		account.ID, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// UpdateDisplayOrders updates the display_order for multiple accounts atomically.
func (r *accountRepository) UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error {
	if len(orders) == 0 {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	query := `
		UPDATE accounts
		SET display_order = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`

	now := time.Now()

	for _, order := range orders {
		ct, err := tx.Exec(ctx, query, order.DisplayOrder, now, order.AccountID, userID)
		if err != nil {
			return err
		}

		if ct.RowsAffected() == 0 {
			return ErrAccountNotFound
		}
	}

	return tx.Commit(ctx)
}

// accountStatusScope is the status predicate GetAccountsWithInstitutionFiltered resolved from
// AccountFilterOptions. Named rather than left as a bare SQL string so the canary counter can be
// derived from the SAME decision the query uses instead of re-deriving it from the flags.
type accountStatusScope int

const (
	// accountStatusActiveOnly is the default: `status = 'active'`, dropping replaced and expired.
	accountStatusActiveOnly accountStatusScope = iota
	// accountStatusActiveAndReplaced is IncludeReplaced: `status IN ('active','replaced')`.
	accountStatusActiveAndReplaced
	// accountStatusReplacedOnly is ReplacedOnly: `status = 'replaced'` — active rows are DROPPED.
	accountStatusReplacedOnly
	// accountStatusAny is AllStatuses: no status predicate (active, replaced AND expired).
	accountStatusAny
)

// resolveAccountStatusScope maps the (independent, non-exclusive) status booleans to exactly one
// predicate. The precedence is MOST SPECIFIC WINS, and it is deliberate rather than incidental:
//
//	ReplacedOnly > AllStatuses > IncludeReplaced > default(active-only)
//
// The only combination where that is observable is ReplacedOnly+AllStatuses, which is
// contradictory ("only replaced" vs "no status filter at all"). It resolves to replaced-only —
// i.e. setting AllStatuses alongside ReplacedOnly does NOT widen the result, it is ignored, and
// active rows are dropped. No caller does this today (the sole ReplacedOnly caller is
// accountService.GetAccountsFiltered's StatusFilter=="replaced" branch, which never sets
// AllStatuses); it is documented and pinned by test rather than left to be rediscovered.
func resolveAccountStatusScope(opts AccountFilterOptions) accountStatusScope {
	switch {
	case opts.ReplacedOnly:
		return accountStatusReplacedOnly
	case opts.AllStatuses:
		return accountStatusAny
	case opts.IncludeReplaced:
		return accountStatusActiveAndReplaced
	default:
		return accountStatusActiveOnly
	}
}

// clause renders the SQL status predicate, appended after `WHERE a.user_id = $1`.
func (s accountStatusScope) clause() string {
	switch s {
	case accountStatusReplacedOnly:
		return " AND a.status = 'replaced'"
	case accountStatusAny:
		return ""
	case accountStatusActiveAndReplaced:
		return " AND a.status IN ('active', 'replaced')"
	default:
		return " AND a.status = 'active'"
	}
}

// dropsReplaced reports whether this scope excludes at least one replaced account — precisely what
// AccountsReplacedFiltered counts ("query executions that excluded at least one replaced account or
// expanded a re-link chain", see observability/metrics.go).
//
// Only the active-only default qualifies. Note what that means for the two edge scopes:
//
//   - accountStatusAny (AllStatuses) drops nothing, so it must stay silent — that decision is
//     already pinned by an integration test (#1537 round 10).
//   - accountStatusReplacedOnly drops ACTIVE rows, not replaced ones, so by the metric's own
//     definition it is also silent. Before this change it INCREMENTED, labelling a replaced-only
//     query as the "accounts_list active-only path" in the family list — a PRE-EXISTING mislabel
//     dating to Issue #1109, not introduced or inherited by this branch's all-statuses work. It is
//     corrected here because deriving the canary from the resolved scope is what removes the
//     fail-open above, and a flags-based special case for ReplacedOnly would have reintroduced the
//     same two-sources-of-truth shape.
func (s accountStatusScope) dropsReplaced() bool {
	return s == accountStatusActiveOnly
}

// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and flexible filtering.
// Issue #679, #680: Also includes linked_item status to show 'disconnected' when subscription expires.
// Issue #1109: Added IncludeReplaced and UserAccountID filter options.
func (r *accountRepository) GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error) {
	// The status predicate is resolved FIRST and the canary is derived FROM it, because the two
	// used to be computed independently from the same booleans and could disagree: the switch
	// matched ReplacedOnly before AllStatuses (so both set yielded `status = 'replaced'`, dropping
	// active rows), while the canary condition `!IncludeReplaced && !AllStatuses` read AllStatuses
	// and stayed silent — a fail-open in the counter whose job is catching fail-opens. Deriving one
	// from the other makes that class of disagreement unrepresentable (Issue #1537).
	statusScope := resolveAccountStatusScope(opts)
	if statusScope.dropsReplaced() {
		observability.AccountsReplacedFiltered.WithLabelValues("accounts_list").Inc()
	}
	hiddenClause := ""
	if !opts.IncludeHidden {
		hiddenClause = " AND a.is_hidden = FALSE"
	}

	statusClause := statusScope.clause()

	userAccountClause := ""
	args := []any{userID}
	if opts.UserAccountID != "" {
		args = append(args, opts.UserAccountID)
		userAccountClause = " AND a.user_account_id = $2"
	}

	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1` + statusClause + hiddenClause + userAccountClause + `
		ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		// Issue #1444 (Site A): l.institution_name and l.status are both nullable columns in
		// linked_items (see domain.Account.InstitutionName/LinkedItemStatus's own "From
		// linked_items JOIN (optional)" comments) but were scanned directly into bare
		// (non-pointer) string fields — "cannot scan NULL into *string". The LEFT JOIN itself
		// can never miss (accounts.linked_item_id is UUID NOT NULL with an FK), but the joined
		// row's own column values can genuinely be NULL, so both must scan through *string
		// intermediates. A NULL status normalizes to "active" via normalizeLinkedItemStatus (the
		// established rule elsewhere in this package); a NULL institution_name surfaces as "".
		var institutionNameRaw, linkedItemStatusRaw *string
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.CreatedAt, &account.UpdatedAt, &institutionNameRaw, &linkedItemStatusRaw,
		)
		if err != nil {
			return nil, err
		}
		if institutionNameRaw != nil {
			account.InstitutionName = *institutionNameRaw
		}
		account.LinkedItemStatus = string(normalizeLinkedItemStatus(linkedItemStatusRaw))
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetByUserAccountAndStatus retrieves accounts by user_account_id and status.
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       user_account_id, status, replaced_at, replaced_by_id,
		       nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1 AND status = $2
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userAccountID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.CreatedAt, &account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetByUserAccountID retrieves all accounts for a user_account (any status).
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, COALESCE(currency_code, 'USD') AS currency_code,
		       user_account_id, status, replaced_at, replaced_by_id,
		       nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.CreatedAt, &account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// UpdateStatus updates an account's status and soft-delete fields with ownership validation.
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error {
	now := time.Now()

	// Set replaced_at only when transitioning to replaced status
	var replacedAt *time.Time
	if status == domain.AccountStatusReplaced {
		replacedAt = &now
	}

	query := `
		UPDATE accounts
		SET status = $1, replaced_at = $2, replaced_by_id = $3, updated_at = $4
		WHERE id = $5 AND user_id = $6
	`

	result, err := r.db.Exec(ctx, query, status, replacedAt, replacedByID, now, accountID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// CheckFingerprintOwnership checks if a fingerprint blind index is already held by a different user.
// Returns ownership info if another active account (owned by a different user) matches the fingerprint.
// Returns nil, nil if the fingerprint is unclaimed or only claimed by the requesting user.
// Issue #1010: Enforce Plaid item uniqueness across all users.
func (r *accountRepository) CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*FingerprintOwnership, error) {
	query := `
		SELECT a.user_id, a.linked_item_id, li.status, li.status_changed_at
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE (a.persistent_account_id_blind_idx = $1 OR a.fallback_fingerprint_blind_idx = $1)
		  AND a.status = 'active'
		  AND a.user_id != $2
		LIMIT 1
	`

	// Issue #1444 (Site B): li.status WAS nullable (linked_items.status) and was scanned directly
	// into ownership.ItemStatus, a bare domain.LinkedItemStatus (string) field — "cannot scan
	// NULL into *string". Scan into a *string intermediate and normalize through
	// normalizeLinkedItemStatus (the established NULL-means-active rule) instead. This backs the
	// Issue #1010 cross-user Plaid-item uniqueness gate (account_service.go's LinkAccounts) —
	// security-relevant, so a NULL status must not silently fail the scan.
	//
	// Migration 000126 (also Issue #1444) has since made the column NOT NULL, so no new NULL can
	// arise. The intermediate is kept deliberately: it costs nothing, it stays correct against an
	// older or partially-migrated database, and removing it would put the panic back one migration
	// rollback away on a security-relevant path.
	var ownership FingerprintOwnership
	var statusRaw *string
	err := r.db.QueryRow(ctx, query, fingerprintBlindIdx, excludeUserID).Scan(
		&ownership.UserID,
		&ownership.LinkedItemID,
		&statusRaw,
		&ownership.StatusChangedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ownership.ItemStatus = normalizeLinkedItemStatus(statusRaw)
	return &ownership, nil
}

// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired'.
// This releases the partial UNIQUE index constraint so a new user can link the same account.
// Issue #1010: Enforce Plaid item uniqueness across all users.
func (r *accountRepository) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error {
	query := `
		UPDATE accounts
		SET status = 'expired', updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
	`
	_, err := r.db.Exec(ctx, query, fingerprintBlindIdx)
	return err
}
