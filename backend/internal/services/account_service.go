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

	// DeleteAccount deletes a single account with ownership validation.
	// Only the account owner can delete their account.
	// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
	DeleteAccount(ctx context.Context, accountID, userID string) error

	// DisconnectInstitution disconnects an institution (linked item) and all associated accounts.
	// This also calls Plaid /item/remove to stop monthly billing.
	// Only the institution owner can disconnect it.
	// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
	//
	// Issue #1486 follow-up part F: returns a non-nil error, WITHOUT deleting anything, if /item/remove fails
	// for any reason other than ITEM_NOT_FOUND — the Item would still be live and still billing, and
	// the local row is the only record of it. See the implementation's doc comment.
	DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error

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
		if err := s.accountRepo.ExpireAccountsByFingerprint(ctx, blindIdx); err != nil {
			return fmt.Errorf("failed to expire accounts by fingerprint: %w", err)
		}
		// Issue #1531 Blocker 1: ExpireAccountsByFingerprint just flipped a DIFFERENT user's
		// (ownership.UserID's) accounts from active to expired — the whole point of this method,
		// since a fingerprint blind index is looked up excluding the ACTING user. The HTTP mutation
		// middleware bumps only the actor (this LinkAccounts caller), never ownership.UserID, so if
		// any cached read of that owner's is status-sensitive, this explicit bump is the only thing
		// that closes the gap. Called AFTER the write above has committed (commit-then-bump).
		//
		// ⚠ #1561 NARROWED THIS. The original justification claimed "every cached analytics query
		// filters accounts.status = 'active'", so the owner's net-worth, cash-flow AND spending
		// aggregates all went stale; #1561 removed those nine household-scope predicates (personal
		// scope has had none since #1109). The #1561 CORRECTION that replaced it was then wrong in
		// turn: it said "the only status filter any of them can reach is
		// GetAggregatedHistoryForHousehold's account-type join". There is no such filter. That join
		// is unfiltered — `LEFT JOIN accounts a ON a.user_account_id = bs.user_account_id`
		// (repository/balance_snapshot_repository.go:320) — and status appears only as the first
		// DISTINCT ON tiebreak, `(a.status = 'active') DESC NULLS LAST, a.id` (:313).
		//
		// Re-derived, and stated as a preference rather than a filter: the three cached wrappers are
		// analytics_service_cached.go, dashboard_preferences_service_cached.go and
		// identity_resolver_cached.go; NONE of the queries they reach FILTERS on accounts.status, and
		// exactly one of them — GetAggregatedHistoryForHousehold, via GetNetWorthTrend — PREFERS an
		// active row when choosing which chain member's a.type a household balance is attributed to.
		// That preference is what makes this write cache-relevant. The accounts LIST does filter
		// status, but it is not a cached read (called directly from GetAccountsFiltered at :960).
		//
		// So the bump is still correct, on a much narrower basis: it covers the owner's HOUSEHOLD
		// net-worth history, where expiring their accounts changes which a.type their balances are
		// attributed to. For an owner in no household it now invalidates nothing — kept anyway,
		// because over-invalidation costs one cache miss and this is a cross-user write the
		// middleware structurally cannot see.
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

	// Step 5b: Issue #171 and #896 — If this is a re-link, soft-delete the old linked_item and accounts
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
			for _, oldAcct := range oldAccounts {
				if err := s.accountRepo.UpdateStatus(ctx, oldAcct.ID, user.ID, domain.AccountStatusReplaced, nil); err != nil {
					s.logger.Error("failed to soft-delete old account",
						"account_id", oldAcct.ID,
						"error", err,
					)
				}
			}
		}
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

	// Step 6b: Create accounts in batch
	if err := s.accountRepo.CreateBatch(ctx, user.ID, accounts); err != nil {
		// TODO: Consider rollback mechanism if account creation fails
		// For now, the linked_item → accounts CASCADE DELETE will clean up on item deletion
		return nil, fmt.Errorf("failed to create accounts: %w", err)
	}

	// Step 6c: Issue #658 — Enqueue initial sync job to fetch transactions
	// This is non-blocking - if it fails, account linking still succeeds
	s.enqueueInitialSync(ctx, linkedItem.ID, user.ID)

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

// DeleteAccount deletes a single account with ownership validation.
//
// This method:
// 1. Gets the account to retrieve its linked_item_id
// 2. Delegates to the repository layer which validates ownership via WHERE id = $1 AND user_id = $2
// 3. After deletion, checks if the linked item now has zero active accounts
// 4. If zero accounts remain, stamps cleanup_scheduled_at and enqueues a Plaid-item cleanup job
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
// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
func (s *accountService) DeleteAccount(ctx context.Context, accountID, userID string) error {
	// Step 1: Get the account to retrieve linked_item_id before deletion
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
		return err
	}

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
	// account.UserAccountID is populated by GetByIDAndUser (Task 1 fix).
	// This is best-effort: log on failure, never return an error.
	if s.userAccountSvc != nil && account.UserAccountID != nil {
		remaining, err := s.accountRepo.GetByUserAccountAndStatus(ctx, *account.UserAccountID, string(domain.AccountStatusActive))
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to check remaining accounts for user_account",
					"user_account_id", *account.UserAccountID,
					"error", err,
				)
			}
		} else if len(remaining) == 0 {
			if hideErr := s.userAccountSvc.SetHidden(ctx, *account.UserAccountID, userID, true); hideErr != nil {
				if s.logger != nil {
					s.logger.Warn("failed to hide user_account after last account deletion",
						"user_account_id", *account.UserAccountID,
						"error", hideErr,
					)
				}
			} else if s.logger != nil {
				s.logger.Info("hid user_account after last account deletion",
					"user_account_id", *account.UserAccountID,
				)
			}
		}
	}

	return nil
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
	if err := s.accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to mark accounts removed after disconnecting linked item",
				"item_id", linkedItemID,
				"user_id", userID,
				"error", err,
			)
		}
		return fmt.Errorf("failed to mark accounts removed for item %s: %w", linkedItemID, err)
	}

	return nil
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

	// Map service options to repository filter options
	repoOpts := repository.AccountFilterOptions{
		IncludeHidden:   opts.IncludeHidden,
		IncludeReplaced: opts.IncludeReplaced || opts.StatusFilter == "all",
		ReplacedOnly:    opts.StatusFilter == "replaced",
		UserAccountID:   opts.UserAccountID,
		AsOf:            opts.AsOf,
	}

	// Query accounts with filters using internal UUID
	accounts, err := s.accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID, repoOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	return accounts, nil
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
func (s *accountService) enqueueInitialSync(ctx context.Context, linkedItemID, userID string) {
	if s.jobQueue == nil {
		return
	}

	opts := []queue.EnqueueOption{
		queue.WithDelay(5 * time.Second),
		queue.WithUnique(*plaidSyncUniqueOpts()),
		queue.WithTags("initial_sync"),
	}

	s.enqueueJob(ctx, plaidSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)
	s.enqueueJob(ctx, investmentHoldingsSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)
	s.enqueueJob(ctx, investmentTransactionsSyncJobArgs{UserID: userID, ItemID: linkedItemID}, linkedItemID, userID, opts)
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
