package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

// plaidSyncJobArgs is a local struct for enqueueing Plaid sync jobs.
// This mirrors jobs.PlaidSyncArgs to avoid import cycle (services → jobs → services).
// The JSON structure must match jobs.PlaidSyncArgs exactly for River to deserialize correctly.
type plaidSyncJobArgs struct {
	UserID    string `json:"user_id"`
	ItemID    string `json:"item_id"`
	WebhookID string `json:"webhook_id,omitempty"`
}

// Kind returns the job type identifier. Must match jobs.PlaidSyncArgs.Kind()
func (plaidSyncJobArgs) Kind() string { return "plaid_sync" }

// plaidSyncUniqueOpts returns uniqueness options for Plaid sync jobs.
// Mirrors jobs.PlaidSyncUniqueOpts() to prevent duplicate syncs within 5-minute window.
func plaidSyncUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 5 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// investmentHoldingsSyncJobArgs is a local struct for enqueueing investment holdings sync jobs.
// This mirrors jobs.InvestmentHoldingsSyncArgs to avoid import cycle (Issue #659).
type investmentHoldingsSyncJobArgs struct {
	UserID string `json:"user_id"`
	ItemID string `json:"item_id"`
}

func (investmentHoldingsSyncJobArgs) Kind() string { return "investment_holdings_sync" }

// investmentTransactionsSyncJobArgs is a local struct for enqueueing investment transactions sync jobs.
// This mirrors jobs.InvestmentTransactionsSyncArgs to avoid import cycle (Issue #659).
type investmentTransactionsSyncJobArgs struct {
	UserID string `json:"user_id"`
	ItemID string `json:"item_id"`
}

func (investmentTransactionsSyncJobArgs) Kind() string { return "investment_transactions_sync" }

// itemCleanupJobArgs is a local struct for enqueueing item cleanup jobs.
// This mirrors jobs.ItemCleanupArgs to avoid import cycle (services → jobs → services).
// The JSON structure must match jobs.ItemCleanupArgs exactly for River to deserialize correctly.
// Issue #668: Plaid Item Management for Cost Optimization
type itemCleanupJobArgs struct {
	LinkedItemID string `json:"linked_item_id"`
	UserID       string `json:"user_id"`
	Reason       string `json:"reason"`
}

// Kind returns the job type identifier. Must match jobs.ItemCleanupArgs.Kind()
func (itemCleanupJobArgs) Kind() string { return "item_cleanup" }

// itemCleanupUniqueOpts returns uniqueness options for item cleanup jobs, mirroring
// jobs.ItemCleanupUniqueOpts().
//
// It does NOT prevent duplicate cleanups of the same item: `ByArgs: true` keys on Kind PLUS the full
// Args, and Args carries Reason — so a sweep-enqueued Reason:"stale" job and the Reason:"zero_accounts"
// job enqueued below are distinct unique keys for the same linked item and can coexist. What makes
// that benign is the Plaid side, not this option; see ClaimItemForCleanup in
// repository/linked_item_repository.go.
// Issue #668: Plaid Item Management for Cost Optimization
func itemCleanupUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

var (
	// ErrInvalidPublicToken is returned when Plaid rejects the public token
	ErrInvalidPublicToken = errors.New("invalid or expired public token")
	// ErrPlaidAPIFailure is returned when Plaid API calls fail
	ErrPlaidAPIFailure = errors.New("plaid API request failed")
	// ErrNoAccountsAvailable is returned when Plaid returns zero accounts during linking
	// Issue #668: Zero-account item prevention - prevents creating billable items with no accounts
	ErrNoAccountsAvailable = errors.New("no accounts available from institution")
	// ErrStableAccountIdentityUnavailable is returned when a stable user_accounts identity cannot
	// be established for an account being linked.
	//
	// Issue #1569: this path used to log and CONTINUE, persisting an account with a NULL
	// user_account_id. That is no longer survivable. Under history-retained-by-default an
	// identity-less account is never cascade-deleted, so it lives forever as a chain member that
	// accountChainKey cannot canonicalise — the permanent double-count seed described at
	// dashboard_service.go:35-73 — and it blocks the accounts.user_account_id NOT NULL constraint
	// that the retention model depends on. Refusing the link is the lesser failure: the user sees
	// an actionable error and can retry, instead of silently acquiring an account whose history
	// can never be tracked across a re-link.
	ErrStableAccountIdentityUnavailable = errors.New("could not establish a stable account identity")
)

// AccountService handles account linking and management
type AccountService interface {
	// LinkAccounts exchanges a Plaid public token for an access token and links bank accounts
	// NOTE: After PR #197, this accepts internal user UUID, not Clerk ID
	LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*LinkAccountsResult, error)

	// GetAccounts retrieves all linked bank accounts for a user with institution information
	// NOTE: After PR #197, this accepts internal user UUID, not Clerk ID
	GetAccounts(ctx context.Context, userID string) ([]domain.Account, error)

	// GetAccountsFiltered retrieves accounts with optional filters (Issue #307, #1109)
	// opts.IncludeHidden=false: Returns only visible accounts (default)
	// opts.IncludeReplaced=true: Also includes replaced accounts (chain transparency)
	// opts.UserAccountID: Filters to accounts sharing a user_account_id chain
	GetAccountsFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error)

	// UpdateAccount updates account customization fields (Issue #307)
	// Supports: nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden
	// nil = no change, empty string = clear field
	// Returns the updated account
	UpdateAccount(ctx context.Context, accountID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error)

	// ReorderAccounts bulk updates display_order for multiple accounts (Issue #307)
	// Used for drag-and-drop reordering
	ReorderAccounts(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error

	// ListActiveByUser returns only active accounts for a user, excluding replaced and expired.
	// Issue #1109: re-link chain transparency — callers should prefer this over GetByUserID.
	ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error)

	// DisconnectAccount stops a single account from syncing and soft-removes it (status='removed'),
	// retaining every row derived from it. Only the account owner may disconnect their account.
	// Issue #1569, Task 6: renamed from DeleteAccount, whose name promised the destructive verb
	// this method has never performed since Task 2 — the DELETE HTTP route this backs is now
	// POST /accounts/{id}/disconnect. The genuinely destructive verb is PurgeAccount below.
	// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
	DisconnectAccount(ctx context.Context, accountID, userID string) error

	// PurgeAccount permanently and transactionally destroys accountID and every row derived from
	// it (balance_snapshots, holdings, investment_transactions, liabilities) — see
	// repository.AccountRepository.PurgeAccount's doc comment for the exact contract. Only the
	// account owner may purge their account. Issue #1569, Task 6.
	//
	// Transactions are NOT destroyed (orphaned via ON DELETE SET NULL) and a LIVE savings goal
	// still referencing this account BLOCKS the purge (repository.ErrPurgeBlockedBySavingsGoal) —
	// callers must delete the goal first, and doing so is now sufficient. A goal the user has
	// already deleted is a tombstone that still carries account_id; the purge destroys it (and its
	// contributions, exactly as POST /savings-goals/{id}/purge would) rather than being blocked by
	// it forever. Issue #1569, task 21 (defect C1).
	//
	// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
	PurgeAccount(ctx context.Context, accountID, userID string) error

	// DisconnectInstitution disconnects an institution (linked item) and all associated accounts.
	// This also calls Plaid /item/remove to stop monthly billing.
	// Only the institution owner can disconnect it.
	// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
	//
	// Issue #1486 follow-up part F: returns a non-nil error, WITHOUT deleting anything, if /item/remove fails
	// for any reason other than ITEM_NOT_FOUND — the Item would still be live and still billing, and
	// the local row is the only record of it. See the implementation's doc comment.
	DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error

	// PurgeInstitution permanently and transactionally destroys the linked item and every account
	// under it — see repository.LinkedItemRepository.PurgeLinkedItem's doc comment for the exact
	// contract. Only the institution owner may purge it. Issue #1569, Task 6.
	//
	// Refuses with repository.ErrLinkedItemNotDisconnected unless the item has already been
	// disconnected (Plaid billing already stopped via DisconnectInstitution) — a purge must never
	// be the first thing that happens to a linked item, or it destroys the only local record of a
	// Plaid Item that might still be billing.
	//
	// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
	PurgeInstitution(ctx context.Context, linkedItemID, userID string) error

	// SetJobQueue sets the job queue for auto-sync after linking (Issue #658).
	// This is needed because of circular dependency: service must be created before
	// River client, but needs client to enqueue jobs.
	SetJobQueue(jq queue.JobQueue)

	// SetCacheInvalidation wires cache generation bumping for the cross-user fingerprint-release
	// path (Issue #1531 Blocker 1). checkFingerprintOwnership's ExpireAccountsByFingerprint call
	// flips a DIFFERENT user's (the dispossessed owner's) accounts.status from active to expired
	// — with no user_id predicate, by design, since it must reach rows owned by someone other
	// than the acting caller. Every cached analytics query filters accounts.status = 'active', so
	// without an explicit bump here the dispossessed owner's net-worth/cash-flow/spending
	// aggregates keep serving pre-release numbers until the namespace TTL, even though the acting
	// user's own HTTP-mutation-middleware bump (for THEIR account link) has already run — that
	// middleware only ever bumps the ACTOR, never a third party a write happens to touch. Both
	// parameters are optional (nil = skip that bump), matching every other SetCacheInvalidation
	// setter in this codebase (transaction_sync_service.go's SyncCacheHouseholdLookup is reused
	// here directly rather than redefined, since the shape is identical and both live in this
	// package).
	SetCacheInvalidation(gens *cache.Generations, hh SyncCacheHouseholdLookup)

	// SetAuditLogger wires the GDPR/security audit trail for account.removed and
	// institution.disconnected entries (Issue #1569, Task 5G). Nil-safe optional dependency —
	// see the auditLogger field's doc comment on accountService for why this takes the plain
	// AuditLogger (Log), not AuditLogRepository's LogInTx.
	SetAuditLogger(al AuditLogger)

	// SetAuditLogRepository wires the account.history_purged / linked_item.history_purged audit
	// entries written by PurgeAccount/PurgeInstitution (Issue #1569, Task 6).
	//
	// ⚠ NOT OPTIONAL SINCE #1569 ROUND 17 TASK 11, and this comment said "nil-safe optional
	// dependency" until round 17 corrected it. PurgeAccount and PurgeInstitution now REFUSE with
	// repository.ErrAuditLogRepositoryNotWired — a 500 — when this has not been called, rather
	// than destroying financial history with no trail. Leaving it unwired does not degrade the
	// audit trail; it disables both purge endpoints. Every other Set* on this interface really is
	// optional; this one is a construction requirement, and
	// TestExplicitDestructionAuditSinkIsWiredInMain fails in CI if main.go stops calling it.
	//
	// ⚠ THE REPOSITORY LAYER IS STILL NIL-TOLERANT, deliberately. accountRepo.PurgeAccount and
	// linkedItemRepo.PurgeLinkedItem take the sink as a parameter and skip the audit write when it
	// is nil, so ~19 integration tests can exercise the destruction without an audit fixture. The
	// refusal above is the ONLY thing standing between a nil sink and an unaudited destroy, which
	// is why it lives in every service-layer caller and not in one place.
	//
	// Distinct from SetAuditLogger: those two writes MUST land inside the same
	// transaction as the destruction they describe (repository.AuditLogRepository.LogInTx), which
	// requires the full repository interface rather than the plain Log-only AuditLogger — see
	// LogInTx's own doc comment for why that is safe here (this transaction never takes FOR UPDATE
	// on a users row).
	SetAuditLogRepository(ar repository.AuditLogRepository)
}

// LinkAccountsResult contains the results of linking bank accounts
type LinkAccountsResult struct {
	LinkedItem *domain.LinkedItem
	Accounts   []domain.Account
}

// accountService implements AccountService
type accountService struct {
	plaidService   PlaidService
	linkedItemRepo repository.LinkedItemRepository
	accountRepo    repository.AccountRepository
	userRepo       repository.ProfileRepository
	userAccountSvc UserAccountService   // Issue #171: For re-link user_account matching
	chainResolver  AccountChainResolver // Issue #1109: Active-only account resolution
	db             *pgxpool.Pool
	jobQueue       queue.JobQueue            // Issue #658: For auto-sync after linking (can be nil)
	enc            *appcrypto.FieldEncryptor // Issue #1010: For fingerprint computation (can be nil)
	logger         *slog.Logger

	// auditLogger records account.removed / institution.disconnected entries (Issue #1569, Task
	// 5G — nothing was audit-logged on either path before this). Nil-safe optional dependency,
	// same setter-injection pattern as jobQueue/cacheGens: SetAuditLogger. Uses the plain
	// AuditLogger.Log, NOT AuditLogRepository.LogInTx — LogInTx's safety rests specifically on
	// running inside DeleteUserCascade's transaction with a NULL actor (see its own doc comment);
	// DeleteAccount/DisconnectInstitution have no such transaction (accountRepo.UpdateStatus and
	// MarkRemovedByLinkedItemID are already-documented standalone autocommit statements), and
	// these are self-service, user-initiated actions — the same class RequestDeletion/
	// CancelDeletion/RegisterUser already log via plain Log rather than LogInTx.
	auditLogger AuditLogger

	// auditLogRepo records account.history_purged / linked_item.history_purged entries INSIDE
	// PurgeAccount/PurgeInstitution's own transaction (Issue #1569, Task 6), via LogInTx — unlike
	// auditLogger above, which is intentionally the non-transactional Log.
	//
	// ⚠ REQUIRED, not optional: while this is nil both PurgeAccount and PurgeInstitution return
	// repository.ErrAuditLogRepositoryNotWired instead of running (#1569 round 17 task 11). Wired
	// by SetAuditLogRepository — see that method's doc for what is still nil-tolerant below it.
	auditLogRepo repository.AuditLogRepository

	// Issue #1531 Blocker 1: cache invalidation for the dispossessed owner on fingerprint release
	// (both optional; nil = skip that bump entirely).
	cacheGens      *cache.Generations
	cacheHousehold SyncCacheHouseholdLookup
}

// AccountServiceOption is a functional option for configuring AccountService
type AccountServiceOption func(*accountService)

// WithJobQueue sets the job queue for auto-sync after linking (Issue #658)
func WithJobQueue(jq queue.JobQueue) AccountServiceOption {
	return func(s *accountService) {
		s.jobQueue = jq
	}
}

// WithAccountServiceLogger sets the logger for AccountService
func WithAccountServiceLogger(logger *slog.Logger) AccountServiceOption {
	return func(s *accountService) {
		s.logger = logger
	}
}

// WithUserAccountService sets the UserAccountService for re-link user_account matching (Issue #171)
func WithUserAccountService(uaSvc UserAccountService) AccountServiceOption {
	return func(s *accountService) {
		s.userAccountSvc = uaSvc
	}
}

// WithEncryptor sets the FieldEncryptor for fingerprint computation (Issue #1010).
// When set, LinkAccounts will compute and check cross-user fingerprints.
func WithEncryptor(enc *appcrypto.FieldEncryptor) AccountServiceOption {
	return func(s *accountService) {
		s.enc = enc
	}
}

// computePersistentBlind computes the blind index for a persistent_account_id fingerprint.
// Uses namespaced prefix "pid:" to prevent cross-namespace collisions.
func computePersistentBlind(persistentAccountID string, enc *appcrypto.FieldEncryptor) string {
	return enc.BlindIndex("pid:" + persistentAccountID)
}

// computeFallbackBlind computes the blind index for an institution|mask|type|subtype fingerprint.
// Uses namespaced prefix "fb:" and null-byte delimiters to prevent hash collisions.
func computeFallbackBlind(institutionID, mask, accountType, subtype string, enc *appcrypto.FieldEncryptor) string {
	return enc.BlindIndex("fb:" + institutionID + "\x00" + mask + "\x00" + accountType + "\x00" + subtype)
}

// shouldReleaseFingerprint returns true when a disconnected/revoked item's 90-day TTL has elapsed.
// Active items are never released.
func shouldReleaseFingerprint(ownership *repository.FingerprintOwnership) bool {
	if ownership.ItemStatus == domain.LinkedItemStatusActive {
		return false
	}
	if ownership.ItemStatus != domain.LinkedItemStatusDisconnected &&
		ownership.ItemStatus != domain.LinkedItemStatusRevoked {
		return false
	}
	if ownership.StatusChangedAt == nil {
		return false
	}
	return time.Since(*ownership.StatusChangedAt) >= 90*24*time.Hour
}

// checkFingerprintOwnership checks whether a blind index is owned by another user.
// Returns ErrAccountOwnedByAnotherUser if blocked, nil if allowed.
// If TTL has expired, it releases the fingerprint by expiring the old accounts first.
func (s *accountService) checkFingerprintOwnership(ctx context.Context, blindIdx, userID string) error {
	ownership, err := s.accountRepo.CheckFingerprintOwnership(ctx, blindIdx, userID)
	if err != nil {
		return fmt.Errorf("fingerprint ownership check failed: %w", err)
	}
	if ownership == nil {
		return nil // no other user owns it
	}
	if shouldReleaseFingerprint(ownership) {
		s.logger.Info("releasing expired fingerprint",
			"blind_idx_prefix", blindIdx[:8],
			"other_item_id", ownership.LinkedItemID,
			"status_changed_at", ownership.StatusChangedAt,
		)
		expiredAccountIDs, err := s.accountRepo.ExpireAccountsByFingerprint(ctx, blindIdx)
		if err != nil {
			return fmt.Errorf("failed to expire accounts by fingerprint: %w", err)
		}

		// Issue #1569 Task 6: audit-log each expired account. Nothing was logged on this path
		// before — the gap plaid_account_reconciliation.go's own audit call explicitly calls out
		// ("the expiry path does not, which is precisely the gap this repeats rather than copies").
		//
		// THE ACTOR PROBLEM: this account belongs to ownership.UserID (the dispossessed owner), but
		// the write was triggered by userID (the caller linking a conflicting fingerprint). Logging
		// this as something ownership.UserID did, or as something userID did TO their own account,
		// would both be false — userID never touched this account, and ownership.UserID took no
		// action at all; their dormant account was flipped by someone else's link. AuditLogger.Log
		// has no actor parameter at all (see its doc comment on writeAuditLogEntry: user_id is
		// always written NULL, "the honest reading" for exactly this kind of action nobody
		// performed on the resource). That absence is deliberate here, not an oversight — do not
		// widen this call to pass userID as if it were the actor.
		//
		// Non-fatal, same "already committed, don't abort" reasoning as every other audit call in
		// this file: the expiry already happened, so a logging failure must not surface as a
		// LinkAccounts error.
		if s.auditLogger != nil {
			for _, expiredID := range expiredAccountIDs {
				if auditErr := s.auditLogger.Log(ctx, "account.expired_by_fingerprint_release", "account", expiredID); auditErr != nil {
					logAuditFailure(ctx, auditErr, "account.expired_by_fingerprint_release", ownership.UserID)
				}
			}
		}
		// Issue #1531 Blocker 1: ExpireAccountsByFingerprint just flipped a DIFFERENT user's
		// (ownership.UserID's) accounts from active to expired — the whole point of this method,
		// since a fingerprint blind index is looked up excluding the ACTING user. The HTTP mutation
		// middleware bumps only the actor (this LinkAccounts caller), never ownership.UserID, so if
		// any cached read of that owner's is status-sensitive, this explicit bump is the only thing
		// that closes the gap. Called AFTER the write above has committed (commit-then-bump).
		//
		// ⚠ THIS JUSTIFICATION HAS NOW BEEN WRONG THREE TIMES, each time because the tree moved and
		// the prose did not. Recorded rather than replaced silently:
		//   1. "every cached analytics query filters accounts.status = 'active'" — #1561 removed the
		//      nine household-scope predicates and personal scope had had none since #1109.
		//   2. "the only status filter any of them can reach is GetAggregatedHistoryForHousehold's
		//      account-type join" — that join was never filtered; status was a DISTINCT ON
		//      preference, not a predicate.
		//   3. "that preference is what makes this write cache-relevant" — #1569 T10.2 deleted the
		//      preference. GetAggregatedHistoryForHousehold now joins the snapshot's OWN account
		//      (a.id = bs.account_id) instead of every member of its chain, so there is nothing left
		//      to prefer between.
		//
		// WHAT IS TRUE NOW, and re-derivable in one grep: NO cached read references accounts.status
		// in any form — not as a filter, not as an ordering term. The four files a cache.GetOrLoad
		// site can reach are pinned as cachedReadRepoFiles in
		// repository/cache_completeness_write_set_test.go, and a qualified .status reference does
		// not appear in any of them outside a comment.
		//
		// So this write — `SET status = 'expired', expired_at = NOW()`, and nothing else — changes
		// no cached answer at all. The bump is KEPT anyway and is not dead weight: over-invalidation
		// costs one cache miss, this is a CROSS-USER write the HTTP middleware structurally cannot
		// see, and the day someone adds a status-sensitive cached read this is the only thing
		// standing between that read and a stale cross-user answer. Removing it would be trading a
		// cache miss for a silent correctness cliff.
		//
		// The accounts LIST does filter status, but it is not a cached read (called directly from
		// GetAccountsFiltered).
		s.bumpCacheForDispossessedOwner(ctx, ownership.UserID)
		return nil
	}
	return fmt.Errorf("%w: account fingerprint already claimed by another user (item_id=%s, status=%s)",
		repository.ErrAccountOwnedByAnotherUser, ownership.LinkedItemID, ownership.ItemStatus)
}

// NewAccountService creates a new AccountService instance
func NewAccountService(
	plaidService PlaidService,
	linkedItemRepo repository.LinkedItemRepository,
	accountRepo repository.AccountRepository,
	userRepo repository.ProfileRepository,
	db *pgxpool.Pool,
	opts ...AccountServiceOption,
) AccountService {
	svc := &accountService{
		plaidService:   plaidService,
		linkedItemRepo: linkedItemRepo,
		accountRepo:    accountRepo,
		userRepo:       userRepo,
		chainResolver:  NewAccountChainResolver(accountRepo), // Issue #1109
		db:             db,
		logger:         slog.Default(),
	}

	// Apply optional configuration
	for _, opt := range opts {
		opt(svc)
	}

	return svc
}

// LinkAccounts exchanges a Plaid public token for an access token and links bank accounts
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
//
// Flow:
// 1. Fetch user by internal UUID
// 2. Exchange public token for access token via Plaid API
// 3. Fetch accounts from Plaid
// 4. Issue #171: Check for re-link (existing item at same institution) and soft-delete old data
// 5. Store linked item and accounts atomically in database
// 6. Issue #171: For each account, match/create user_account for stable reference
//
// Returns ErrInvalidPublicToken if Plaid rejects the token
// Returns ErrPlaidAPIFailure if other Plaid API errors occur
func (s *accountService) LinkAccounts(
	ctx context.Context,
	userID, publicToken, institutionID, institutionName string,
) (*LinkAccountsResult, error) {
	// Step 1: Fetch user by internal UUID
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	// Step 2: Exchange public token for access token via Plaid API
	client := s.plaidService.GetClient()

	exchangeReq := plaid.NewItemPublicTokenExchangeRequest(publicToken)
	exchangeResp, _, err := client.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*exchangeReq).Execute()
	if err != nil {
		// Check if it's an invalid token error
		if plaidErr, ok := err.(plaid.GenericOpenAPIError); ok {
			return nil, fmt.Errorf("%w: %s", ErrInvalidPublicToken, plaidErr.Error())
		}
		return nil, fmt.Errorf("%w: %s", ErrPlaidAPIFailure, err.Error())
	}

	accessToken := exchangeResp.GetAccessToken()
	itemID := exchangeResp.GetItemId()

	// Step 3: Fetch accounts from Plaid
	accountsReq := plaid.NewAccountsGetRequest(accessToken)
	accountsResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accountsReq).Execute()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to fetch accounts: %s", ErrPlaidAPIFailure, err.Error())
	}

	plaidAccounts := accountsResp.GetAccounts()

	// Step 3b: Validate that Plaid returned at least one account
	// Issue #668: Prevent creating billable Plaid items with zero accounts
	if len(plaidAccounts) == 0 {
		return nil, fmt.Errorf("%w: the institution returned no accounts to link - please ensure you select accounts during the connection process", ErrNoAccountsAvailable)
	}

	// Step 3c: Issue #1010 — Cross-user fingerprint ownership check.
	// Uses server-provided institution_id (trusted) for fallback fingerprint.
	plaidItem := accountsResp.GetItem()
	if serverInstID := plaidItem.GetInstitutionId(); serverInstID != "" && serverInstID != institutionID {
		s.logger.Warn("institution_id mismatch: client vs server",
			"client_institution_id", institutionID,
			"server_institution_id", serverInstID,
		)
	}
	if s.enc != nil {
		institutionIDFromPlaid := plaidItem.GetInstitutionId()
		for _, plaidAccount := range plaidAccounts {
			// Check persistent fingerprint (when available)
			if persistentID, ok := plaidAccount.GetPersistentAccountIdOk(); ok && persistentID != nil && *persistentID != "" {
				persistentBlind := computePersistentBlind(*persistentID, s.enc)
				if err := s.checkFingerprintOwnership(ctx, persistentBlind, user.ID); err != nil {
					return nil, err
				}
			}

			// Check fallback fingerprint (always)
			mask := "__NO_MASK__"
			if m, ok := plaidAccount.GetMaskOk(); ok && m != nil {
				mask = *m
			}
			accountType := string(plaidAccount.GetType())
			subtype := "__NO_SUBTYPE__"
			if st, ok := plaidAccount.GetSubtypeOk(); ok && st != nil {
				subtype = string(*st)
			}
			fallbackBlind := computeFallbackBlind(institutionIDFromPlaid, mask, accountType, subtype, s.enc)
			if err := s.checkFingerprintOwnership(ctx, fallbackBlind, user.ID); err != nil {
				return nil, err
			}
		}
	}

	// Step 4: Issue #171 — Check for re-link (existing active item at same institution)
	var existingLinkedItem *domain.LinkedItem
	existingLinkedItem, err = s.linkedItemRepo.GetByUserAndInstitution(
		ctx, user.ID, institutionID, string(domain.LinkedItemStatusActive),
	)
	if err != nil && !errors.Is(err, repository.ErrLinkedItemNotFound) {
		// Unexpected error checking for existing item
		return nil, fmt.Errorf("failed to check for existing linked item: %w", err)
	}

	// Step 4b: Issue #896 — Fallback: check for disconnected item (re-subscription reconnection)
	if existingLinkedItem == nil {
		existingLinkedItem, err = s.linkedItemRepo.GetByUserAndInstitution(
			ctx, user.ID, institutionID, string(domain.LinkedItemStatusDisconnected),
		)
		if err != nil && !errors.Is(err, repository.ErrLinkedItemNotFound) {
			return nil, fmt.Errorf("failed to check for disconnected linked item: %w", err)
		}
		if existingLinkedItem != nil {
			s.logger.Info("re-subscription reconnection detected",
				"disconnected_item_id", existingLinkedItem.ID,
				"institution_id", institutionID,
			)
		}
	}

	// Step 5: Create new linked item (stores access token and links accounts)
	linkedItem := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          itemID,
		AccessToken:     accessToken,
		InstitutionID:   institutionID,
		InstitutionName: institutionName,
		Provider:        "plaid",                       // Set provider to avoid database constraint violation (Issue #305)
		Status:          domain.LinkedItemStatusActive, // Issue #171: Explicitly set active status
	}

	// Create linked item
	if err := s.linkedItemRepo.Create(ctx, linkedItem); err != nil {
		return nil, fmt.Errorf("failed to create linked item: %w", err)
	}

	// Step 6: Convert Plaid accounts to domain accounts with user_account_id
	institutionIDFromPlaid := plaidItem.GetInstitutionId()
	accounts := make([]domain.Account, len(plaidAccounts))
	for i, plaidAccount := range plaidAccounts {
		account := convertPlaidAccount(plaidAccount, linkedItem.ID, user.ID)

		// Issue #171: Match or create user_account for stable reference across re-links.
		// Issue #1569: a missing stable identity now FAILS the link — see
		// ErrStableAccountIdentityUnavailable for why continuing is not survivable under
		// history-retained-by-default.
		if s.userAccountSvc == nil {
			s.logger.Error("user_account service is not wired; refusing to link identity-less accounts",
				"plaid_account_id", plaidAccount.GetAccountId(),
				"user_id", user.ID,
			)
			observability.AccountLinkRefusedNoStableIdentity.WithLabelValues("service_unwired").Inc()
			return nil, fmt.Errorf("%w: user_account service unavailable", ErrStableAccountIdentityUnavailable)
		}

		accountType := string(plaidAccount.GetType())
		accountSubtype := ""
		if subtype, ok := plaidAccount.GetSubtypeOk(); ok && subtype != nil {
			accountSubtype = string(*subtype)
		}

		userAccount, err := s.userAccountSvc.MatchOrCreate(
			ctx,
			user.ID,
			institutionID,
			account.Mask,
			plaidAccount.GetName(),
			accountType,
			accountSubtype,
		)
		if err != nil {
			s.logger.Error("failed to match/create user_account; refusing the link",
				"plaid_account_id", plaidAccount.GetAccountId(),
				"user_id", user.ID,
				"error", err,
			)
			observability.AccountLinkRefusedNoStableIdentity.WithLabelValues("match_or_create_failed").Inc()
			return nil, fmt.Errorf("%w: %v", ErrStableAccountIdentityUnavailable, err)
		}
		account.UserAccountID = &userAccount.ID

		// Issue #1010: Compute fingerprint blind indices for storage
		if s.enc != nil {
			mask := "__NO_MASK__"
			if account.Mask != nil {
				mask = *account.Mask
			}
			subtype := "__NO_SUBTYPE__"
			if account.Subtype != nil {
				subtype = *account.Subtype
			}
			if account.PersistentAccountID != nil {
				blind := computePersistentBlind(*account.PersistentAccountID, s.enc)
				account.PersistentAccountIDBlindIdx = &blind
			}
			fallbackBlind := computeFallbackBlind(institutionIDFromPlaid, mask, account.Type, subtype, s.enc)
			account.FallbackFingerprintBlindIdx = &fallbackBlind
		}

		accounts[i] = account
	}

	// Step 6b-pre: Issue #171 and #896 — retire the predecessor, IMMEDIATELY before the successor
	// rows are written.
	//
	// ⚠ #1569 T10-FIX item 1: THIS USED TO RUN AS "STEP 5B", BEFORE THE CONVERSION LOOP ABOVE, AND
	// THAT ORDERING COULD STRAND EVERY BALANCE AT THE INSTITUTION. The loop refuses the whole link
	// when any account has no stable identity (ErrStableAccountIdentityUnavailable), and on that
	// path the predecessor had already been retired: old item 'replaced', every old account
	// 'replaced', the new item holding NO accounts, and no successor pointer anywhere. Neither
	// repair mechanism can reach that state — supersedeStrandedChainSiblings and the succession
	// closure both run INSIDE CreateBatch, which never executes. Retirement is now the last thing
	// before the write it belongs to, so an early return leaves the user's live connection intact.
	// Pinned by TestLinkAccounts_RelinkRefused_LeavesThePredecessorLive.
	//
	// It stays BEFORE CreateBatch rather than after: the successor insert needs the predecessor's
	// fingerprint released, and the backstop is a repair for a retirement that FAILED, not a
	// substitute for one that never ran (TestLinkAccounts_RelinkSucceeds_StillRetiresThePredecessor
	// pins the order).
	if existingLinkedItem != nil {
		s.logger.Info("re-link detected, soft-deleting old linked item",
			"old_item_id", existingLinkedItem.ID,
			"new_item_id", linkedItem.ID,
			"institution_id", institutionID,
		)

		// Mark old linked_item as 'replaced' with replaced_by_id pointing to new item
		now := time.Now()
		existingLinkedItem.Status = domain.LinkedItemStatusReplaced
		existingLinkedItem.StatusChangedAt = &now // Issue #1010: TTL clock for fingerprint release
		existingLinkedItem.ReplacedAt = &now
		existingLinkedItem.ReplacedByID = &linkedItem.ID
		if err := s.linkedItemRepo.Update(ctx, existingLinkedItem); err != nil {
			s.logger.Error("failed to soft-delete old linked item",
				"old_item_id", existingLinkedItem.ID,
				"error", err,
			)
			// Continue - the new item was created successfully
		}

		// Mark old accounts as 'replaced'
		oldAccounts, err := s.accountRepo.GetByLinkedItemID(ctx, existingLinkedItem.ID)
		if err != nil {
			s.logger.Error("failed to get old accounts for soft-delete",
				"old_item_id", existingLinkedItem.ID,
				"error", err,
			)
		} else {
			// ⚠ FAIL-OPEN, and deliberately left that way — #1569 task 9.1 item B.
			//
			// This loop runs BEFORE the successor rows exist (step 6b), and returning here would
			// throw away a bank connection the user just authorised in order to fix a bookkeeping
			// row. Until this task it was also UNBACKED: a transient failure left two 'active'
			// accounts sharing one user_account_id with nothing recording it, and every
			// current-state read — live holdings, the four investment-analytics SUMs — counted
			// one logical position twice, permanently.
			//
			// It is now backed by the write that would otherwise create the second active row:
			// accountRepository.CreateBatch supersedes any stranded sibling of the chain INSIDE
			// the transaction that inserts the successor (supersedeStrandedChainSiblings). So a
			// failure here degrades to "the tombstone lands a few seconds later, atomically",
			// which is why this stays a log and not a return.
			//
			// Still logged at Error: CreateBatch can itself fail, in which case the predecessor is
			// left 'replaced' with no successor to supersede it. That is the one window the
			// reordering above does NOT close — it is two statements wide instead of the whole
			// conversion loop — and a database write failing is worth surfacing on its own.
			for _, oldAcct := range oldAccounts {
				if err := s.accountRepo.UpdateStatus(ctx, oldAcct.ID, user.ID, domain.AccountStatusReplaced, nil); err != nil {
					s.logger.Error("failed to mark the re-linked predecessor account 'replaced'; CreateBatch will supersede it atomically when the successor is inserted",
						"account_id", oldAcct.ID,
						"error", err,
					)
				}
			}
		}
	}

	// Step 6b: Create accounts in batch
	if err := s.accountRepo.CreateBatch(ctx, user.ID, accounts); err != nil {
		// TODO: Consider rollback mechanism if account creation fails
		// For now, the linked_item → accounts CASCADE DELETE will clean up on item deletion
		return nil, fmt.Errorf("failed to create accounts: %w", err)
	}

	// Step 6c: Issue #658 — Enqueue initial sync job to fetch transactions
	// This is non-blocking - if it fails, account linking still succeeds
	// Issue #1613 (P3-T4): only enqueue the investment jobs when Plaid actually granted
	// investments for this item — see itemSupportsProduct's doc comment.
	investmentsSupported := itemSupportsProduct(plaidItem, plaid.PRODUCTS_INVESTMENTS)
	s.enqueueInitialSync(ctx, linkedItem.ID, user.ID, investmentsSupported)

	return &LinkAccountsResult{
		LinkedItem: linkedItem,
		Accounts:   accounts,
	}, nil
}

// convertPlaidAccount converts a Plaid account to a domain.Account
func convertPlaidAccount(plaidAccount plaid.AccountBase, linkedItemID, userID string) domain.Account {
	account := domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: plaidAccount.GetAccountId(),
		Name:           plaidAccount.GetName(),
		Type:           string(plaidAccount.GetType()),
		CurrencyCode:   "USD", // Default to USD
	}

	// Optional fields
	if officialName, ok := plaidAccount.GetOfficialNameOk(); ok && officialName != nil {
		account.OfficialName = officialName
	}

	if subtype, ok := plaidAccount.GetSubtypeOk(); ok && subtype != nil {
		subtypeStr := string(*subtype)
		account.Subtype = &subtypeStr
	}

	if mask, ok := plaidAccount.GetMaskOk(); ok && mask != nil {
		account.Mask = mask
	}

	// Issue #1010: Extract persistent_account_id for cross-user uniqueness fingerprinting
	if persistentID, ok := plaidAccount.GetPersistentAccountIdOk(); ok && persistentID != nil && *persistentID != "" {
		account.PersistentAccountID = persistentID
	}

	// Balance information
	if balances, ok := plaidAccount.GetBalancesOk(); ok && balances != nil {
		if current, ok := balances.GetCurrentOk(); ok && current != nil {
			currentFloat := float64(*current)
			account.CurrentBalance = &currentFloat
		}

		if available, ok := balances.GetAvailableOk(); ok && available != nil {
			availableFloat := float64(*available)
			account.AvailableBalance = &availableFloat
		}

		if isoCurrencyCode, ok := balances.GetIsoCurrencyCodeOk(); ok && isoCurrencyCode != nil {
			account.CurrencyCode = *isoCurrencyCode
		}
	}

	return account
}

// GetAccounts retrieves all linked bank accounts for a user with institution information
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
func (s *accountService) GetAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	// Fetch user by internal UUID to verify user exists
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			// User authenticated via Clerk but not yet in our database
			// Return empty list instead of error (user hasn't linked any accounts yet)
			return []domain.Account{}, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Query accounts using internal UUID
	accounts, err := s.accountRepo.GetAccountsWithInstitution(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	return accounts, nil
}

// DisconnectAccount stops a single account from syncing with ownership validation.
//
// This method:
// 1. Gets the account to retrieve its linked_item_id
// 2. Soft-removes it (status='removed') — ownership validated via WHERE id = $1 AND user_id = $2
// 3. After the soft-remove, checks if the linked item now has zero active accounts
// 4. If zero accounts remain, stamps cleanup_scheduled_at and enqueues a Plaid-item cleanup job
//
// Issue #1569, Task 6: renamed from DeleteAccount — this method has soft-removed rather than
// hard-deleted since Task 2, so the name no longer matched what it did. The DELETE HTTP route it
// used to back is now POST /accounts/{id}/disconnect; the genuinely destructive verb lives in
// PurgeAccount below. Nothing about this method's behavior changed in that rename.
//
// Issue #1486 — step 4's stamp (MarkForCleanup) is NOT optional and must precede the enqueue:
// ItemCleanupWorker re-verifies eligibility before calling Plaid and requires the item to still be
// scheduled, so an unstamped job is declined and silently reports success while Plaid keeps billing.
// See the inline comment at the enqueue site for the full argument.
//
// Use case: User wants to remove a single account (e.g., closed credit card) while
// keeping other accounts from the same institution.
//
// Issue #668: Zero-account item detection and cleanup - proactively remove Plaid items
// when all accounts are deleted to stop unnecessary billing.
//
// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it, and
// repository.ErrAccountAlreadyDisconnected if the account is already 'replaced' or 'expired' — a
// genuinely different transition already happened, so 409 is the right answer. A REPEAT disconnect
// on an account that is already 'removed' is deliberately NOT an error (fix round 1 of Task 2,
// Issue #1569): the caller's desired end state is already reality, so this returns nil — idempotent
// success, matching this endpoint's DefaultStatus of 204 with no distinguishable body either way.
func (s *accountService) DisconnectAccount(ctx context.Context, accountID, userID string) error {
	// Step 1: Get the account to retrieve linked_item_id before soft-removing it
	account, err := s.accountRepo.GetByIDAndUser(ctx, accountID, userID)
	if err != nil {
		return err // Returns ErrAccountNotFound if not found or not owned
	}
	linkedItemID := account.LinkedItemID

	// Step 2: Soft-remove the account (Issue #1569) — set status='removed' + removed_at rather
	// than hard-deleting the row. Before this fix, accountRepo.Delete hard-deleted the account,
	// which CASCADE-deleted its balance_snapshots, holdings, investment transactions, and (via
	// migration 000150's now-RESTRICT FK) would have started failing outright once a savings_goal
	// pointed at it. Retaining the row means that history now survives a single-account removal,
	// matching the institution-level DisconnectInstitution behavior below.
	if err := s.accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil); err != nil {
		// Idempotency case, not an error (fix round 1 of Task 2): the account is ALREADY exactly
		// what this call wants it to be. Return success immediately — no audit log (nothing
		// happened to log), no cleanup pass below (nothing changed for it to react to). Critically,
		// this must NOT fall through to a retried UpdateStatus call: the whole point of the
		// repository guard is that a repeat disconnect never writes, and "success" here must mean
		// that too, not a second attempt that happens to look different.
		if errors.Is(err, repository.ErrAccountAlreadyRemoved) {
			return nil
		}
		return err
	}

	// Issue #1569, Task 5G: audit-log the removal. Non-fatal — the account is already removed by
	// this point, so an audit-write failure must not be reported back to the caller as a failed
	// deletion (same "already committed, don't abort" reasoning as RequestDeletion/
	// CancelDeletion's own audit calls — see AuditLogger's doc comment).
	if s.auditLogger != nil {
		if auditErr := s.auditLogger.Log(ctx, "account.removed", "account", accountID); auditErr != nil {
			logAuditFailure(ctx, auditErr, "account.removed", userID)
		}
	}

	s.cleanupAfterAccountRemoval(ctx, accountID, userID, linkedItemID, account.UserAccountID)

	return nil
}

// PurgeAccount permanently and transactionally destroys accountID and everything derived from it.
// See repository.AccountRepository.PurgeAccount's doc comment for the full contract (what is
// destroyed, what survives, and why the audit write shares that transaction). Issue #1569, Task 6.
//
// After the purge, this runs the SAME zero-account-item cleanup and orphaned-user_account hiding
// as DisconnectAccount — if this was the last account under its linked item, the Plaid item still
// needs its billing stopped, exactly as it would after a soft disconnect. Fetching the account
// BEFORE the purge (rather than after, when the row is gone) is what makes that possible.
//
// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it, or
// repository.ErrPurgeBlockedBySavingsGoal if a savings goal still references it.
func (s *accountService) PurgeAccount(ctx context.Context, accountID, userID string) error {
	// Control 2 is not optional: refuse rather than destroy un-audited. See
	// repository.ErrAuditLogRepositoryNotWired for why a nil sink is reachable at all.
	if s.auditLogRepo == nil {
		return repository.ErrAuditLogRepositoryNotWired
	}

	// Step 1: Get the account to retrieve linked_item_id/user_account_id BEFORE it is destroyed.
	account, err := s.accountRepo.GetByIDAndUser(ctx, accountID, userID)
	if err != nil {
		return err
	}
	linkedItemID := account.LinkedItemID

	if err := s.accountRepo.PurgeAccount(ctx, accountID, userID, s.auditLogRepo); err != nil {
		return err
	}

	s.cleanupAfterAccountRemoval(ctx, accountID, userID, linkedItemID, account.UserAccountID)

	return nil
}

// cleanupAfterAccountRemoval runs the bookkeeping shared by DisconnectAccount and PurgeAccount
// once accountID has stopped being an active account (soft-removed or hard-purged): zero-account
// Plaid-item cleanup, and hiding an orphaned user_account. Both callers fetch the account BEFORE
// removing it so linkedItemID/userAccountID are still available here. Best-effort throughout —
// every failure here is logged, never returned, matching the two callers' pre-extraction behavior.
func (s *accountService) cleanupAfterAccountRemoval(ctx context.Context, accountID, userID, linkedItemID string, userAccountID *string) {
	// Steps 4-5: Zero-account item cleanup (requires job queue)
	if s.jobQueue != nil {
		// Step 4: Count remaining active accounts for this linked item
		remainingCount, err := s.accountRepo.CountActiveByLinkedItemID(ctx, linkedItemID)
		if err != nil {
			// Log error but don't fail the deletion
			if s.logger != nil {
				s.logger.Warn("failed to count remaining accounts after deletion",
					"account_id", accountID,
					"linked_item_id", linkedItemID,
					"error", err,
				)
			}
		} else if remainingCount == 0 {
			// Step 5: If no accounts remain, stamp the cleanup schedule and THEN enqueue.
			//
			// Issue #1486 — why the MarkForCleanup call is here and must stay here. This method is
			// the SECOND production producer of "item_cleanup" jobs; the sweep
			// (queue/jobs/item_cleanup_check.go) is the first, and it has always called
			// MarkForCleanup before enqueueing. ItemCleanupWorker.Work now re-verifies eligibility
			// through ClaimItemForCleanup before calling Plaid /item/remove, and that predicate
			// requires the item to still be SCHEDULED. Enqueueing without stamping produced a job
			// the worker was GUARANTEED to decline — and a declined claim is deliberately a benign
			// no-op, so the worker logged, returned nil, and River recorded SUCCESS while the Plaid
			// item went on billing. No error, no retry, no metric.
			//
			// The daily sweep is NOT a reliable backstop for that, which is why this is a stamp
			// rather than a comment: accountRepository.GetByIDAndUser applies no status filter, so an
			// account belonging to a 'replaced' or 'disconnected' linked item is deletable, and the
			// sweep's candidate predicate excludes both statuses. Such an item would have been
			// stranded permanently, still billing.
			//
			// So both producers now uphold ONE invariant: every item_cleanup job's item carries a
			// cleanup schedule. No grace period — cleanupAt is now, matching the sweep's own
			// isImmediate branch for the zero_accounts leg.
			//
			// ErrLinkedItemNotFound is EXPECTED and benign: MarkForCleanup's WHERE includes
			// `cleanup_scheduled_at IS NULL`, so it reports that sentinel both when the sweep has
			// already stamped this item (the claim will succeed anyway) and when the row is gone (the
			// job becomes a no-op the worker already handles). Any OTHER error means the schedule did
			// not land, so the job would be a guaranteed silent no-op — do not create it; leave the
			// item to the sweep, which can still see it precisely because the stamp failed.
			//
			// NON-ATOMIC, disclosed rather than fixed: accountRepo.UpdateStatus above is a standalone
			// autocommit Exec, so it has already committed by the time this stamp runs as a separate
			// statement. A crash in that window leaves the account marked 'removed', cleanup_scheduled_at
			// NULL and no job — the sweep re-selects most such items, but not a 'replaced'/'disconnected' one
			// (it excludes both statuses), which is then stranded and still billing. Pre-existing in
			// kind — the same window previously sat between Delete and Enqueue — and widened by this
			// one statement, so not a regression; closing it would need a transaction spanning the
			// account delete, which is out of scope here.
			//
			// Structured as a flag rather than an early return so Step 6 below (hiding an orphaned
			// user_account) still runs on the stamp-failure path — it is unrelated bookkeeping and
			// must not be skipped.
			scheduleStamped := true
			if markErr := s.linkedItemRepo.MarkForCleanup(ctx, linkedItemID, time.Now()); markErr != nil &&
				!errors.Is(markErr, repository.ErrLinkedItemNotFound) {
				scheduleStamped = false
				if s.logger != nil {
					s.logger.Warn("failed to mark zero-account item for cleanup, not enqueueing a job the worker would decline",
						"linked_item_id", linkedItemID,
						"user_id", userID,
						"error", markErr,
					)
				}
			}

			if scheduleStamped {
				cleanupJob := itemCleanupJobArgs{
					LinkedItemID: linkedItemID,
					UserID:       userID,
					Reason:       "zero_accounts",
				}
				_, err := s.jobQueue.Enqueue(ctx, cleanupJob, queue.WithUnique(*itemCleanupUniqueOpts()))
				if err != nil {
					if s.logger != nil {
						s.logger.Warn("failed to enqueue item cleanup job, releasing the cleanup stamp",
							"linked_item_id", linkedItemID,
							"user_id", userID,
							"error", err,
						)
					}

					// Release the stamp we just set. cleanup_scheduled_at is a DURABLE marker that
					// hides a row from the sweep (repository.cleanupCandidateSelectQuery requires
					// cleanup_scheduled_at IS NULL), so a stamp left set with no job in flight is
					// not a harmless leftover — it removes a still-billing Plaid item from every
					// future sweep permanently. Before the stamp existed this path left the row
					// untouched and the next daily sweep recovered it; keeping the stamp here would
					// make a transient queue error strictly worse than the old behaviour.
					//
					// Safe from the race the stamp guards against: that race is with a worker
					// executing the job, and the enqueue that would have created it just failed, so
					// no worker can be mid-flight.
					if clearErr := s.linkedItemRepo.ClearCleanupSchedule(ctx, linkedItemID); clearErr != nil && s.logger != nil {
						// Both writes failed. The row keeps cleanup_scheduled_at with no job, so it is
						// invisible to the CANDIDATE sweep (cleanupCandidateSelectQuery requires
						// cleanup_scheduled_at IS NULL) and Plaid keeps billing for it.
						//
						// Issue #1486 follow-up part C corrected this comment, which used to say the
						// item stays hidden "until cleanup_scheduled_at is cleared manually". That is
						// no longer true: stamped-and-overdue with no live job is EXACTLY the state
						// ItemCleanupCheckWorker's stranded-cleanup backstop selects for
						// (GetItemsPendingCleanup), so the next daily sweep re-enqueues it without any
						// operator involvement. This same correction was applied to the structurally
						// identical release in ItemCleanupCheckWorker.Work; this site was missed in
						// that pass and is fixed in fix round 1.
						//
						// Still logged at Error, on narrower grounds: recovery is now automatic but
						// DELAYED by up to one sweep interval, during which Plaid keeps billing, and a
						// double write failure is a signal about the database worth surfacing on its
						// own. Do not downgrade it to Warn on the argument that the backstop handles
						// it — the backstop bounds the damage, it does not make it free.
						s.logger.Error("failed to release cleanup stamp after a failed enqueue; item is hidden from the candidate sweep and still billing until the stranded-cleanup backstop re-enqueues it on a later run",
							"linked_item_id", linkedItemID,
							"user_id", userID,
							"error", clearErr,
						)
					}
				} else if s.logger != nil {
					s.logger.Info("enqueued item cleanup job for zero-account item",
						"linked_item_id", linkedItemID,
						"user_id", userID,
						"reason", "zero_accounts",
					)
				}
			}
		}
	}

	// Step 6: Hide UserAccount if no other active accounts reference it.
	// userAccountID is populated by the caller from GetByIDAndUser (Task 1 fix).
	// This is best-effort: log on failure, never return an error.
	if s.userAccountSvc != nil && userAccountID != nil {
		remaining, err := s.accountRepo.GetByUserAccountAndStatus(ctx, *userAccountID, string(domain.AccountStatusActive), userID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to check remaining accounts for user_account",
					"user_account_id", *userAccountID,
					"error", err,
				)
			}
		} else if len(remaining) == 0 {
			if hideErr := s.userAccountSvc.SetHidden(ctx, *userAccountID, userID, true); hideErr != nil {
				if s.logger != nil {
					s.logger.Warn("failed to hide user_account after last account deletion",
						"user_account_id", *userAccountID,
						"error", hideErr,
					)
				}
			} else if s.logger != nil {
				s.logger.Info("hid user_account after last account deletion",
					"user_account_id", *userAccountID,
				)
			}
		}
	}
}

// DisconnectInstitution disconnects an institution (linked item) while RETAINING the item's row
// and its accounts' financial history (Issue #1569).
//
// This method performs the following steps:
//  1. Retrieve the linked item with ownership validation
//  2. Decrypt the Plaid access token
//  3. Call Plaid /item/remove to stop monthly billing (critical for cost control - Issue #226)
//  4. Mark the item 'disconnected', stamp plaid_removed_at, and mark its accounts 'removed' —
//     see the "Issue #1569" paragraph below for why this replaced a hard DeleteByUser.
//
// Issue #1486 follow-up part F: step 4 is CONDITIONAL on step 3 having succeeded. Until that fix a
// Plaid failure was logged and the row deleted anyway, which is the same defect as parts A/B
// (PlaidItemCleanupService.RemoveItem) on a second, user-initiated path. That condition is
// unchanged by the #1569 rewrite below: step 4 still does not run unless step 3 succeeded (or
// ITEM_NOT_FOUND, see below).
//
// On FREQUENCY, since an earlier version of this comment claimed this path "runs far more often than
// the daily cleanup sweep": that was asserted, not measured, and it is currently FALSE. The frontend
// hook `useDisconnectInstitution` has no consumer in apps/web/src outside test/mocks/accounts-api.ts,
// and no E2E spec exercises disconnect — the endpoint exists and is reachable by API, but nothing in
// the shipped UI calls it. The claim is removed rather than softened; the fix stands on the defect
// being identical and irreversible, which needs no frequency argument. Do not reintroduce a frequency
// claim here without a measurement.
//
// Issue #1569: step 4 used to call linkedItemRepo.DeleteByUser, which hard-deleted the linked_items
// row and, via accounts.linked_item_id ON DELETE CASCADE, every account under it — and through
// those accounts, every balance_snapshot, holding, investment transaction, and liability row. That
// is the literal "I changed banks" action irreversibly destroying the user's financial history, so
// it is replaced with the same claim → remove → stamp → disconnect contract
// PlaidItemCleanupService.RemoveItem already implements for the cleanup worker (see that method,
// and item_cleanup.go's Work): mark the item 'disconnected' (MarkItemsDisconnected, the status this
// repo has carried since migration 000032 — "'disconnected': User manually disconnected, data
// preserved" — designed years ago and bypassed by DeleteByUser ever since), stamp
// plaid_removed_at (MarkPlaidRemoved), and transition the item's accounts to status='removed' with
// removed_at set (accountRepo.MarkRemovedByLinkedItemID). Nothing is deleted.
//
// The plaid_removed_at stamp is NOT bookkeeping — DeleteUserCascade's C2 guard
// (repository.uncleanedLinkedItemExistsClause) checks plaid_removed_at IS NULL, not item status,
// and refuses to erase a user while any item still owes Plaid-side cleanup. A stamp failure here is
// therefore propagated, exactly as it is in RemoveItem, rather than logged and dropped: leaving it
// unstamped would silently block that user's GDPR erasure forever.
//
// The inherited rationale for the OLD hard delete — "the access token is likely already invalid, so
// delete anyway; this prevents orphaned database records" — traded a harmless orphan (a local row)
// for a harmful one (a billing Plaid Item nobody can find) AND for destroyed history, and rested on
// plaidService.RemoveItem's doc comment, which asserted the same thing and was corrected alongside
// the #1486 fix. Plaid bills per ITEM: an Item with an unusable token is still an Item until
// /item/remove succeeds.
//
// The ONE Plaid failure still treated as success is ITEM_NOT_FOUND (isPlaidItemAlreadyGone): Plaid
// has already forgotten the Item, so billing has stopped and there is nothing left to remove.
// Without that branch this method would refuse forever for any such item and the user could never
// clear it from their UI — the same asymmetry parts A and B close in RemoveItem.
//
// On a genuine Plaid failure the row and its accounts are LEFT IN PLACE, still 'active', and the
// error is returned to the caller (a 500 via the handler). Recovery is the ordinary one: the user
// retries the disconnect. Note the cleanup sweep will NOT retry it automatically — a healthy,
// recently-synced item is not a cleanup candidate — which is precisely why the failure must surface
// rather than be swallowed into a false success.
//
// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
func (s *accountService) DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error {
	// Step 1: Get linked item with ownership validation
	linkedItem, err := s.linkedItemRepo.GetByIDAndUser(ctx, linkedItemID, userID)
	if err != nil {
		return err // Returns ErrLinkedItemNotFound if not found or not owned
	}

	// Step 3: Call Plaid to remove the item (stops monthly billing).
	// This is critical for cost control per Issue #226.
	if removeErr := s.plaidService.RemoveItem(ctx, linkedItem.AccessToken); removeErr != nil {
		if !isPlaidItemAlreadyGone(removeErr) {
			if s.logger != nil {
				s.logger.Error("plaid RemoveItem failed; NOT disconnecting the linked item locally, since Plaid billing has not stopped",
					"item_id", linkedItemID,
					"user_id", userID,
					"error", removeErr,
				)
			}
			return fmt.Errorf("failed to remove Plaid item %s; the institution was not disconnected: %w", linkedItemID, removeErr)
		}
		if s.logger != nil {
			s.logger.Info("plaid item already gone (ITEM_NOT_FOUND), treating the disconnect as successful",
				"item_id", linkedItemID,
				"user_id", userID,
			)
		}
	}

	// Step 4: Retain the row and its history — reached only when Plaid billing really has stopped
	// (see the doc comment). Mirrors PlaidItemCleanupService.RemoveItem's order, which is
	// load-bearing there for the same reason: stamp plaid_removed_at BEFORE marking 'disconnected'.
	// A crash between the two leaves status live, so a retried DisconnectInstitution call reaches
	// this line again (RemoveItem answers ITEM_NOT_FOUND on the retry) and the stamp still lands.
	// In the reverse order, a crash after 'disconnected' but before the stamp would still be
	// recoverable here — cleanupCandidateSelectQuery's sweep does not exclude 'disconnected', only
	// 'replaced' — but matching RemoveItem's order keeps this path from becoming a second place
	// that has to reason about that independently.
	//
	// MarkPlaidRemoved failure is NOT swallowed: see the doc comment's GDPR paragraph. A genuine
	// failure here must surface so the caller can retry, not be logged and dropped.
	if stampErr := s.linkedItemRepo.MarkPlaidRemoved(ctx, []string{linkedItemID}); stampErr != nil &&
		!errors.Is(stampErr, repository.ErrNoLinkedItemsUpdated) {
		// ErrNoLinkedItemsUpdated means the row is already gone (MarkPlaidRemoved has no status
		// guard that could otherwise filter it out) — nothing left to stamp, and nothing left for
		// uncleanedLinkedItemExistsClause to see. Any OTHER error must propagate.
		if s.logger != nil {
			s.logger.Error("plaid removal succeeded but stamping plaid_removed_at failed; NOT proceeding to mark the item disconnected",
				"item_id", linkedItemID,
				"user_id", userID,
				"error", stampErr,
			)
		}
		return fmt.Errorf("failed to record plaid_removed_at for item %s after Plaid removal: %w", linkedItemID, stampErr)
	}

	if markErr := s.linkedItemRepo.MarkItemsDisconnected(ctx, []string{linkedItemID}); markErr != nil &&
		!errors.Is(markErr, repository.ErrNoLinkedItemsUpdated) {
		// ErrNoLinkedItemsUpdated is a legitimate no-op — the row is already 'disconnected' or
		// 'replaced' (MarkItemsDisconnected's WHERE excludes both). Either way the Plaid side is
		// already recorded done by the stamp above; any OTHER error must propagate.
		return fmt.Errorf("failed to mark linked item %s disconnected: %w", linkedItemID, markErr)
	}

	// Transition the item's accounts to 'removed' with removed_at set — the change that actually
	// retains history: no CASCADE, no DELETE, balance_snapshots/holdings/investment
	// transactions/liability rows for these accounts are untouched.
	if err := s.accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID, domain.RemovalReasonUserDisconnect); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to mark accounts removed after disconnecting linked item",
				"item_id", linkedItemID,
				"user_id", userID,
				"error", err,
			)
		}
		return fmt.Errorf("failed to mark accounts removed for item %s: %w", linkedItemID, err)
	}

	// Issue #1569, Task 5G: audit-log the disconnect. Non-fatal, same reasoning as
	// DeleteAccount's audit call above — the disconnect has already committed by this point.
	if s.auditLogger != nil {
		if auditErr := s.auditLogger.Log(ctx, "institution.disconnected", "linked_item", linkedItemID); auditErr != nil {
			logAuditFailure(ctx, auditErr, "institution.disconnected", userID)
		}
	}

	return nil
}

// PurgeInstitution permanently and transactionally destroys the linked item and every account
// under it. See repository.LinkedItemRepository.PurgeLinkedItem's doc comment for the full
// contract. Issue #1569, Task 6.
//
// Deliberately a thin pass-through: unlike PurgeAccount, there is no post-purge zero-account-item
// cleanup to run — the item itself, along with every account under it, is what was just destroyed.
//
// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it,
// repository.ErrLinkedItemNotDisconnected unless the item was already disconnected first, or
// repository.ErrPurgeBlockedBySavingsGoal if a savings goal still references one of its accounts.
func (s *accountService) PurgeInstitution(ctx context.Context, linkedItemID, userID string) error {
	// See PurgeAccount's identical refusal.
	if s.auditLogRepo == nil {
		return repository.ErrAuditLogRepositoryNotWired
	}
	return s.linkedItemRepo.PurgeLinkedItem(ctx, linkedItemID, userID, s.auditLogRepo)
}

// AccountFilterOptions holds filter options for GetAccountsFiltered (Issue #1109).
type AccountFilterOptions struct {
	IncludeHidden   bool // if true, include hidden accounts
	IncludeReplaced bool // if true, equivalent to StatusFilter="all" (backward-compat alias)
	// StatusFilter is "active" (default), "replaced" (replaced only), or "all". "all" is a
	// misnomer inherited from the public query parameter: it maps to the repository's
	// IncludeReplaced, i.e. `status IN ('active','replaced')`, so accounts a fully-disconnected
	// chain left at status='expired' are NOT returned. No service-level option exposes the
	// repository's AllStatuses (the genuine no-status-predicate read) today. Issue #1537.
	StatusFilter  string
	UserAccountID string // if set, filter to accounts with this user_account_id

	// AsOf resolves the account universe as it really was at this instant, from
	// account_status_history, instead of the current accounts.status snapshot. Mutually
	// exclusive with a "replaced"-only StatusFilter and with the repository's AllStatuses (see
	// repository.ErrAccountFilterAsOfConflict / resolveAccountStatusScope) — the API layer must
	// not offer a combination that errors. Issue #1569.
	AsOf *time.Time
}

// GetAccountsFiltered retrieves accounts with optional filters (Issue #307, #1109).
// Supports hidden, replaced-status, and user_account_id chain filtering.
func (s *accountService) GetAccountsFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error) {
	// Fetch user by internal UUID to verify user exists
	_, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			// User authenticated via Clerk but not yet in our database
			// Return empty list instead of error (user hasn't linked any accounts yet)
			return []domain.Account{}, nil
		}
		// For other errors, also return empty list for consistency with GetAccounts
		return []domain.Account{}, nil
	}

	// Map service options to repository filter options.
	//
	// StatusFilter=="all_statuses" is distinct from the pre-existing StatusFilter=="all" misnomer:
	// "all" maps to IncludeReplaced (`status IN ('active','replaced')`), while "all_statuses" maps
	// to the repository's genuine no-status-predicate AllStatuses read (active, replaced, expired
	// AND removed). Issue #1569, Task 5D: the accounts page's "Closed accounts" surface needs the
	// full universe so it can locally split active vs. closed via isInactive.
	repoOpts := repository.AccountFilterOptions{
		IncludeHidden:   opts.IncludeHidden,
		IncludeReplaced: opts.IncludeReplaced || opts.StatusFilter == "all",
		ReplacedOnly:    opts.StatusFilter == "replaced",
		AllStatuses:     opts.StatusFilter == "all_statuses",
		UserAccountID:   opts.UserAccountID,
		AsOf:            opts.AsOf,

		// Issue #1591: the accounts list is also the accounts DETAIL surface — AccountDetailPage
		// has no single-account endpoint and reads this same response — so this is the one read
		// whose rows reach a place that can print "Last known balance: $X as of <date>" beside a
		// Closed badge. Terminal accounts only; active ones come back with both fields nil.
		//
		// Gated on AsOf being absent because the repository REFUSES the combination
		// (ErrAccountFilterLastKnownAsOfConflict): an as-of read is anchored to a past instant and
		// "the newest snapshot on record" is not an answer to it. Asking for it here would turn
		// every as-of listing into an error rather than a blend — correct, but the caller should
		// not be asking. See lastKnownBalanceLateral in repository/account_liveness_sql.go.
		IncludeLastKnownBalance: opts.AsOf == nil,
	}

	// Query accounts with filters using internal UUID
	accounts, err := s.accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID, repoOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	return accounts, nil
}

// upperCaseHexColor normalizes a hex colour the way the legacy accounts-side write does, so the
// two UpdateAccount legs cannot disagree about the stored casing. nil (no change) and "" (clear)
// pass through untouched — both are meaningful to the update request and neither is a colour.
func upperCaseHexColor(color *string) *string {
	if color == nil || *color == "" {
		return color
	}
	upper := strings.ToUpper(*color)
	return &upper
}

// UpdateAccount updates account customization fields (Issue #307)
//
// Flow:
// 1. Validate the request (hex color, icon, nickname length, etc.)
// 2. Fetch existing account with ownership validation
// 3. Apply updates to account (nil = no change, empty string = clear)
// 4. Persist updated account to database
// 5. Return updated account
func (s *accountService) UpdateAccount(
	ctx context.Context,
	accountID, userID string,
	req *domain.UpdateAccountRequest,
) (*domain.Account, error) {
	// Step 1: Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Step 2: Fetch existing account with ownership validation
	account, err := s.accountRepo.GetByIDAndUser(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}

	// Step 2b: THE WRITE FOLLOWS THE CHAIN (#1609, task 3c).
	//
	// Task 3b made every account read chain-authoritative — for a chained account the six
	// customization fields come from user_accounts, and accounts.<field> stops being read at all.
	// Writing `accounts` here would therefore save into a column nothing reads and revert on the
	// next refresh: the exact defect this batch exists to fix, relocated to this endpoint.
	//
	// So a chained account is delegated to the user-account service — the SAME method
	// PATCH /api/v1/user-accounts/{id} calls — and the response is RE-READ through the
	// chain-authoritative query rather than reconstructed from the request, so the body this
	// endpoint returns is what the next GET will return.
	//
	// ⚠ Not redundant with the frontend, which already routes chained accounts to the
	// user-accounts endpoint (useAccountCustomization.ts). The API must not depend on client
	// discipline: MCP tools, integrations and anyone holding the OpenAPI contract reach this
	// endpoint directly.
	//
	// userAccountSvc is an OPTIONAL dependency (nil in several construction paths), so a nil one
	// falls back to the legacy write rather than panicking. That fallback is a strictly better
	// outcome than a crash and is exactly what the code did before this change; it is not an
	// endorsement of the accounts-side write.
	if account.UserAccountID != nil && s.userAccountSvc != nil {
		if _, err := s.userAccountSvc.UpdateCustomization(ctx, *account.UserAccountID, userID,
			&domain.UpdateUserAccountRequest{
				Nickname:     req.Nickname,
				CustomColor:  upperCaseHexColor(req.CustomColor),
				CustomIcon:   req.CustomIcon,
				CustomEmoji:  req.CustomEmoji,
				DisplayOrder: req.DisplayOrder,
				IsHidden:     req.IsHidden,
			}); err != nil {
			return nil, fmt.Errorf("failed to update account customization on its chain: %w", err)
		}
		return s.accountRepo.GetByIDAndUser(ctx, accountID, userID)
	}

	// Step 3: Apply updates (nil = no change, empty string = clear)
	if req.Nickname != nil {
		trimmed := strings.TrimSpace(*req.Nickname)
		if trimmed == "" {
			account.Nickname = nil // Clear the field
		} else {
			account.Nickname = &trimmed
		}
	}

	if req.CustomColor != nil {
		if *req.CustomColor == "" {
			account.CustomColor = nil // Clear the field
		} else {
			// Uppercase hex color for consistency
			upper := strings.ToUpper(*req.CustomColor)
			account.CustomColor = &upper
		}
	}

	if req.CustomIcon != nil {
		if *req.CustomIcon == "" {
			account.CustomIcon = nil // Clear the field
		} else {
			account.CustomIcon = req.CustomIcon
		}
	}

	if req.CustomEmoji != nil {
		if *req.CustomEmoji == "" {
			account.CustomEmoji = nil // Clear the field
		} else {
			account.CustomEmoji = req.CustomEmoji
		}
	}

	if req.DisplayOrder != nil {
		account.DisplayOrder = req.DisplayOrder
	}

	if req.IsHidden != nil {
		account.IsHidden = *req.IsHidden
	}

	// Step 4: Persist updated account
	if err := s.accountRepo.Update(ctx, account, userID); err != nil {
		return nil, fmt.Errorf("failed to update account: %w", err)
	}

	// Step 5: Return updated account
	return account, nil
}

// ReorderAccounts bulk updates display_order for multiple accounts (Issue #307)
//
// Used for drag-and-drop reordering. Validates that order list is not empty
// and delegates to repository for atomic updates.
func (s *accountService) ReorderAccounts(
	ctx context.Context,
	userID string,
	req *domain.ReorderAccountsRequest,
) error {
	// Validate that order list is not empty
	if len(req.AccountOrders) == 0 {
		return errors.New("account orders cannot be empty")
	}

	// Delegate to repository for atomic update
	if err := s.accountRepo.UpdateDisplayOrders(ctx, userID, req.AccountOrders); err != nil {
		return fmt.Errorf("failed to reorder accounts: %w", err)
	}

	return nil
}

// SetJobQueue sets the job queue for auto-sync after linking (Issue #658).
// This is needed because of circular dependency: service must be created before
// River client, but needs client to enqueue jobs.
// ListActiveByUser returns only active (non-replaced, non-expired) accounts for a user.
// Issue #1109: re-link chain transparency.
func (s *accountService) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	return s.chainResolver.ListActiveByUser(ctx, userID)
}

func (s *accountService) SetJobQueue(jq queue.JobQueue) {
	s.jobQueue = jq
}

// SetCacheInvalidation wires cache generation bumping for the cross-user fingerprint-release path.
// See the interface method's doc comment for why this exists. Issue #1531 Blocker 1.
func (s *accountService) SetCacheInvalidation(gens *cache.Generations, hh SyncCacheHouseholdLookup) {
	s.cacheGens = gens
	s.cacheHousehold = hh
}

// SetAuditLogger wires the GDPR/security audit trail for account.removed and
// institution.disconnected entries. Nil-safe optional dependency (Issue #1569, Task 5G) — see
// the auditLogger field's doc comment for why this uses Log rather than LogInTx.
func (s *accountService) SetAuditLogger(al AuditLogger) {
	s.auditLogger = al
}

// SetAuditLogRepository implements AccountService.SetAuditLogRepository — see that method's doc
// comment for why purge needs the LogInTx-capable repository interface rather than the plain
// AuditLogger used by SetAuditLogger above.
func (s *accountService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

// bumpCacheForDispossessedOwner invalidates userID's cached analytics (and, when resolvable,
// their household's) after checkFingerprintOwnership's ExpireAccountsByFingerprint call has
// committed. Void by design — cache.Generations.Bump itself logs+counts its own Redis failures —
// matching every other generation bump in this codebase.
//
// Issue #1531 Blocker 2 hygiene: this can run inside an HTTP request (LinkAccounts' caller), so it
// detaches from ctx's own cancellation/deadline the same way the HTTP mutation middleware and the
// sync services' bump helpers do — a client disconnecting right after the write above commits
// must not cancel a bump for a write that already durably happened, even though that write
// affects a user other than the one whose request this is.
func (s *accountService) bumpCacheForDispossessedOwner(ctx context.Context, userID string) {
	if s.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()

	s.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "fingerprint_release")

	if s.cacheHousehold == nil {
		return
	}
	household, _, found, err := s.cacheHousehold.GetHouseholdForUser(bumpCtx, userID)
	if err != nil {
		// Void by design: the expiry already committed and the user-scope bump above already
		// ran; a household lookup failure must never suppress that.
		if s.logger != nil {
			s.logger.Error("cache invalidation: household lookup failed after fingerprint release; "+
				"household-scoped cache may serve stale data until its namespace TTL",
				"user_id", userID, "error", err)
		}
		return
	}
	if found && household != nil {
		s.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, household.ID, "fingerprint_release")
	}
}

// enqueueInitialSync enqueues sync jobs for transactions and investments after account linking.
// Issue #658: Auto-sync transactions after Plaid account linking
// Issue #659: Auto-sync investments after Plaid account linking
//
// This method is non-blocking: if the job queue fails, we log a warning but don't
// fail the account linking operation. The user will still have their accounts linked,
// and transactions will be synced when webhooks arrive.
//
// Issue #1613 (P3-T4): investmentsSupported gates ONLY the two investment jobs, never the
// plaid_sync job. plaid_sync/TRANSACTIONS is the required product for every Plaid Link in this
// app (plaid_service.go's CreateLinkToken) and Plaid enforces PRODUCTS_NOT_SUPPORTED per
// product requested, not per item (transaction_sync.go's PlaidErrorPermanent case) — an item
// that genuinely can't do investments (e.g. a plain checking/savings bank like Rocket Mortgage)
// still needs its transactions synced. Gating the whole helper, as a prior draft of this fix
// did, would have silently stopped that customer's transactions from ever syncing — the same
// false-negative shape this phase already withdrew once for account-type gating. See
// itemSupportsProduct's doc comment for how the caller decides investmentsSupported.
func (s *accountService) enqueueInitialSync(ctx context.Context, linkedItemID, userID string, investmentsSupported bool) {
	if s.jobQueue == nil {
		return
	}

	opts := []queue.EnqueueOption{
		queue.WithDelay(5 * time.Second),
		queue.WithUnique(*plaidSyncUniqueOpts()),
		queue.WithTags("initial_sync"),
	}

	s.enqueueJob(ctx, plaidSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)

	// #1613 P4-T3 item E: make the grant decision DURABLE. Until this, the only place in the
	// system that knew Plaid had granted no investments for an Item was this function, for the
	// duration of one call — it logged the fact and dropped it.
	//
	// ⚠ Written in BOTH directions, and ⚠ CORRECTED: the first version of this comment said that
	// was because "LinkAccounts reuses an existing linked_items row on re-link". It does NOT — it
	// marks the old item `replaced` and Creates a new row, whose id is what reaches this call — so
	// the FALSE write is a no-op against today's only caller. It stays a setter rather than a
	// latch because this records a DECISION and a column default is not one, and because this
	// helper takes an id rather than a freshly built row: nothing in its signature stops a future
	// caller passing an existing item, where a latch would strand a stale TRUE.
	//
	// ⚠ Non-blocking, like every other write in this helper: a failed bookkeeping write must
	// never cost the user their transaction sync.
	s.recordInvestmentsSkippedAtLink(ctx, linkedItemID, userID, !investmentsSupported)

	if !investmentsSupported {
		s.logger.Info("skipping initial investment sync: Plaid did not grant investments for this item",
			"linked_item_id", linkedItemID,
			"user_id", userID,
		)
		return
	}

	s.enqueueJob(ctx, investmentHoldingsSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)
	s.enqueueJob(ctx, investmentTransactionsSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)
}

// recordInvestmentsSkippedAtLink persists linked_items.investments_skipped_at_link. It never
// returns an error: enqueueInitialSync is documented non-blocking, and this is bookkeeping.
//
// ── RULING (#1613 P4-T3 item E): the per-sync product RE-CHECK was considered and DECLINED. ──
//
// The proposal was to re-read Plaid's product grants on later syncs, gated on this flag, so an
// investments grant that materialises AFTER link (an OAuth or institution-side delay) still
// enqueues the investment jobs. It is not done, for three reasons, and the reasoning is written
// here rather than in a ticket because this is where the next person will look:
//
//  1. It is NOT free, and the obvious cheap version does not exist. plaid-go's
//     TransactionsSyncResponse carries Accounts, NextCursor and the change arrays — there is no
//     Item field on it — so a re-check cannot read product grants off a sync we already make. It
//     costs one extra AccountsGet call.
//  2. The cost falls almost entirely on the population it cannot help. The dominant flagged item
//     is a plain bank that genuinely cannot serve investments; its flag NEVER clears, so an
//     "every sync while flagged" re-check bills that item an extra Plaid call on every sync,
//     forever. The population that benefits — a grant that appears later — is bounded by Plaid's
//     own documented behaviour that an optional product the institution cannot support is simply
//     never added to the Item.
//  3. The failure being fixed is a FALSE NEGATIVE with an independent recovery path already in
//     the tree: webhook_processing.go handles HOLDINGS and INVESTMENTS_TRANSACTIONS webhooks, so
//     a grant that does materialise is picked up without a poll. Adding an outbound Plaid call to
//     the transaction-sync path would add a new outage dependency to a path that must not break,
//     in exchange for closing a gap that is already covered in the common case.
//
// ⚠ What is NOT claimed: that the webhook path is GUARANTEED. It is not, and that is precisely
// why the flag is written even though nothing read it at the time — the measurement now exists (`GetInvestmentsSkippedAtLinkCount`, the admin health gauge, live-scoped). A future re-check needs exactly this
// column, and — more usefully — the flag makes the affected population COUNTABLE before anyone
// pays for a poll, which is the measurement this decision was made without. If it turns out to be
// large, the right shape is a ONE-SHOT re-check on the first webhook-driven sync (bounded at one
// extra AccountsGet per item, ever), not an every-sync poll. It must ride a LATER sync in any
// case: the first transactions sync runs 5 seconds after link (queue.WithDelay above), and a
// product grant re-read 5 seconds after the link-time read will essentially never see a change.
func (s *accountService) recordInvestmentsSkippedAtLink(ctx context.Context, linkedItemID, userID string, skipped bool) {
	// Mirrors the `s.jobQueue == nil` guard at the top of enqueueInitialSync. Unreachable in
	// production — NewAccountService takes linkedItemRepo as a required positional parameter —
	// and present only so a partially-constructed service in a unit test does not panic.
	if s.linkedItemRepo == nil {
		return
	}
	if err := s.linkedItemRepo.SetInvestmentsSkippedAtLink(ctx, linkedItemID, skipped); err != nil && s.logger != nil {
		s.logger.Warn("could not record investments_skipped_at_link; the initial sync jobs are unaffected",
			"linked_item_id", linkedItemID,
			"user_id", userID,
			"skipped", skipped,
			"error", err,
		)
	}
}

// itemSupportsProduct reports whether Plaid actually granted the given product for this Item,
// using only data already returned by the AccountsGet call LinkAccounts makes (Step 3) — no new
// Plaid API call is needed.
//
// Issue #1613 (P3-T4), Codex finding 3: plaid-go v23's Item carries FOUR product arrays
// (AvailableProducts, BilledProducts, Products, ConsentedProducts) and they are not
// interchangeable. AvailableProducts ("available for the Item that have not yet been accessed")
// and BilledProducts ("billed for the Item") are documented as mutually exclusive — together
// they are Plaid's own record of what it added to the Item. Products "in almost all cases" only
// mirrors BilledProducts, but model_item.go's own comment carves out exactly the optional-product
// case this task cares about: a product "may appear in products but not in billed_products" when
// it was added but not yet billed (INVESTMENTS is requested as an optional product in
// CreateLinkToken). Checking all three — never ConsentedProducts, which tracks Data Transparency
// consent collection, not product availability — is the closest read of "will this item serve
// investments" obtainable from data already in hand. Plaid's own docs (plaid.com/docs/link/
// initializing-products) say an optional product that the institution can't actually support is
// simply never added to the Item, so its absence from all three arrays here is the same fact
// that would otherwise surface later as PRODUCTS_NOT_SUPPORTED.
//
// This is deliberately conservative in the direction that protects the feature: HOLDINGS and
// INVESTMENTS_TRANSACTIONS webhooks (webhook_processing.go's processInvestmentWebhook) are the
// only OTHER path that syncs investments, and Plaid only sends those webhooks for items it has
// actually granted investments to — so if this check is ever wrong in the false-negative
// direction (skips an item that later turns out to support investments, e.g. after an update-mode
// re-auth), the webhook path still recovers it. It does not silently and permanently strand the
// feature the way the withdrawn account-type gate did.
func itemSupportsProduct(item plaid.Item, product plaid.Products) bool {
	for _, p := range item.GetAvailableProducts() {
		if p == product {
			return true
		}
	}
	for _, p := range item.GetBilledProducts() {
		if p == product {
			return true
		}
	}
	for _, p := range item.GetProducts() {
		if p == product {
			return true
		}
	}
	return false
}

// enqueueJob enqueues a single job and logs success or failure.
func (s *accountService) enqueueJob(ctx context.Context, job queue.Job, linkedItemID, userID string, opts []queue.EnqueueOption) {
	_, err := s.jobQueue.Enqueue(ctx, job, opts...)
	if err != nil {
		s.logger.Warn("failed to enqueue initial sync job",
			"linked_item_id", linkedItemID,
			"user_id", userID,
			"job_kind", job.Kind(),
			"error", err,
		)
	} else {
		s.logger.Info("enqueued initial sync job",
			"linked_item_id", linkedItemID,
			"user_id", userID,
			"job_kind", job.Kind(),
		)
	}
}
