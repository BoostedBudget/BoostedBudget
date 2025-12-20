package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

var (
	// ErrAccountLimitExceeded is returned when user tries to link more accounts than their tier allows
	ErrAccountLimitExceeded = errors.New("account limit exceeded for current subscription tier")
	// ErrInvalidPublicToken is returned when Plaid rejects the public token
	ErrInvalidPublicToken = errors.New("invalid or expired public token")
	// ErrPlaidAPIFailure is returned when Plaid API calls fail
	ErrPlaidAPIFailure = errors.New("plaid API request failed")
)

// AccountService handles account linking and management
type AccountService interface {
	// LinkAccounts exchanges a Plaid public token for an access token and links bank accounts
	// NOTE: After PR #197, this accepts internal user UUID, not Auth0 ID
	LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*LinkAccountsResult, error)

	// GetAccounts retrieves all linked bank accounts for a user with institution information
	// NOTE: After PR #197, this accepts internal user UUID, not Auth0 ID
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
}

// LinkAccountsResult contains the results of linking bank accounts
type LinkAccountsResult struct {
	LinkedItem *domain.LinkedItem
	Accounts   []domain.Account
}

// accountService implements AccountService
type accountService struct {
	plaidService       PlaidService
	subscriptionSvc    *SubscriptionService
	encryptionSvc      EncryptionService
	linkedItemRepo     repository.LinkedItemRepository
	accountRepo        repository.AccountRepository
	userRepo           repository.ProfileRepository
	db                 *sql.DB
}

// NewAccountService creates a new AccountService instance
func NewAccountService(
	plaidService PlaidService,
	subscriptionSvc *SubscriptionService,
	encryptionSvc EncryptionService,
	linkedItemRepo repository.LinkedItemRepository,
	accountRepo repository.AccountRepository,
	userRepo repository.ProfileRepository,
	db *sql.DB,
) AccountService {
	return &accountService{
		plaidService:    plaidService,
		subscriptionSvc: subscriptionSvc,
		encryptionSvc:   encryptionSvc,
		linkedItemRepo:  linkedItemRepo,
		accountRepo:     accountRepo,
		userRepo:        userRepo,
		db:              db,
	}
}

// LinkAccounts exchanges a Plaid public token for an access token and links bank accounts
// NOTE: After PR #197, this method receives internal user UUID, not Auth0 ID
//
// Flow:
// 1. Fetch user by internal UUID to get subscription tier
// 2. Check current account count against subscription limit (fail fast)
// 3. Exchange public token for access token via Plaid API
// 4. Fetch accounts from Plaid
// 5. Verify combined count doesn't exceed limit
// 6. Encrypt access token
// 7. Store linked item and accounts atomically in database
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

	// Step 5: Verify combined count doesn't exceed limit
	newTotalCount := currentAccountCount + len(plaidAccounts)
	if newTotalCount > limitCheck.Limit {
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

	// Step 7: Store linked item and accounts atomically in database
	linkedItem := &domain.LinkedItem{
		UserID:               user.ID,
		ItemID:               itemID,
		AccessTokenEncrypted: encryptedToken,
		InstitutionID:        institutionID,
		InstitutionName:      institutionName,
		Provider:             "plaid", // Set provider to avoid database constraint violation (Issue #305)
	}

	// Create linked item
	if err := s.linkedItemRepo.Create(ctx, linkedItem); err != nil {
		return nil, fmt.Errorf("failed to create linked item: %w", err)
	}

	// Convert Plaid accounts to domain accounts
	accounts := make([]domain.Account, len(plaidAccounts))
	for i, plaidAccount := range plaidAccounts {
		account := convertPlaidAccount(plaidAccount, linkedItem.ID, user.ID)
		accounts[i] = account
	}

	// Create accounts in batch
	if err := s.accountRepo.CreateBatch(ctx, accounts); err != nil {
		// TODO: Consider rollback mechanism if account creation fails
		// For now, the linked_item → accounts CASCADE DELETE will clean up on item deletion
		return nil, fmt.Errorf("failed to create accounts: %w", err)
	}

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
// NOTE: After PR #197, this method receives internal user UUID, not Auth0 ID
func (s *accountService) GetAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	// Fetch user by internal UUID to verify user exists
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			// User authenticated via Auth0 but not yet in our database
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
// This method delegates to the repository layer which validates ownership via
// WHERE id = $1 AND user_id = $2. Only the account owner can delete their account.
//
// Use case: User wants to remove a single account (e.g., closed credit card) while
// keeping other accounts from the same institution.
//
// Returns repository.ErrAccountNotFound if account doesn't exist or user doesn't own it.
func (s *accountService) DeleteAccount(ctx context.Context, accountID, userID string) error {
	// Delegate to repository - it handles ownership validation
	return s.accountRepo.Delete(ctx, accountID, userID)
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
	err = s.plaidService.RemoveItem(ctx, accessToken)
	if err != nil {
		// Log the error but continue - access token may already be invalid
		// We still want to clean up our database even if Plaid call fails
		fmt.Printf("Warning: Plaid RemoveItem failed for item %s: %v\n", linkedItemID, err)
	}

	// Step 4: Delete from database (CASCADE deletes all accounts)
	err = s.linkedItemRepo.DeleteByUser(ctx, linkedItemID, userID)
	if err != nil {
		return err // Returns ErrLinkedItemNotFound if deletion fails
	}

	return nil
}

// GetAccountsFiltered retrieves accounts with optional hidden filter (Issue #307)
// Similar to GetAccounts but supports filtering hidden accounts.
func (s *accountService) GetAccountsFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
	// Fetch user by internal UUID to verify user exists
	_, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			// User authenticated via Auth0 but not yet in our database
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
