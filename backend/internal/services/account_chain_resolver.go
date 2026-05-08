package services

import (
	"context"
	"errors"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Sentinel errors returned by AccountChainResolver methods.
var (
	// ErrNoActiveAccount is returned when no active account exists for a given user_account_id.
	ErrNoActiveAccount = errors.New("no active account found for user_account_id")

	// ErrEmptyUserAccountID is returned when an empty user_account_id is provided.
	ErrEmptyUserAccountID = errors.New("user_account_id must not be empty")
)

// AccountChainResolver resolves the re-link account chain for a user.
// When Plaid accounts are re-linked, new account rows are created with
// status='active' and old rows are marked status='replaced'. All rows in the
// same chain share the same user_account_id. This service centralises
// status-aware lookups so consumer code never accidentally uses replaced rows.
type AccountChainResolver interface {
	// ListActiveByUser returns only status='active' accounts for a user.
	// This is the safe replacement for direct accountRepo.GetByUserID calls
	// in contexts where replaced rows must be excluded.
	ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error)

	// ResolveActiveID returns the account.id where status='active' for the
	// given user_account_id. Used by write paths (e.g. rule execution) that
	// store a stable user_account_id and need the current live account.id.
	// Returns ErrEmptyUserAccountID if userAccountID is empty.
	// Returns ErrNoActiveAccount if no active account exists in the chain.
	ResolveActiveID(ctx context.Context, userAccountID string) (string, error)

	// ChainAccountIDs returns ALL account.id values in the chain for the given
	// user_account_id (both active and replaced). Needed for history queries
	// that must include transactions from previous account rows.
	// Returns ErrEmptyUserAccountID if userAccountID is empty.
	ChainAccountIDs(ctx context.Context, userAccountID string) ([]string, error)
}

type accountChainResolver struct {
	accountRepo repository.AccountRepository
}

// NewAccountChainResolver constructs an AccountChainResolver backed by the
// provided AccountRepository.
func NewAccountChainResolver(accountRepo repository.AccountRepository) AccountChainResolver {
	return &accountChainResolver{accountRepo: accountRepo}
}

// ListActiveByUser fetches all accounts for the user and filters to active only.
// Legacy rows with a null user_account_id are included if their status is active.
func (r *accountChainResolver) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	all, err := r.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	active := make([]domain.Account, 0, len(all))
	for _, a := range all {
		if a.Status == domain.AccountStatusActive {
			active = append(active, a)
		}
	}
	return active, nil
}

// ResolveActiveID looks up the single active account for the given user_account_id
// and returns its account.id.
func (r *accountChainResolver) ResolveActiveID(ctx context.Context, userAccountID string) (string, error) {
	if userAccountID == "" {
		return "", ErrEmptyUserAccountID
	}

	accounts, err := r.accountRepo.GetByUserAccountAndStatus(ctx, userAccountID, string(domain.AccountStatusActive))
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 {
		return "", ErrNoActiveAccount
	}
	return accounts[0].ID, nil
}

// ChainAccountIDs returns every account.id in the chain (active + replaced)
// for the given user_account_id.
func (r *accountChainResolver) ChainAccountIDs(ctx context.Context, userAccountID string) ([]string, error) {
	if userAccountID == "" {
		return nil, ErrEmptyUserAccountID
	}

	accounts, err := r.accountRepo.GetByUserAccountID(ctx, userAccountID)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	return ids, nil
}
