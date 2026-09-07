package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	// ErrAccountFilterAsOfConflict is returned when AccountFilterOptions.AsOf is combined with
	// ReplacedOnly or AllStatuses. Those ask contradictory questions — "which accounts were LIVE
	// at instant T" versus "which accounts are replaced right now" / "every account regardless of
	// status" — and there is no defensible silent winner between them, so the resolver refuses
	// rather than picking one. Issue #1569.
	ErrAccountFilterAsOfConflict = errors.New("AccountFilterOptions.AsOf cannot be combined with ReplacedOnly or AllStatuses")

	// ErrAccountFilterLastKnownAsOfConflict is returned when
	// AccountFilterOptions.IncludeLastKnownBalance is combined with AsOf (Issue #1591).
	//
	// The last-known balance is "the most recent snapshot on record, full stop". An as-of read
	// resolves the account universe at a PAST instant, and answering it with today's snapshot
	// would put an August figure, correctly labelled August, inside a response whose every other
	// number is dated June. Blending the current and historical universes is the regression this
	// whole branch exists to stop, so the combination is refused rather than silently ignored —
	// an ignored option is indistinguishable from a working one at the call site.
	//
	// An AS-OF last-known balance is a coherent question (`snapshot_at <= $asOf` on the same
	// lateral). It is deliberately not answered until a caller asks it.
	ErrAccountFilterLastKnownAsOfConflict = errors.New("AccountFilterOptions.IncludeLastKnownBalance cannot be combined with AsOf")

	// ErrPurgeBlockedBySavingsGoal is returned by PurgeAccount/PurgeLinkedItem when a savings
	// goal still points at one of the accounts being purged. Migration 000150 made
	// savings_goals.account_id ON DELETE RESTRICT specifically so this refusal is enforced by the
	// database, not reimplemented here — this sentinel exists only to translate the resulting
	// Postgres foreign-key-violation error (23503 on fk_savings_goals_account_id) into something
	// the API layer can map to an actionable 409, instead of leaking a raw pgconn.PgError. Issue
	// #1569, Task 6.
	//
	// ⚠ "a savings goal" here means a LIVE one. A TOMBSTONED goal does not reach this error: both
	// purge paths remove it themselves before the delete that would trip the FK (#1569 task 21,
	// defect C1). That carve-out is what makes the 409's advice — "delete the goal first, then try
	// again" — a step the user can actually complete. Between task 8 (goal deletion became a
	// tombstone, which leaves account_id set) and that fix it was not: the goal became invisible to
	// every read, could not be deleted a second time, and the FK fired forever, so explicit
	// destruction was permanently unreachable for any account that had ever had a goal.
	ErrPurgeBlockedBySavingsGoal = errors.New("a savings goal still references this account; delete the goal first")

	// ErrAccountNotDisconnected is returned by PurgeAccount when the account is still 'active'.
	// A purge must never be the FIRST thing that happens to a live, actively-syncing account: the
	// next Plaid sync's MatchOrCreate would recreate the row, silently undoing the "permanent"
	// deletion the user just confirmed. Mirrors ErrLinkedItemNotDisconnected
	// (linked_item_repository.go) for the same reason at the institution level. Issue #1569, Task
	// 6, fix round 1.
	ErrAccountNotDisconnected = errors.New("account must be disconnected before it can be purged")

	// ErrAccountAlreadyDisconnected is returned by UpdateStatus when the account has a DIFFERENT
	// terminal status than the one being requested — currently 'replaced' or 'expired' when the
	// caller asked for 'removed' (or vice versa). This is NOT the idempotency case: a genuinely
	// different transition already happened, so the caller's assumption about the account's state
	// is wrong and a 409 is the right, unambiguous answer. Mirrors ErrAccountNotDisconnected's
	// naming (purge wants NOT active; this wants active) and the purge path's established
	// lock-then-check-then-409 shape. Issue #1569, Task 2.
	ErrAccountAlreadyDisconnected = errors.New("account is already disconnected")

	// ErrAccountAlreadyRemoved is returned by UpdateStatus specifically when the account is
	// already 'removed' — the idempotency case, distinguished from ErrAccountAlreadyDisconnected
	// (fix round 1 of Task 2, per code review): a repeat disconnect's desired end state (removed)
	// is already the account's actual state, so the caller's intent is already satisfied. Callers
	// that consider 'removed' their own terminal goal (DisconnectAccount) should treat this as
	// idempotent success — NOT re-run the write. Nothing gets touched either way: this sentinel is
	// returned from inside the same lock-then-check path as ErrAccountAlreadyDisconnected, before
	// any UPDATE is issued, so removed_at/replaced_at/replaced_by_id all stay exactly as they were.
	ErrAccountAlreadyRemoved = errors.New("account is already removed")

	// ErrAccountNotRestorable is returned by RestoreProviderRemovedAccount when the row is not a
	// provider-initiated removal: it is not 'removed' at all, or it is 'removed' with
	// removed_reason 'user_disconnect' or NULL. Issue #1569 Task 5, fix round 2.
	//
	// This is an EXPECTED, routine outcome, not a failure: every sync asks the question of every
	// retained account under an item, and most answers are no. Callers log it at debug and move on.
	// It exists as a distinct sentinel so that "we declined to restore" can never be mistaken for
	// "the restore failed" — the difference matters, because one of them means a user disconnect
	// was correctly honoured.
	ErrAccountNotRestorable = errors.New("account is not restorable: not a provider-initiated removal")
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

	// AsOf resolves the account universe as it REALLY WAS at an instant, from the recorded
	// intervals in account_status_history, instead of guessing it from current
	// accounts.status. It replaces the active-only default: `a.status = 'active'` becomes
	// "an 'active' interval for this account covered AsOf", with intervals half-open
	// [valid_from, valid_to) — live AT valid_from, NOT live at valid_to.
	//
	// This is what AllStatuses was an approximation of. AllStatuses answers "ever existed in
	// any status", which over-includes (an account opened AFTER the instant counts) and
	// under-explains (it cannot say the account was live then, only that it exists now). A
	// baseline computed over it is right only because the balance comes from a snapshot that
	// happens to exist. AsOf is the recorded truth; prefer it for any new point-in-time read.
	//
	// Combining AsOf with ReplacedOnly or AllStatuses is a programming error and returns
	// ErrAccountFilterAsOfConflict — see resolveAccountStatusScope for the full precedence
	// table. Issue #1569.
	AsOf *time.Time

	// IncludeLastKnownBalance populates domain.Account.LastKnownBalance / LastKnownBalanceAt for
	// TERMINAL accounts only, from the newest balance_snapshots row (Issue #1591). Opt-in, and
	// deliberately so: three callers share this read and only one of them renders accounts to a
	// person. See lastKnownBalanceLateral in account_liveness_sql.go for the full three-decision
	// note — which reads carry it, and why an as-of read must not.
	//
	// Combining it with AsOf returns ErrAccountFilterLastKnownAsOfConflict.
	IncludeLastKnownBalance bool
}

// AccountRepository defines the interface for account data access
type AccountRepository interface {
	// CreateBatch creates multiple accounts in a single transaction for a single tenant.
	// Every element's UserID must equal userID; a mismatch returns
	// ErrAccountBatchTenantMismatch and writes nothing (issue #1566).
	CreateBatch(ctx context.Context, userID string, accounts []domain.Account) error

	// GetByLinkedItemID retrieves all ACTIVE accounts for a specific linked item.
	// Its remaining wipe-and-reinsert callers are holdings and liability syncs
	// (investment_sync_service.go's SyncHoldings, liability_sync_service.go), which write via
	// ReplaceForAccount on every run. Widening this to include non-active statuses would feed
	// a retained (removed/expired/replaced) account into those paths and let a later sync
	// clear or replace its holdings/liabilities — the exact destruction Issue #1569 exists to
	// stop. If a caller needs non-active rows, add a separate method (see
	// GetByLinkedItemIDIncludingRetained) rather than widening this one.
	//
	// It is also used by account_service.go's re-link path to mark the PREDECESSOR item's
	// still-active accounts 'replaced' — active-only there because an already-retained
	// account needs no further status change.
	//
	// ⚠ investment-transaction sync does NOT belong in the wipe-and-reinsert group above:
	// investment_transaction_repository.UpsertBatch is a pure INSERT ... ON CONFLICT DO
	// UPDATE, so it reads GetByLinkedItemIDIncludingRetained instead (Issue #1569 round 11
	// task 1 fixed a prior misclassification that grouped it here). Balance-snapshot sync
	// (transaction_sync_service.go's createBalanceSnapshots) doesn't call this method at all —
	// it reads GetByLinkedItemIDIncludingRetained for an unrelated reason (its account-restore
	// pass needs to see removed rows) and filters to the active subset itself before writing
	// any snapshot.
	GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error)

	// GetByLinkedItemIDIncludingRetained retrieves accounts for a specific linked item
	// REGARDLESS of status (active, replaced, expired, removed). Its callers are
	// transaction_sync_service.buildAccountMaps and createBalanceSnapshots (Issue #1569 Task
	// 1b and Task 5) and investment_sync_service.SyncInvestmentTransactions (Issue #1569
	// round 11 task 1): a Plaid item keeps delivering transactions for an account after the
	// user disconnects it (status 'removed'), or after the 90-day fingerprint TTL releases a
	// hold (status 'expired'), and those transactions are real — the money moved — so they
	// belong in transaction/investment-transaction and spending views like any other.
	// Transactions are stored against the account regardless of status; retained-account
	// visibility in the unified feed is handled separately by retainedAccountVisibilityLeg
	// (transaction_repository.go). createBalanceSnapshots' use is narrower: it needs the
	// non-active rows only to find restore candidates, and filters back down to active
	// accounts before writing any snapshot.
	//
	// Do NOT use this for holdings/liability syncs — see the warning on GetByLinkedItemID
	// above.
	GetByLinkedItemIDIncludingRetained(ctx context.Context, linkedItemID string) ([]domain.Account, error)

	// GetByUserID retrieves all accounts for a specific user
	GetByUserID(ctx context.Context, userID string) ([]domain.Account, error)

	// GetForHouseholdScopeActiveNow retrieves every CURRENTLY-active account shared into the
	// household (visibility='shared', not revoked, owner is an active member), across
	// ALL members — not just requestingUserID. Household balance aggregation must use
	// this instead of GetByUserID, which is single-user and would silently drop every
	// non-requester member's accounts (Codex adversarial review, P0-2). Joint accounts
	// dedup to one row per physical fingerprint (freshest-synced copy AT OR BEFORE
	// grantsAsOf wins). requestingUserID is passed through only to satisfy the shared
	// household CTE's parameter typing.
	//
	// ⚠ WHAT grantsAsOf DOES AND DOES NOT SCOPE (Issue #1569 AC2). It resolves the household
	// SHARE GRANTS and the joint-account fingerprint winner at that instant, and nothing else;
	// account STATUS is read from the live `accounts` row, so an account disconnected since
	// grantsAsOf is absent however live it was then. That is deliberate — see task 12's
	// universes ruling in account_liveness_sql.go — but under the method's previous name
	// (GetForHouseholdScope, with the argument called asOf) it read as a defect, and three
	// call-site comments in dashboard_service.go existed only to warn readers about it. The
	// name now carries that fact, and TestAccountRepository_HouseholdScopeReadNamesDoNotLieAboutAsOf
	// stops the old one coming back. For "which accounts were LIVE at a past instant", use
	// GetForHouseholdScopeLiveAsOf below.
	//
	// grantsAsOf must match the asOf passed to whichever GetSnapshotsAsOfForHousehold call this
	// account map will be paired with, or the two can disagree on which member's copy wins for a
	// joint account (Issue #1369 gap-closure G1). Issue #1369 — Phase 5.
	GetForHouseholdScopeActiveNow(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, grantsAsOf time.Time) ([]domain.Account, error)

	// GetForHouseholdScopeLiveAsOf is GetForHouseholdScopeActiveNow with the `a.status = 'active'`
	// predicate REPLACED by recorded liveness at asOf (account_status_history), rather than
	// removed. It is the correct source for a household baseline "as of" a past date and
	// It answers "was live then", rather than the "ever existed" universe #1537's
	// since-removed all-statuses variant produced, which wrongly admitted accounts opened
	// AFTER asOf. This is the ONLY one of the pair whose date argument scopes status; the
	// grant/dedup half of the contract is identical, and the date must match the asOf of the
	// GetSnapshotsAsOfForHousehold call it is paired with. Callers must NOT use this for a
	// "current" figure: the product rule is that current totals include only linked/active
	// accounts, and passing asOf=now here would readmit an account removed moments ago.
	// Issue #1569.
	GetForHouseholdScopeLiveAsOf(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error)

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

	// GetByUserAccountAndStatus retrieves accounts by user_account_id and status, SCOPED to
	// userID. This is used for querying accounts across re-links using stable user_account
	// reference.
	// Issue #171: Plaid Re-Link Handling
	// Issue #1569 R11 T2: the user_id predicate was added because this query previously had
	// NONE — any caller supplying a raw user_account_id (a client-facing stable identifier)
	// with no ownership check could read another user's accounts. Match the pattern established
	// in investment_sync_service.go's accounts-by-linked-item guard: scope reads that
	// cross a stable-identifier boundary to the authenticated actor.
	//
	// Returns empty slice if no accounts found (not an error).
	GetByUserAccountAndStatus(ctx context.Context, userAccountID, status, userID string) ([]domain.Account, error)

	// GetByUserAccountIDAndUser retrieves an account by user_account_id with ownership validation.
	// Returns ErrAccountNotFound if no account with that user_account_id belongs to userID.
	// Used for IDOR protection when a TransferAction carries from_user_account_id/to_user_account_id
	// (the stable account reference that survives Plaid re-links). Issue #972: GAP AE.
	//
	// Prefers the chain's active member but does not require one — a fully terminal chain still
	// answers, so the returned Account carries Status and RemovedAt and a caller that needs a LIVE
	// account must check them (#1569 residual E2).
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

	// MarkRemovedByProvider soft-removes an account because Plaid stopped returning it from a
	// confirmed-complete /accounts/get — i.e. the bank closed it. Issue #1569 Task 5.
	//
	// Identical to UpdateStatus(..., AccountStatusRemoved, nil) in every respect (same active-only
	// FOR UPDATE guard, same two refusal sentinels) EXCEPT that it stamps
	// removed_reason = 'provider_absent' instead of 'user_disconnect'. That single column is what
	// makes RestoreProviderRemovedAccount able to tell the two removals apart, and it is why this
	// is a separate method rather than a flag: a caller must choose a cause by choosing a method,
	// so no future call site can remove an account without saying who did it.
	MarkRemovedByProvider(ctx context.Context, accountID, userID string) error

	// RestoreProviderRemovedAccount returns a PROVIDER-removed account to 'active', clearing
	// removed_at and removed_reason. Issue #1569 Task 5, fix round 2: a bank-closed account that
	// reappears in a later confirmed-complete roster was not closed after all, and nothing else in
	// this codebase ever writes an account back to 'active'.
	//
	// ⚠ THE CAUSE CHECK IS THE SAFETY PROPERTY, AND IT LIVES IN GO — not, as this comment claimed
	// until #1569's ADR review, in the UPDATE's WHERE clause. The implementation reads
	// `status, removed_reason` under `SELECT … FOR UPDATE`, refuses in a Go `if` unless the row is
	// `status = 'removed'` with a NON-NULL `removed_reason` equal to 'provider_absent', and only
	// then issues an UPDATE whose predicate is just `id = $2 AND user_id = $3`. A user-disconnected
	// row ('user_disconnect') and a legacy row (NULL cause, written before migration 000154) are
	// both refused with ErrAccountNotRestorable — the NULL case needs an explicit nil check
	// precisely BECAUSE it is not SQL equality doing the work.
	//
	// The distinction is not pedantry: the check and the write share one transaction and one
	// FOR UPDATE lock, so the behaviour IS race-safe today, but nothing in the schema or the
	// statement would stop a future caller that reached the UPDATE by another route. Callers may
	// pre-filter for efficiency, but they are not trusted to: auto-resurrecting an account the user
	// deliberately disconnected would invert the whole principle of Issue #1569. If you add a
	// second write path to `status = 'active'`, carry the cause check with it.
	RestoreProviderRemovedAccount(ctx context.Context, accountID, userID string) error

	// CheckFingerprintOwnership checks if a fingerprint blind index is already held by a different user.
	// Returns ownership info (user, linked item, item status) if another user owns the fingerprint.
	// Returns nil, nil if the fingerprint is unclaimed or only claimed by the requesting user.
	// Issue #1010: Enforce Plaid item uniqueness across all users.
	CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*FingerprintOwnership, error)

	// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired',
	// stamping expired_at. This releases the partial UNIQUE index constraint so a new user can link
	// the same account. Called when the 90-day TTL expires for a disconnected/revoked item. Returns
	// the IDs of every account it actually flipped, so the caller can audit-log each one — see the
	// implementation's doc comment for why this is cross-user by design and carries no user_id
	// predicate. Issue #1010 (original), Issue #1569 Task 6 (expired_at + audit trail).
	ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) ([]string, error)

	// MarkRemovedByLinkedItemID transitions every ACTIVE account under a linked item to
	// status='removed' with removed_at stamped, without deleting the row or destroying anything
	// that references it (balance_snapshots, holdings, investment transactions, liability rows,
	// savings_goals). Issue #1569: disconnecting an institution must retain history.
	//
	// `reason` is written to removed_reason and is a REQUIRED parameter rather than a hardcoded
	// 'user_disconnect' (Issue #1569 Task 20). Two callers now exist and they disagree about it:
	// accountService.DisconnectInstitution passes RemovalReasonUserDisconnect, and
	// plaidItemCleanupService passes RemovalReasonLinkRemoved because the user asked for nothing.
	// The parameter is deliberately not defaulted — the wrong value here is not cosmetic, it decides
	// whether RestoreAccount may later resurrect the row.
	MarkRemovedByLinkedItemID(ctx context.Context, linkedItemID string, reason domain.RemovalReason) error

	// PurgeAccount permanently and transactionally destroys accountID and every row derived from
	// it: the account row itself, its balance_snapshots (explicit — migration 000150 made that FK
	// SET NULL, so it does not fall out of a plain account delete), and — via
	// ON DELETE CASCADE, unchanged since account creation — its holdings, investment_transactions,
	// credit/mortgage/student_loan_liabilities, and account_status_history rows. Issue #1569, Task 6.
	//
	// transactions.account_id is ON DELETE SET NULL (migration 000035) and is deliberately left
	// alone: purging an account must not delete the transactions the user categorized and
	// budgeted against. transaction_repository.go's unified feed already renders a NULL
	// account_id row (the "truly orphaned" leg), so those transactions remain visible.
	//
	// Refuses with ErrAccountNotDisconnected, doing and changing nothing, unless the account is
	// already non-'active' (removed/replaced/expired) — a purge must never be the first thing
	// that happens to a live, actively-syncing account, or the next Plaid sync recreates it.
	//
	// Refuses with ErrPurgeBlockedBySavingsGoal, doing and changing nothing, while a savings goal
	// still references this account (migration 000150's RESTRICT constraint on
	// fk_savings_goals_account_id) — the caller must delete the goal first. The constraint itself
	// is the enforcement; this method only translates its violation into a domain sentinel.
	//
	// auditLog, when non-nil, is used to write an "account.history_purged" entry INSIDE the same
	// transaction via LogInTx, so the entry and the destruction it describes commit or roll back
	// together. Safe on this path specifically because the transaction never touches a users row
	// with FOR UPDATE — see AuditLogRepository.LogInTx's doc comment for why that precondition
	// matters.
	//
	// Returns ErrAccountNotFound if the account doesn't exist or isn't owned by userID.
	PurgeAccount(ctx context.Context, accountID, userID string, auditLog AuditLogRepository) error
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

	if err := supersedeStrandedChainSiblings(ctx, tx, userID, accounts, now); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// supersedeStrandedChainSiblings closes the re-link chain: any OTHER account of this user that is
// still 'active' under a user_account_id this batch is creating a row for becomes 'replaced',
// pointing at the row that supersedes it. Issue #1569, task 9.1 item B.
//
// ── WHY THIS IS HERE AND NOT IN THE SERVICE ─────────────────────────────────────────────────
// accountService's re-link already marks the predecessor 'replaced' — and does it FAIL-OPEN: the
// error is logged and the loop continues ("failed to soft-delete old account"), several Plaid
// round-trips BEFORE the successor rows are created. So a transient failure leaves two 'active'
// accounts sharing one user_account_id with nothing recording that it happened. That is precisely
// the state balance_snapshot_repository.go's dedup comment calls impossible — "the Plaid re-link
// handler guarantees [it] never happens (no DB constraint enforces it — see migration 000033)" —
// and while it lasts, every CURRENT-STATE read counts one logical position twice: live holdings
// (holding_repository.go's GetByUserID) and all four SUM(a.current_balance) aggregates in
// investment_analytics_repository.go, each of which scopes on currentStateAccountLeg. A $100
// position reads as $200 and nothing repairs it.
//
// ── WHY NOT JUST FAIL THE RE-LINK ───────────────────────────────────────────────────────────
// Because the failing write is not the one the user asked for. Returning an error from the
// service's step 5b would throw away a bank connection the user just authorised in order to fix a
// bookkeeping row, and #1569's whole bias is retain-and-degrade rather than destroy. Moving the
// enforcement into the transaction that CREATES the second active row inverts that trade: either
// the successor exists and the predecessor is superseded, or neither happened and the user's next
// Link attempt starts from a consistent state. Nothing is destroyed on either branch — 'replaced'
// is the retention tombstone the re-link path already used, and every history read reaches the row
// through it.
//
// ── WHY NOT A REPAIR JOB ────────────────────────────────────────────────────────────────────
// There is no repair path to extend, and this was checked rather than assumed: 'replaced' has
// exactly ONE writer in the whole backend (account_service.go's re-link), and the two sweeps that
// do reconcile account status — plaidItemCleanupService.markAccountsRemoved and the orphaned-item
// retention sweep — both key off the item being gone AT PLAID, which a re-linked predecessor's
// item is not. A stranded sibling is invisible to both, forever. Enforcing the invariant at its
// only write point is smaller than the sweep that would otherwise have to exist, and it is also a
// repair: a sibling stranded by an EARLIER failure is superseded by the next re-link of that chain.
//
// ── SCOPE, and both halves are pinned by TestAccountRepository_CreateBatch_SupersedeTouchesOnlyItsOwnChain ──
// `user_account_id = $2` and nothing looser. A NULL user_account_id — the legacy shape — matches
// nothing, because SQL equality over NULL is NULL: an IS NOT DISTINCT FROM here would supersede
// every identity-less account the user has, in one statement, silently. Rows created by THIS batch
// are excluded by id, so two new rows that happened to share a chain key cannot supersede each
// other. And the UPDATE goes through `accounts` normally, so migration 000152's trigger records
// the transition in account_status_history — without that, accountLiveOnDateClause would keep
// reading the predecessor as live on every future day.
func supersedeStrandedChainSiblings(ctx context.Context, tx pgx.Tx, userID string, accounts []domain.Account, now time.Time) error {
	newIDs := make([]string, 0, len(accounts))
	byChain := make(map[string][]string)
	chainOrder := make([]string, 0, len(accounts))
	for i := range accounts {
		newIDs = append(newIDs, accounts[i].ID)
		if accounts[i].UserAccountID == nil {
			continue
		}
		key := *accounts[i].UserAccountID
		if _, seen := byChain[key]; !seen {
			chainOrder = append(chainOrder, key)
		}
		byChain[key] = append(byChain[key], accounts[i].ID)
	}

	// ⚠ ONE STATEMENT PER CHAIN KEY, NOT ONE PER NEW ROW, and the pairing is the point.
	//
	// `succ` numbers the successors this batch brought for the chain; `cand` numbers the rows it
	// may retire, ACTIVE ONES FIRST. Joining rk to rk does three things at once, and each is a
	// money defect if it is dropped:
	//
	//   - it caps the retirements at the number of successors (#1569 T10.1 item E). A chain can
	//     legitimately hold two live maskless siblings — MatchOrCreate keys on
	//     (user, institution, MASK) and a NULL mask matches every other NULL mask at that
	//     institution — and a batch carrying ONE row for the chain must not retire both. The one
	//     it retires is gone from live holdings and from all four SUM(a.current_balance)
	//     aggregates, and the historical rationing that keeps a sibling on a transition day
	//     CANNOT put it back into current state.
	//   - it gives each retired predecessor a DISTINCT successor (item A). accountChainLiveCTE
	//     rations by count(DISTINCT succ_id), so N predecessors all pointing at successor #1
	//     ration away exactly one of them and the rest are summed beside their own replacements.
	//     That is the original doubling, reached through the mitigation written to fix it.
	//   - it reaches a predecessor a fail-open re-link left 'replaced' with a NULL pointer
	//     (item B / topology 7a). account_service step 5b writes the ITEM pointer and the ACCOUNT
	//     status in two INDEPENDENT fail-open blocks; when only the item write fails the row is
	//     already retired, so a `status = 'active'` filter never sees it, and NEITHER succession
	//     route can identify its successor. Its status does not change here — only the missing
	//     pointer is recorded — so no current-state read moves.
	//
	// Active rows sort first because their duplication is the one visible in current state TODAY;
	// a pointerless 'replaced' row only mis-sums history. When successors run out, the leftovers
	// are left exactly as they were.
	//
	// ⚠ THE TRADE THE CAP MAKES, stated rather than left to be discovered. When a chain holds two
	// live siblings and the batch brings one row for it, capping means the survivor stays 'active'
	// beside the new row — so a chain CAN hold two active accounts. That was never a real
	// invariant anyway (MatchOrCreate's NULL-mask collision produces it legitimately, which is why
	// accountChainLiveCTE is not a DISTINCT ON), and history is unchanged by the cap either way:
	// the rationing already kept exactly one predecessor in the total whether one pointer was
	// recorded or two. What changes is CURRENT state, and only in the retaining direction. The
	// normal re-link path is untouched — account_service step 5b retires every account of the old
	// item explicitly — so the cap only decides the failure paths, where guessing that a sibling
	// was replaced is not something the chain key alone can support.
	//
	// ⚠ A pointerless 'replaced' row of ANY age is eligible for the pointer fill; there is no
	// principled cut-off to apply and an arbitrary one would be worse. Filling one long after the
	// fact is inert: succession pairs require both rows live on the SAME day, and a row retired
	// months ago never overlaps a row created today.
	const supersedeQuery = `
		WITH succ AS (
		    SELECT id, row_number() OVER (ORDER BY id) AS rk
		    FROM unnest($3::uuid[]) AS t(id)
		),
		cand AS (
		    SELECT a.id, row_number() OVER (ORDER BY (a.status <> 'active'), a.id) AS rk
		    FROM accounts a
		    WHERE a.user_id = $1
		      AND a.user_account_id = $2
		      AND a.id <> ALL($5::uuid[])
		      AND (a.status = 'active'
		           OR (a.status = 'replaced' AND a.replaced_by_id IS NULL))
		)
		UPDATE accounts a
		SET status = 'replaced',
		    -- COALESCE, not $4: a row retired by an earlier fail-open write keeps the instant it
		    -- was actually retired, or accountLiveOnDateClause re-dates its live interval.
		    replaced_at = COALESCE(a.replaced_at, $4),
		    replaced_by_id = s.id,
		    updated_at = $4
		FROM cand c
		JOIN succ s ON s.rk = c.rk
		WHERE a.id = c.id
	`

	for _, key := range chainOrder {
		if _, err := tx.Exec(ctx, supersedeQuery,
			userID, key, byChain[key], now, newIDs,
		); err != nil {
			return fmt.Errorf("supersede stranded chain siblings of user_account %s: %w", key, err)
		}
	}
	return nil
}

// GetByLinkedItemID retrieves all accounts for a specific linked item
func (r *accountRepository) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
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

// GetByLinkedItemIDIncludingRetained is GetByLinkedItemID with the `status = 'active'`
// predicate removed. See the interface doc comment (AccountRepository) for why this is a
// SEPARATE method rather than a widening of GetByLinkedItemID: holdings and liability syncs
// wipe-and-reinsert on every run, and feeding them a retained account would let a later
// sync destroy its holdings/liabilities.
//
// Deliberately NOT re-listing callers here: the interface doc comment above is the one place
// that enumerates them, and a second enumeration is how this exact drift happened (Issue
// #1569 round 11 fix-round-2 found this copy silently missing createBalanceSnapshots after
// round 11 task 1 added a caller to the interface comment but not here).
func (r *accountRepository) GetByLinkedItemIDIncludingRetained(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       removed_reason,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1
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
			// Issue #1569 Task 5: this is the ONLY read that populates RemovedReason — see that
			// field's doc comment for why, and why nil is the safe default everywhere else.
			&account.RemovedReason,
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
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
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

// GetForHouseholdScopeActiveNow retrieves every CURRENTLY-active account shared into the
// household across all its members. Uses the date-bounded balance-oriented household CTE
// (freshest-synced copy AT OR BEFORE grantsAsOf wins for joint-account fingerprint collisions —
// see householdAccountsCTEForBalancesAsOf) since account metadata backs balance/net-worth
// aggregation, not transaction history. grantsAsOf bounds the share grants and that dedup and
// NOT account status — see the interface doc for the full contract and for why the method is
// no longer called GetForHouseholdScope. Issue #1369 — Phase 5; renamed by Issue #1569 AC2.
func (r *accountRepository) GetForHouseholdScopeActiveNow(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, grantsAsOf time.Time) ([]domain.Account, error) {
	return r.getForHouseholdScope(ctx, requestingUserID, scope, grantsAsOf, householdActiveOnly)
}

// GetForHouseholdScopeLiveAsOf is GetForHouseholdScopeActiveNow with the unconditional
// `a.status = 'active'` predicate REPLACED by recorded liveness at asOf, read from
// account_status_history.
//
// This is what the other read's date argument USED to imply, back when that method was called
// GetForHouseholdScope and its argument was called asOf: it threads the date into the
// joint-account dedup CTE and then filters on CURRENT status anyway, so a member's account
// disconnected since the cutoff is absent. Issue #1569 AC2 renamed that method rather than
// widening it — the two universes stay separate (account_liveness_sql.go) and the name now says
// which one you are asking. #1537's workaround was an all-statuses variant (take
// every status and let the snapshot decide); this is the recorded answer, and it additionally
// EXCLUDES accounts that did not exist yet at asOf, which all-statuses could not — which is why
// that variant was removed rather than kept beside this one.
// Issue #1569.
func (r *accountRepository) GetForHouseholdScopeLiveAsOf(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error) {
	return r.getForHouseholdScope(ctx, requestingUserID, scope, asOf, householdLiveAsOf)
}

// householdStatusFilter selects which account statuses a household-scope read returns. It is a
// typed flag rather than a SQL fragment on purpose: this helper used to take the WHERE clause
// itself as a string and splice it in, which was safe only for as long as every call site was
// inspected and stayed a compile-time literal. A closed, finite type makes an injected
// predicate impossible by construction, not merely absent today. Issue #1537.
type householdStatusFilter int

const (
	// householdActiveOnly restricts to `a.status = 'active'` — CURRENT status, regardless of the
	// query's date argument (GetForHouseholdScopeActiveNow, and its name says so).
	householdActiveOnly householdStatusFilter = iota
	// householdLiveAsOf restricts to accounts that account_status_history records as having been
	// 'active' at the query's asOf instant (GetForHouseholdScopeLiveAsOf). Issue #1569.
	householdLiveAsOf
)

// statusClause renders the SQL predicate for this filter, appended after `WHERE TRUE`. These
// strings are the only status SQL any household-scope read can produce.
//
// householdLiveAsOf binds $4 — NOT a newly appended argument. getForHouseholdScope already appends
// asOf as the fourth arg after householdScopeArgs' three (requesting user, household, member IDs),
// and householdAccountsCTEForBalancesAsOf already reads it as $4. Adding asOf to householdScopeArgs
// as well would double-bind it and shift every other placeholder. Issue #1569.
func (f householdStatusFilter) statusClause() string {
	switch f {
	case householdLiveAsOf:
		return accountLiveAsOfPredicate("a", 4)
	default:
		return " AND a.status = 'active'"
	}
}

// householdScopeQuery builds the household-scope account SELECT. Pure and separate from the
// read itself so the exact SQL each filter produces can be asserted without a database.
func householdScopeQuery(statuses householdStatusFilter) string {
	return "WITH " + householdAccountsCTEForBalancesAsOf() + `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
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
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at, a.removed_at,
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
			&account.RemovedAt,
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

// CountByUserID counts a user's accounts: one row per account the user actually has.
//
// `status = 'active'` rather than a NOT IN list, and the difference is 'replaced' (#1569 residual
// E10). The three non-active statuses are all the same kind of thing here — a row the schema
// keeps that is not an account the user has:
//
//	expired  — the 90-day fingerprint TTL released the hold (#1010).
//	removed  — the user disconnected it; retained, not deleted (#1569).
//	replaced — the PRE-RE-LINK COPY of an account whose successor is the active row beside it,
//	           sharing one user_account_id. Counting both reports two accounts where the user has
//	           one, and a re-link is a routine event, not an edge case.
//
// The earlier NOT IN ('expired', 'removed') form excluded the two that had been noticed and kept
// the one that had not. CountActiveByLinkedItemID (below) has counted 'active' only since #668.
//
// NOTE (#1569, Task 6): this method has NO production caller today — the only real subscription
// limit in this codebase is transaction_history_days (config/subscription_tiers.yaml); there is no
// account-count entitlement. It is hygiene against a future caller, and
// TestAccountRepository_CountByUserID_Integration_CountsOneRowPerRealAccount (a LIVE-Postgres
// test, because this method's two pgxmock tests supply their own answer and cannot see a wrong
// WHERE clause) is what keeps it honest.
func (r *accountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status = 'active'
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

// isForeignKeyViolation reports whether err is a Postgres 23503 foreign_key_violation on the
// named constraint. Mirrors isUniqueViolation (household_repository.go) for the 23505 case —
// translates a raw pgconn.PgError into something a caller can act on without parsing SQLSTATEs
// itself. Issue #1569, Task 6: used to turn a RESTRICT-constraint failure during a purge into
// ErrPurgeBlockedBySavingsGoal.
func isForeignKeyViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraintName
}

// PurgeAccount implements AccountRepository.PurgeAccount — see that method's doc comment for the
// full contract (what gets destroyed, what survives, and why the audit write shares this
// transaction).
func (r *accountRepository) PurgeAccount(ctx context.Context, accountID, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	// Lock the row and check status BEFORE doing anything destructive. Mirrors
	// PurgeLinkedItem's plaid_removed_at guard: a purge must never be the FIRST thing that
	// happens to a LIVE, actively-syncing account — Plaid would recreate the row on its next sync
	// via MatchOrCreate, and the "permanent" deletion the user just confirmed would silently
	// resurrect itself. FOR UPDATE here is a row lock on accounts, not users, so it cannot
	// participate in the users-row lock cycle LogInTx's doc comment warns about.
	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		accountID, userID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("purge account %s: %w", accountID, err)
	}
	if status == string(domain.AccountStatusActive) {
		return ErrAccountNotDisconnected
	}

	// accounts.replaced_by_id -> accounts(id) has NO explicit ON DELETE action (defaults to NO
	// ACTION, migration 000033). If an OLDER account was replaced by this one on a re-link
	// (older.replaced_by_id = accountID), the accounts DELETE below would fail with a dangling
	// foreign-key violation as an opaque error. Mirrors DeleteUserCascade's identical fix
	// (profile_repository.go) for the same column. Nulling unconditionally is harmless: the
	// column is nullable and carries no meaning once its target is gone.
	if _, err := tx.Exec(ctx, `UPDATE accounts SET replaced_by_id = NULL WHERE replaced_by_id = $1`, accountID); err != nil {
		return fmt.Errorf("purge account %s: clearing dangling replaced_by_id references: %w", accountID, err)
	}

	// balance_snapshots.account_id is ON DELETE SET NULL (migration 000150), so it will NOT be
	// removed by the accounts DELETE below — it must be deleted explicitly, before the account
	// row disappears out from under this query.
	if _, err := tx.Exec(ctx, `DELETE FROM balance_snapshots WHERE account_id = $1`, accountID); err != nil {
		return fmt.Errorf("purge account %s: deleting balance snapshots: %w", accountID, err)
	}

	// TOMBSTONED savings goals go with the account; LIVE ones still block it. Issue #1569, task 21
	// (defect C1).
	//
	// Migration 000150 made savings_goals.account_id RESTRICT precisely so the accounts DELETE
	// below would refuse while a goal pointed at it, and the 409 that refusal maps to tells the
	// user to delete the goal first. Task 8 then made goal deletion a TOMBSTONE, which leaves
	// account_id set — so doing what the error asked changed nothing, the FK fired again, and the
	// goal was by then invisible to every read and could not be deleted a second time. The one
	// sanctioned way to destroy data was permanently unreachable for any account that had ever had
	// a savings goal.
	//
	// The tombstone is exactly PurgeGoal's own precondition (ErrSavingsGoalNotDeleted), so a user
	// who reaches here has already performed the two explicit acts that endpoint demands, plus the
	// purge confirmation itself. Nothing becomes destructible that POST /savings-goals/{id}/purge
	// could not already destroy on the same row; the CASCADE that carries the goal's
	// savings_goal_contributions, budget_categories and savings_goal_category_detach rows is the
	// same one PurgeGoal relies on.
	//
	// A goal with deleted_at IS NULL is deliberately NOT matched here. It still trips the FK below
	// and still returns ErrPurgeBlockedBySavingsGoal — which is what keeps that error's text true:
	// deleting the goal now really does unblock the retry.
	if _, err := tx.Exec(ctx, `
		-- tombstone-exempt(write:savings_goals): the predicate is INVERTED on purpose and is right above --
		-- deleted_at IS NOT NULL. This is the purge finishing the destruction the tombstone
		-- deferred; a live goal is deliberately left to trip the FK and return
		-- ErrPurgeBlockedBySavingsGoal (#1569 T8 write-target audit).
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
	`, accountID, userID); err != nil {
		return fmt.Errorf("purge account %s: deleting tombstoned savings goals: %w", accountID, err)
	}

	// Deleting the account row itself CASCADEs to holdings, investment_transactions, the three
	// liability tables, and account_status_history (all still ON DELETE CASCADE — see migration
	// 000151's header, which anticipated exactly this explicit-purge path). transactions.account_id
	// is ON DELETE SET NULL and is untouched: those rows survive, orphaned, and remain visible via
	// the unified feed's NULL-account_id leg.
	//
	// `AND status <> $3` restates the SELECT ... FOR UPDATE's `status == active -> refuse` check
	// (#1569 round 14) so it is mechanically pinned by
	// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate, not just enforced by a Go `if` no
	// guard can see. The row lock above still prevents any concurrent write from flipping status
	// back to active between that SELECT and this DELETE; restating the leg here is defense against
	// a future EDIT of this function removing the Go check, not against a race. If it ever
	// disagreed with the Go check, this DELETE would affect zero rows and fail closed below rather
	// than destroying a live account.
	ct, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1 AND user_id = $2 AND status <> $3`,
		accountID, userID, string(domain.AccountStatusActive))
	if err != nil {
		if isForeignKeyViolation(err, "fk_savings_goals_account_id") {
			return ErrPurgeBlockedBySavingsGoal
		}
		return fmt.Errorf("purge account %s: %w", accountID, err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAccountNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "account.history_purged", "account", accountID); err != nil {
			return fmt.Errorf("purge account %s: audit log: %w", accountID, err)
		}
	}

	return tx.Commit(ctx)
}

// GetByIDAndUser retrieves an account by ID with ownership validation.
//
// Selects `status` (Issue #1569): callers like savings_progress_service.go need to tell a
// retained (replaced/expired/removed) account apart from an active one — this query applies NO
// status predicate itself (deliberately: a caller may need to look up a since-disconnected
// account, e.g. to explain why its balance is frozen), so returning the status is what lets
// them make that call themselves instead of silently treating every result as active.
//
// Selects `replaced_at`/`replaced_by_id` too, and the reason is NOT a Go caller — there is none
// (#1569 residual E2, review finding F1). This read's result is returned over the wire verbatim:
// accountService.UpdateAccount fetches through it and PlaidHandler.UpdateAccount assigns the
// account straight into the PATCH /api/v1/accounts/{id} response body. domain.Account's fields are
// `json:",omitempty"`, and apps/web/src/features/accounts/utils.ts's getAccountClosedDate
// reads `removed_at ?? replaced_at ?? expired_at ?? updated_at` — so omitting replaced_at did not
// merely hide a column, it made the client fall through to updated_at and label a REPLACED
// account's frozen balance with the timestamp of the edit that had just been made. Any read whose
// rows reach that helper must carry all FOUR; "no Go consumer" is not the test.
//
// ⚠ `expired_at` joined the list in #1569 round 6 (G-F), and it was the SAME defect one status
// along: 'expired' had no dedicated column at all until migration 000155 on this branch, so it
// always fell through to updated_at — and this read is the one PATCH /api/v1/accounts/{id}
// returns verbatim, meaning the rename request itself was where the wrong date first appeared.
// Pinned by TestAccountRepository_ExpiredAtSurvivesACosmeticWrite_Integration.
func (r *accountRepository) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       a.user_account_id, a.status, a.removed_at, a.replaced_at, a.expired_at, a.replaced_by_id,
		       a.created_at, a.updated_at
		FROM accounts a` + accountCustomizationJoin("a") + `
		WHERE a.id = $1 AND a.user_id = $2
	`

	var account domain.Account
	err := r.db.QueryRow(ctx, query, accountID, userID).Scan(
		&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
		&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
		&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
		&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
		&account.DisplayOrder, &account.IsHidden,
		&account.UserAccountID, &account.Status, &account.RemovedAt,
		&account.ReplacedAt, &account.ExpiredAt, &account.ReplacedByID,
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
// + N replaced/removed). LIMIT 1 with status='active' first ensures a deterministic result and
// PREFERS the current live account — but it does not guarantee one: a chain whose every member
// is terminal still returns a row, which is correct (the identity is still the user's) and is
// exactly why status and removed_at are selected (#1569 residual E2). The query already sorts on
// status; withholding it from the result left the caller unable to tell which case it got, and
// left Account.Status at its zero value "" — a value no accounts row can ever hold, so a caller
// comparing against AccountStatusActive would silently read every account as not-live.
func (r *accountRepository) GetByUserAccountIDAndUser(ctx context.Context, userAccountID, userID string) (*domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       user_account_id, status, removed_at,
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
		&account.UserAccountID, &account.Status, &account.RemovedAt,
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
	// accountStatusLiveAsOf is AsOf: recorded liveness at an instant, read from
	// account_status_history rather than inferred from the current accounts.status. Issue #1569.
	accountStatusLiveAsOf
)

// resolveAccountStatusScope maps the (independent, non-exclusive) status options to exactly one
// predicate. The precedence is MOST SPECIFIC WINS, and it is deliberate rather than incidental:
//
//	AsOf(+IncludeReplaced) > ReplacedOnly > AllStatuses > IncludeReplaced > default(active-only)
//
// with two combinations REJECTED outright rather than resolved. The full table:
//
//	AsOf set, ReplacedOnly set      -> ErrAccountFilterAsOfConflict
//	AsOf set, AllStatuses set       -> ErrAccountFilterAsOfConflict
//	AsOf set (alone or with IncludeReplaced) -> accountStatusLiveAsOf
//	ReplacedOnly                    -> accountStatusReplacedOnly
//	AllStatuses                     -> accountStatusAny
//	IncludeReplaced                 -> accountStatusActiveAndReplaced
//	none of the above               -> accountStatusActiveOnly
//
// Why AsOf is rejected against ReplacedOnly/AllStatuses instead of being ranked against them: both
// ask a question about CURRENT status ("which are replaced right now", "all of them regardless"),
// while AsOf asks a question about a PAST instant. Either silent winner would return a universe
// the caller did not ask for, and the whole point of #1569 is to stop answering point-in-time
// questions with current-state guesses. IncludeReplaced is different: it merely widens the
// active-only default, and a since-replaced account is already covered by AsOf — its own 'active'
// interval was open at any instant before the re-link — so AsOf subsumes it rather than
// contradicting it.
//
// The pre-existing ReplacedOnly+AllStatuses case remains as it was: contradictory ("only replaced"
// vs "no status filter at all") and resolved to replaced-only — setting AllStatuses alongside
// ReplacedOnly does NOT widen the result, it is ignored, and active rows are dropped. No caller
// does this today (the sole ReplacedOnly caller is accountService.GetAccountsFiltered's
// StatusFilter=="replaced" branch, which never sets AllStatuses); it is documented and pinned by
// test rather than left to be rediscovered. It is NOT promoted to an error here because doing so
// would change the behaviour of a path this issue is not touching.
func resolveAccountStatusScope(opts AccountFilterOptions) (accountStatusScope, error) {
	if opts.AsOf != nil {
		if opts.ReplacedOnly || opts.AllStatuses {
			return 0, ErrAccountFilterAsOfConflict
		}
		return accountStatusLiveAsOf, nil
	}
	switch {
	case opts.ReplacedOnly:
		return accountStatusReplacedOnly, nil
	case opts.AllStatuses:
		return accountStatusAny, nil
	case opts.IncludeReplaced:
		return accountStatusActiveAndReplaced, nil
	default:
		return accountStatusActiveOnly, nil
	}
}

// accountLiveAsOfPredicate renders the recorded-liveness EXISTS predicate for an account aliased
// `alias`, testing the instant bound to placeholder $asOfPH. Shared by the personal
// (accountStatusScope.clause) and household (householdStatusFilter.statusClause) reads so the two
// cannot drift: a difference between them would mean the personal and household dashboards
// disagreed about which accounts existed on the same day. Half-open [valid_from, valid_to) —
// see migration 000151. Issue #1569.
func accountLiveAsOfPredicate(alias string, asOfPH int) string {
	return fmt.Sprintf(
		" AND EXISTS (SELECT 1 FROM account_status_history h"+
			" WHERE h.account_id = %s.id AND h.status = 'active'"+
			" AND h.valid_from <= $%d AND (h.valid_to IS NULL OR h.valid_to > $%d))",
		alias, asOfPH, asOfPH)
}

// clause renders the SQL status predicate, appended after `WHERE a.user_id = $1`. asOfPH is the
// argument placeholder holding the as-of instant; it is read ONLY by accountStatusLiveAsOf, so
// every other scope is safe to render with asOfPH = 0 (no as-of argument bound).
func (s accountStatusScope) clause(asOfPH int) string {
	switch s {
	case accountStatusReplacedOnly:
		return " AND a.status = 'replaced'"
	case accountStatusAny:
		return ""
	case accountStatusActiveAndReplaced:
		return " AND a.status IN ('active', 'replaced')"
	case accountStatusLiveAsOf:
		return accountLiveAsOfPredicate("a", asOfPH)
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
//   - accountStatusLiveAsOf (AsOf) resolves the universe as it really was at its instant, so it
//     drops nothing that was live then — nothing to warn about, same reasoning as AllStatuses.
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
	statusScope, err := resolveAccountStatusScope(opts)
	if err != nil {
		return nil, err
	}
	// Refused, not ignored (Issue #1591): "the newest snapshot" is not an as-of answer, and a
	// silently-dropped option looks identical to a working one from the call site.
	if opts.IncludeLastKnownBalance && opts.AsOf != nil {
		return nil, ErrAccountFilterLastKnownAsOfConflict
	}
	if statusScope.dropsReplaced() {
		observability.AccountsReplacedFiltered.WithLabelValues("accounts_list").Inc()
	}
	hiddenClause := ""
	if !opts.IncludeHidden {
		// #1609: the FILTER reads the same overlay the projection does. Reading a.is_hidden here
		// while projecting the chain's value is the worst of both — the list shows the row as
		// hidden and keeps returning it.
		hiddenClause = " AND " + accountHiddenExpr("a") + " = FALSE"
	}

	userAccountClause := ""
	args := []any{userID}
	if opts.UserAccountID != "" {
		args = append(args, opts.UserAccountID)
		userAccountClause = " AND a.user_account_id = $2"
	}

	// Appended AFTER the optional user_account_id arg so its placeholder is whatever position it
	// actually lands in ($2 or $3), never a hardcoded number that silently shifts when the other
	// optional filter is present.
	asOfPH := 0
	if statusScope == accountStatusLiveAsOf {
		args = append(args, *opts.AsOf)
		asOfPH = len(args)
	}
	statusClause := statusScope.clause(asOfPH)

	// The projection and the join move together — see lastKnownBalanceProjection's comment. Both
	// are empty unless the caller opted in, so the shared callers (dashboard baseline, transaction
	// enrichment) issue byte-for-byte the query they issued before Issue #1591.
	lastKnownProjection, lastKnownJoin := "", ""
	if opts.IncludeLastKnownBalance {
		lastKnownProjection = ", " + lastKnownBalanceProjection
		lastKnownJoin = lastKnownBalanceLateralSQL("a")
	}

	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       ` + accountBalanceProjection("a") + `, COALESCE(a.currency_code, 'USD') AS currency_code,
		       ` + accountCustomizationProjection("a") + `,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id, a.removed_at, a.expired_at,
		       a.created_at, a.updated_at, l.institution_name, l.status` + lastKnownProjection + `
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id` + accountCustomizationJoin("a") + lastKnownJoin + `
		WHERE a.user_id = $1` + statusClause + hiddenClause + userAccountClause + `
		ORDER BY COALESCE(` + accountDisplayOrderExpr("a") + `, 999999) ASC, a.created_at ASC`

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
		dest := []any{
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID, &account.RemovedAt, &account.ExpiredAt,
			&account.CreatedAt, &account.UpdatedAt, &institutionNameRaw, &linkedItemStatusRaw,
		}
		// Appended in the same `if` shape as the projection so the scan list and the select list
		// cannot disagree about their length — the failure pgx reports as an unhelpful
		// "number of field descriptions must equal number of destinations".
		if opts.IncludeLastKnownBalance {
			dest = append(dest, &account.LastKnownBalance, &account.LastKnownBalanceAt)
		}
		if err := rows.Scan(dest...); err != nil {
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

// GetByUserAccountAndStatus retrieves accounts by user_account_id and status, scoped to userID.
// Issue #171: Plaid Re-Link Handling
// Issue #1569 R11 T2: added the user_id predicate — see the interface doc comment.
func (r *accountRepository) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status, userID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
		       user_account_id, status, replaced_at, replaced_by_id,
		       nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1 AND status = $2 AND user_id = $3
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userAccountID, status, userID)
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
		       ` + accountBalanceProjection("") + `, COALESCE(currency_code, 'USD') AS currency_code,
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
//
// Scoped to a currently-'active' row, enforced via SELECT ... FOR UPDATE before the write
// (Issue #1569, Task 2) — mirrors MarkRemovedByLinkedItemID's `AND status = 'active'` scoping,
// which this method was the inconsistent outlier against. Without the guard, a REPEAT call (a
// second disconnect, or a re-link's replace step racing another writer) unconditionally re-stamped
// removed_at/replaced_at = now() even though the history trigger correctly wrote no new history row
// for the no-op status transition — so the true disconnect date survived only in
// account_status_history, which nothing reads for the "as of <date>" label. Worse, transitioning an
// already-'replaced' account to 'removed' nulled its replaced_at/replaced_by_id, destroying the
// re-link chain. Never silently no-ops or re-writes: refuses instead, via one of two distinct
// sentinels (fix round 1 of Task 2) depending on what the row's current status actually is —
// ErrAccountAlreadyRemoved when it is already 'removed' (the idempotency case: the caller's
// desired end state is already reality, so DisconnectAccount treats this as success, not an
// error), or ErrAccountAlreadyDisconnected when it is 'replaced'/'expired' (a genuinely different
// transition already happened, so this is a hard 409 — matching the purge path's already-shipped
// ErrAccountNotDisconnected precondition-check-then-409 shape; see PurgeAccount, same file).
func (r *accountRepository) UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error {
	// The user-initiated cause. Every existing caller of UpdateStatus that transitions to 'removed'
	// is a user disconnect (DisconnectAccount); the provider path uses MarkRemovedByProvider, which
	// is the same statement with the other cause. Issue #1569 Task 5, fix round 2.
	return r.updateStatusWithRemovalReason(ctx, accountID, userID, status, replacedByID, domain.RemovalReasonUserDisconnect)
}

// MarkRemovedByProvider implements AccountRepository.MarkRemovedByProvider — see that method's doc
// comment. Deliberately a thin alias over the same statement UpdateStatus uses, so the two removal
// causes cannot drift apart on locking, guard behaviour, or which columns they stamp.
func (r *accountRepository) MarkRemovedByProvider(ctx context.Context, accountID, userID string) error {
	return r.updateStatusWithRemovalReason(ctx, accountID, userID, domain.AccountStatusRemoved, nil, domain.RemovalReasonProviderAbsent)
}

// updateStatusWithRemovalReason is the single implementation behind UpdateStatus and
// MarkRemovedByProvider. removalReason is stamped ONLY when transitioning to 'removed'; every other
// transition writes NULL, which keeps migration 000154's accounts_removed_reason_valid CHECK
// satisfied by construction rather than by each caller remembering.
func (r *accountRepository) updateStatusWithRemovalReason(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string, removalReason domain.RemovalReason) error {
	now := time.Now()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	var currentStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		accountID, userID,
	).Scan(&currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("update account %s status: %w", accountID, err)
	}
	if currentStatus != string(domain.AccountStatusActive) {
		// 'removed' is the idempotency case (fix round 1 of Task 2): the account is already in
		// the one terminal state DisconnectAccount itself would produce, so return the distinct
		// sentinel that lets that caller treat this as "already done" rather than an error.
		// 'replaced'/'expired' are a genuinely different transition — ErrAccountAlreadyDisconnected
		// stays a hard refusal for those.
		if currentStatus == string(domain.AccountStatusRemoved) {
			return ErrAccountAlreadyRemoved
		}
		return ErrAccountAlreadyDisconnected
	}

	// Set replaced_at only when transitioning to replaced status
	var replacedAt *time.Time
	if status == domain.AccountStatusReplaced {
		replacedAt = &now
	}

	// Set removed_at only when transitioning to removed status (Issue #1569). Required by
	// migration 000149's accounts_removed_at_consistency CHECK: status='removed' demands
	// removed_at IS NOT NULL, mirroring the replaced_at pairing above.
	//
	// removed_reason is stamped in the same breath, and NULLed for every other transition —
	// migration 000154's accounts_removed_reason_valid CHECK requires the two to agree, and pairing
	// them in one statement is what makes that impossible to violate. Issue #1569 Task 5.
	var removedAt *time.Time
	var removedReason *string
	if status == domain.AccountStatusRemoved {
		removedAt = &now
		reason := string(removalReason)
		removedReason = &reason
	}

	query := `
		UPDATE accounts
		SET status = $1, replaced_at = $2, replaced_by_id = $3, removed_at = $4, removed_reason = $5, updated_at = $6
		WHERE id = $7 AND user_id = $8
	`

	result, err := tx.Exec(ctx, query, status, replacedAt, replacedByID, removedAt, removedReason, now, accountID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrAccountNotFound
	}

	return tx.Commit(ctx)
}

// RestoreProviderRemovedAccount implements AccountRepository.RestoreProviderRemovedAccount — read
// that method's doc comment for the safety argument before touching the predicate below.
//
// Shaped exactly like updateStatusWithRemovalReason: SELECT ... FOR UPDATE, decide, then write, all
// in one transaction, so a concurrent disconnect cannot slip between the check and the UPDATE and
// have this method resurrect an account the user removed a millisecond ago.
func (r *accountRepository) RestoreProviderRemovedAccount(ctx context.Context, accountID, userID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	var currentStatus string
	var removedReason *string
	err = tx.QueryRow(ctx,
		`SELECT status, removed_reason FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		accountID, userID,
	).Scan(&currentStatus, &removedReason)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("restore account %s: %w", accountID, err)
	}

	// ⚠ The one check that matters. NULL (a legacy 'removed' row, written before migration 000154)
	// and 'user_disconnect' both land here, and both are refused: a user disconnect must never be
	// undone by a sync, and a row whose cause was never recorded cannot be proven to be anything
	// else. Only an explicit 'provider_absent' passes.
	if currentStatus != string(domain.AccountStatusRemoved) ||
		removedReason == nil ||
		*removedReason != string(domain.RemovalReasonProviderAbsent) {
		return ErrAccountNotRestorable
	}

	// Clearing removed_at alongside the status is NOT required by migration 000149's
	// accounts_removed_at_consistency CHECK — that constraint is one-directional and tolerates a
	// stamped removed_at on a non-'removed' row (#1569 task 7.9). This line IS the enforcement: a
	// live account whose removed_at still reads six months ago is a lie about current state, and
	// the schema will not catch it. Any new path back to 'active' must clear it too.
	// removed_reason is different — 000154's CHECK really does forbid it off a 'removed' row.
	result, err := tx.Exec(ctx, `
		UPDATE accounts
		SET status = 'active', removed_at = NULL, removed_reason = NULL, updated_at = $1
		WHERE id = $2 AND user_id = $3
	`, time.Now(), accountID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrAccountNotFound
	}

	return tx.Commit(ctx)
}

// MarkRemovedByLinkedItemID transitions every ACTIVE account under a linked item to
// status='removed' with removed_at stamped, WITHOUT deleting the row or anything that
// references it (balance_snapshots, holdings, investment transactions, liability rows,
// savings_goals). Issue #1569: disconnecting an institution must retain its accounts'
// financial history.
//
// Scoped to status = 'active' only, deliberately: an already-'replaced' or 'expired' account
// carries a more specific terminal reason for why it stopped being active, and a blanket
// disconnect must not overwrite that with the less specific 'removed'. Mirrors
// ExpireAccountsByFingerprint's `AND status = 'active'` scoping above.
func (r *accountRepository) MarkRemovedByLinkedItemID(ctx context.Context, linkedItemID string, reason domain.RemovalReason) error {
	// The reason is the CALLER's to state — see the interface method's doc comment. It used to be
	// hardcoded 'user_disconnect' here, which was true of the only caller that existed then
	// (DisconnectInstitution) and became a lie the moment plaidItemCleanupService started calling it
	// (Issue #1569 Task 20).
	//
	// Whichever value arrives, these rows are NOT eligible for the provider auto-restore:
	// RestoreAccount accepts 'provider_absent' and nothing else, and no caller of this method passes
	// that. Refused here as well, rather than left to the caller's discretion, because a
	// 'provider_absent' written on this path would mean "the bank closed it" about accounts the bank
	// is still reporting — and would make them silently resurrectable.
	if reason == domain.RemovalReasonProviderAbsent {
		return fmt.Errorf("MarkRemovedByLinkedItemID: refusing to mark accounts under item %s as %q — "+
			"this path is never the provider dropping an account, and that reason is the only one "+
			"RestoreAccount will act on", linkedItemID, reason)
	}
	if reason == "" {
		return fmt.Errorf("MarkRemovedByLinkedItemID: a removal reason is required for item %s", linkedItemID)
	}

	query := `
		UPDATE accounts
		SET status = 'removed', removed_at = NOW(), removed_reason = $2, updated_at = NOW()
		WHERE linked_item_id = $1 AND status = 'active'
	`
	_, err := r.db.Exec(ctx, query, linkedItemID, string(reason))
	return err
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

// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired'. This
// releases the partial UNIQUE index constraint so a new user can link the same account.
// Issue #1010: Enforce Plaid item uniqueness across all users.
//
// ⚠ CROSS-USER BY DESIGN, WITH NO user_id PREDICATE. The whole point of this method is to release a
// fingerprint another user's dormant accounts are holding, so it must reach rows the acting caller
// does not own. Do not add a user scope — see services/account_service.go's checkFingerprintOwnership,
// the sole caller, which looks up the owning fingerprint EXCLUDING the acting user precisely so this
// bulk UPDATE can act on the other user's rows.
//
// Stamps expired_at (Issue #1569 Task 6, migration 000155) — the timestamp 'expired' lacked while
// 'replaced'/'removed' each had one, forcing 000152's account_status_history backfill to approximate
// with updated_at. Returns the IDs of every account this call actually flipped (via RETURNING, not a
// second SELECT — no window for a concurrent write to desync the two), so the caller can write one
// audit_logs entry per affected account. Empty/nil, not an error, when nothing matched (already
// non-active, or the fingerprint is unclaimed) — mirrors UpdateStatus's per-row idempotency guard in
// spirit, but as a bulk statement there is no single "current status" to compare against or a
// sentinel to return for, so a no-op is silent success rather than an error.
func (r *accountRepository) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) ([]string, error) {
	query := `
		UPDATE accounts
		SET status = 'expired', expired_at = NOW(), updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
		RETURNING id
	`
	rows, err := r.db.Query(ctx, query, fingerprintBlindIdx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expiredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		expiredIDs = append(expiredIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return expiredIDs, nil
}
