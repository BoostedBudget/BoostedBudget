package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*LinkAccountsResult, error)

	// GetAccounts retrieves all linked bank accounts for a user with institution information
	GetAccounts(ctx context.Context, userID string) ([]domain.Account, error)
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
//
// Flow:
// 1. Fetch user to get subscription tier
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
	// Step 1: Fetch user to get subscription tier
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	// Step 2: Check current account count against subscription limit (fail fast)
	currentAccountCount, err := s.accountRepo.CountByUserID(ctx, userID)
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
		UserID:               userID,
		ItemID:               itemID,
		AccessTokenEncrypted: encryptedToken,
		InstitutionID:        institutionID,
		InstitutionName:      institutionName,
	}

	// Create linked item
	if err := s.linkedItemRepo.Create(ctx, linkedItem); err != nil {
		return nil, fmt.Errorf("failed to create linked item: %w", err)
	}

	// Convert Plaid accounts to domain accounts
	accounts := make([]domain.Account, len(plaidAccounts))
	for i, plaidAccount := range plaidAccounts {
		account := convertPlaidAccount(plaidAccount, linkedItem.ID, userID)
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
func (s *accountService) GetAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	// Delegate to repository which performs LEFT JOIN with linked_items
	accounts, err := s.accountRepo.GetAccountsWithInstitution(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	return accounts, nil
}
