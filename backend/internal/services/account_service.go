package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

// itemCleanupUniqueOpts returns uniqueness options for item cleanup jobs.
// Mirrors jobs.ItemCleanupUniqueOpts() to prevent duplicate cleanups.
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
	// ErrAccountLimitExceeded is returned when user tries to link more accounts than their tier allows
	ErrAccountLimitExceeded = errors.New("account limit exceeded for current subscription tier")
	// ErrInvalidPublicToken is returned when Plaid rejects the public token
	ErrInvalidPublicToken = errors.New("invalid or expired public token")
	// ErrPlaidAPIFailure is returned when Plaid API calls fail
	ErrPlaidAPIFailure = errors.New("plaid API request failed")
	// ErrNoAccountsAvailable is returned when Plaid returns zero accounts during linking
	// Issue #668: Zero-account item prevention - prevents creating billable items with no accounts
	ErrNoAccountsAvailable = errors.New("no accounts available from institution")
)

// AccountService handles account linking and management
type AccountService interface {
	// LinkAccounts exchanges a Plaid public token for an access token and links bank accounts
	// NOTE: After PR #197, this accepts internal user UUID, not Clerk ID
	LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*LinkAccountsResult, error)

	// GetAccounts retrieves all linked bank accounts for a user with institution information
	// NOTE: After PR #197, this accepts internal user UUID, not Clerk ID
	GetAccounts(ctx context.Context, userID string) ([]domain.Account, error)

	// GetAccountsFiltered retrieves accounts with optional hidden filter (Issue #307)
	// includeHidden=false: Returns only visible accounts (default view)
	// includeHidden=true: Returns all accounts including hidden ones
	GetAccountsFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error)

	// UpdateAccount updates account customization fields (Issue #307)
	// Supports: nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden
	// nil = no change, empty string = clear field
	// Returns the updated account
	UpdateAccount(ctx context.Context, accountID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error)

	// ReorderAccounts bulk updates display_order for multiple accounts (Issue #307)
	// Used for drag-and-drop reordering
	ReorderAccounts(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error

	// DeleteAccount deletes a single account with ownership validation.
	// Only the account owner can delete their account.
	// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
	DeleteAccount(ctx context.Context, accountID, userID string) error

	// DisconnectInstitution disconnects an institution (linked item) and all associated accounts.
	// This also calls Plaid /item/remove to stop monthly billing.
	// Only the institution owner can disconnect it.
	// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
	DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error

	// SetJobQueue sets the job queue for auto-sync after linking (Issue #658).
	// This is needed because of circular dependency: service must be created before
	// River client, but needs client to enqueue jobs.
	SetJobQueue(jq queue.JobQueue)
}

// LinkAccountsResult contains the results of linking bank accounts
type LinkAccountsResult struct {
	LinkedItem *domain.LinkedItem
	Accounts   []domain.Account
}

// accountService implements AccountService
type accountService struct {
	plaidService    PlaidService
	subscriptionSvc *SubscriptionService
	encryptionSvc   EncryptionService
	linkedItemRepo  repository.LinkedItemRepository
	accountRepo     repository.AccountRepository
	userRepo        repository.ProfileRepository
	userAccountSvc  UserAccountService // Issue #171: For re-link user_account matching
	db              *pgxpool.Pool
	jobQueue        queue.JobQueue // Issue #658: For auto-sync after linking (can be nil)
	logger          *slog.Logger
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

// NewAccountService creates a new AccountService instance
func NewAccountService(
	plaidService PlaidService,
	subscriptionSvc *SubscriptionService,
	encryptionSvc EncryptionService,
	linkedItemRepo repository.LinkedItemRepository,
	accountRepo repository.AccountRepository,
	userRepo repository.ProfileRepository,
	db *pgxpool.Pool,
	opts ...AccountServiceOption,
) AccountService {
	svc := &accountService{
		plaidService:    plaidService,
		subscriptionSvc: subscriptionSvc,
		encryptionSvc:   encryptionSvc,
		linkedItemRepo:  linkedItemRepo,
		accountRepo:     accountRepo,
		userRepo:        userRepo,
		db:              db,
		logger:          slog.Default(),
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
// 1. Fetch user by internal UUID to get subscription tier
// 2. Check current account count against subscription limit (fail fast)
// 3. Exchange public token for access token via Plaid API
// 4. Fetch accounts from Plaid
// 5. Verify combined count doesn't exceed limit
// 6. Encrypt access token
// 7. Issue #171: Check for re-link (existing item at same institution) and soft-delete old data
// 8. Store linked item and accounts atomically in database
// 9. Issue #171: For each account, match/create user_account for stable reference
//
// Returns ErrAccountLimitExceeded if user is at or over their account limit
// Returns ErrInvalidPublicToken if Plaid rejects the token
// Returns ErrPlaidAPIFailure if other Plaid API errors occur
func (s *accountService) LinkAccounts(
	ctx context.Context,
	userID, publicToken, institutionID, institutionName string,
) (*LinkAccountsResult, error) {
	// Step 1: Fetch user by internal UUID to get subscription tier
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	// Step 2: Check current account count against subscription limit (fail fast)
	currentAccountCount, err := s.accountRepo.CountByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to count user accounts: %w", err)
	}

	// Check limit before making Plaid API call (fail fast)
	limitCheck, err := s.subscriptionSvc.CheckLimit(
		ctx,
		user,
		domain.ResourceAccounts,
		currentAccountCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to check account limit: %w", err)
	}

	if !limitCheck.Allowed {
		return nil, fmt.Errorf("%w: current=%d, limit=%d, tier=%s",
			ErrAccountLimitExceeded,
			limitCheck.CurrentUsage,
			limitCheck.Limit,
			user.SubscriptionTier)
	}

	// Step 3: Exchange public token for access token via Plaid API
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

	// Step 4: Fetch accounts from Plaid
	accountsReq := plaid.NewAccountsGetRequest(accessToken)
	accountsResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accountsReq).Execute()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to fetch accounts: %s", ErrPlaidAPIFailure, err.Error())
	}

	plaidAccounts := accountsResp.GetAccounts()

	// Step 4b: Validate that Plaid returned at least one account
	// Issue #668: Prevent creating billable Plaid items with zero accounts
	if len(plaidAccounts) == 0 {
		return nil, fmt.Errorf("%w: the institution returned no accounts to link - please ensure you select accounts during the connection process", ErrNoAccountsAvailable)
	}

	// Step 5: Verify combined count doesn't exceed limit
	// Skip this check for unlimited tiers (limit=-1)
	newTotalCount := currentAccountCount + len(plaidAccounts)
	if !limitCheck.IsUnlimited && newTotalCount > limitCheck.Limit {
		return nil, fmt.Errorf("%w: linking %d accounts would exceed limit (current=%d, limit=%d)",
			ErrAccountLimitExceeded,
			len(plaidAccounts),
			currentAccountCount,
			limitCheck.Limit)
	}

	// Step 6: Encrypt access token
	encryptedToken, err := s.encryptionSvc.Encrypt(accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt access token: %w", err)
	}

	// Step 7: Issue #171 - Check for re-link (existing active item at same institution)
	var existingLinkedItem *domain.LinkedItem
	existingLinkedItem, err = s.linkedItemRepo.GetByUserAndInstitution(
		ctx, user.ID, institutionID, string(domain.LinkedItemStatusActive),
	)
	if err != nil && !errors.Is(err, repository.ErrLinkedItemNotFound) {
		// Unexpected error checking for existing item
		return nil, fmt.Errorf("failed to check for existing linked item: %w", err)
	}

	// Step 7b: Issue #896 - Fallback: check for disconnected item (re-subscription reconnection)
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

	// Step 8: Create new linked item (stores access token and links accounts)
	linkedItem := &domain.LinkedItem{
		UserID:               user.ID,
		ItemID:               itemID,
		AccessTokenEncrypted: encryptedToken,
		InstitutionID:        institutionID,
		InstitutionName:      institutionName,
		Provider:             "plaid",                       // Set provider to avoid database constraint violation (Issue #305)
		Status:               domain.LinkedItemStatusActive, // Issue #171: Explicitly set active status
	}

	// Create linked item
	if err := s.linkedItemRepo.Create(ctx, linkedItem); err != nil {
		return nil, fmt.Errorf("failed to create linked item: %w", err)
	}

	// Step 9: Issue #171 and #896 - If this is a re-link, soft-delete the old linked_item and accounts
	if existingLinkedItem != nil {
		s.logger.Info("re-link detected, soft-deleting old linked item",
			"old_item_id", existingLinkedItem.ID,
			"new_item_id", linkedItem.ID,
			"institution_id", institutionID,
		)

		// Mark old linked_item as 'replaced' with replaced_by_id pointing to new item
		now := time.Now()
		existingLinkedItem.Status = domain.LinkedItemStatusReplaced
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

	// Step 9b: Convert Plaid accounts to domain accounts with user_account_id
	accounts := make([]domain.Account, len(plaidAccounts))
	for i, plaidAccount := range plaidAccounts {
		account := convertPlaidAccount(plaidAccount, linkedItem.ID, user.ID)

		// Issue #171: Match or create user_account for stable reference across re-links
		if s.userAccountSvc != nil {
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
				s.logger.Error("failed to match/create user_account",
					"plaid_account_id", plaidAccount.GetAccountId(),
					"error", err,
				)
				// Continue without user_account_id - transactions can still be synced
			} else {
				account.UserAccountID = &userAccount.ID
			}
		}

		accounts[i] = account
	}

	// Step 10: Create accounts in batch
	if err := s.accountRepo.CreateBatch(ctx, accounts); err != nil {
		// TODO: Consider rollback mechanism if account creation fails
		// For now, the linked_item → accounts CASCADE DELETE will clean up on item deletion
		return nil, fmt.Errorf("failed to create accounts: %w", err)
	}

	// Step 11: Issue #658 - Enqueue initial sync job to fetch transactions
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
// 4. If zero accounts remain, enqueues a cleanup job to remove the Plaid item (stop billing)
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

	// Step 2: Delete the account
	if err := s.accountRepo.Delete(ctx, accountID, userID); err != nil {
		return err
	}

	// Step 3: Check if zero-account cleanup should be triggered
	// Only proceed if jobQueue is available (optional dependency)
	if s.jobQueue == nil {
		return nil
	}

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
		return nil
	}

	// Step 5: If no accounts remain, enqueue cleanup job
	if remainingCount == 0 {
		cleanupJob := itemCleanupJobArgs{
			LinkedItemID: linkedItemID,
			UserID:       userID,
			Reason:       "zero_accounts",
		}
		uniqueOpts := itemCleanupUniqueOpts()
		_, err := s.jobQueue.Enqueue(ctx, cleanupJob, queue.WithUnique(*uniqueOpts))
		if err != nil {
			// Log error but don't fail the deletion
			if s.logger != nil {
				s.logger.Warn("failed to enqueue item cleanup job",
					"linked_item_id", linkedItemID,
					"user_id", userID,
					"error", err,
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

	return nil
}

// DisconnectInstitution disconnects an institution (linked item) and all associated accounts.
//
// This method performs the following steps:
// 1. Retrieve the linked item with ownership validation
// 2. Decrypt the Plaid access token
// 3. Call Plaid /item/remove to stop monthly billing (critical for cost control - Issue #226)
// 4. Delete the linked item from database (CASCADE deletes all associated accounts)
//
// Note: If Plaid removal fails, we still delete from database since the access token
// is likely already invalid. This prevents orphaned database records.
//
// Returns repository.ErrLinkedItemNotFound if item doesn't exist or user doesn't own it.
func (s *accountService) DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error {
	// Step 1: Get linked item with ownership validation
	linkedItem, err := s.linkedItemRepo.GetByIDAndUser(ctx, linkedItemID, userID)
	if err != nil {
		return err // Returns ErrLinkedItemNotFound if not found or not owned
	}

	// Step 2: Decrypt the Plaid access token
	accessToken, err := s.encryptionSvc.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt access token: %w", err)
	}

	// Step 3: Call Plaid to remove the item (stops monthly billing)
	// This is critical for cost control per Issue #226
	// Log the error but continue - access token may already be invalid
	// We still want to clean up our database even if Plaid call fails
	if err := s.plaidService.RemoveItem(ctx, accessToken); err != nil {
		if s.logger != nil {
			s.logger.Warn("plaid RemoveItem failed for item",
				"item_id", linkedItemID,
				"error", err,
			)
		}
	}

	// Step 4: Delete from database (CASCADE deletes all accounts)
	return s.linkedItemRepo.DeleteByUser(ctx, linkedItemID, userID)
}

// GetAccountsFiltered retrieves accounts with optional hidden filter (Issue #307)
// Similar to GetAccounts but supports filtering hidden accounts.
func (s *accountService) GetAccountsFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
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

	// Query accounts with hidden filter using internal UUID
	accounts, err := s.accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID, includeHidden)
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
func (s *accountService) SetJobQueue(jq queue.JobQueue) {
	s.jobQueue = jq
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
