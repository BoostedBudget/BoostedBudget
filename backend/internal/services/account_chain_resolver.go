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

	// ErrAmbiguousActiveAccount is returned when MORE THAN ONE active account shares the given
	// user_account_id (#1569 R11 T2). This is an ordinary Plaid shape, not an edge case: two
	// maskless accounts at one institution are FORCED to share a user_account_id by migration
	// 000100's NULLS NOT DISTINCT unique index (see dashboard_service.go's
	// buildChainAwareAccountMap doc comment for the same collision documented elsewhere).
	//
	// Before this sentinel existed, ResolveActiveID returned accounts[0] from a query ordered
	// `ORDER BY created_at ASC` — deterministically the OLDER of the two live siblings — and
	// discarded the other with no signal. That silent pick is the defect. Every caller MUST
	// treat this distinctly from ErrNoActiveAccount: the chain is not dead, it is ambiguous, and
	// guessing which live sibling is "the" account is exactly the bug this sentinel exists to
	// stop a caller from re-introducing.
	ErrAmbiguousActiveAccount = errors.New("more than one active account found for user_account_id")
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
	// given user_account_id, SCOPED to userID (#1569 R11 T2 — the underlying
	// query previously had no user_id predicate at all). Used by write paths
	// (e.g. rule execution) that store a stable user_account_id and need the
	// current live account.id.
	// Returns ErrEmptyUserAccountID if userAccountID is empty.
	// Returns ErrNoActiveAccount if no active account exists in the chain.
	// Returns ErrAmbiguousActiveAccount if MORE THAN ONE active account shares
	// this user_account_id — this resolver refuses to guess between them, and
	// every caller must decide deliberately what to do with the ambiguity.
	ResolveActiveID(ctx context.Context, userAccountID, userID string) (string, error)

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

// ListActiveByUser returns all active accounts for the user.
// Defense-in-depth: client-side filter guarantees active-only across mock/test boundaries.
func (r *accountChainResolver) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	accounts, err := r.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	active := accounts[:0]
	for _, a := range accounts {
		if a.Status == domain.AccountStatusActive {
			active = append(active, a)
		}
	}
	return active, nil
}

// ResolveActiveID returns the account.id for the active account in the given user_account_id chain,
// scoped to userID. It refuses to guess when more than one active account shares the chain — see
// ErrAmbiguousActiveAccount.
func (r *accountChainResolver) ResolveActiveID(ctx context.Context, userAccountID, userID string) (string, error) {
	if userAccountID == "" {
		return "", ErrEmptyUserAccountID
	}

	accounts, err := r.accountRepo.GetByUserAccountAndStatus(ctx, userAccountID, string(domain.AccountStatusActive), userID)
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 {
		return "", ErrNoActiveAccount
	}
	if len(accounts) > 1 {
		// #1569 R11 T2: two live siblings sharing one user_account_id is an ordinary Plaid
		// shape (maskless accounts), not corruption. Silently returning accounts[0] here is
		// exactly the defect this task fixes — surface the ambiguity instead.
		return "", ErrAmbiguousActiveAccount
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
