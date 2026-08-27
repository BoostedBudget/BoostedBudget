package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAccountRepositoryForChainResolver is a lightweight inline mock for
// AccountRepository that only stubs the methods used by AccountChainResolver.
type mockAccountRepositoryForChainResolver struct {
	getByUserIDFn               func(ctx context.Context, userID string) ([]domain.Account, error)
	getByUserAccountAndStatusFn func(ctx context.Context, userAccountID, status, userID string) ([]domain.Account, error)
	getByUserAccountIDFn        func(ctx context.Context, userAccountID string) ([]domain.Account, error)
}

func (m *mockAccountRepositoryForChainResolver) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	if m.getByUserIDFn != nil {
		return m.getByUserIDFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockAccountRepositoryForChainResolver) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status, userID string) ([]domain.Account, error) {
	if m.getByUserAccountAndStatusFn != nil {
		return m.getByUserAccountAndStatusFn(ctx, userAccountID, status, userID)
	}
	return nil, nil
}

func (m *mockAccountRepositoryForChainResolver) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error) {
	if m.getByUserAccountIDFn != nil {
		return m.getByUserAccountIDFn(ctx, userAccountID)
	}
	return nil, nil
}

// Stub remaining AccountRepository methods to satisfy the interface.
func (m *mockAccountRepositoryForChainResolver) CreateBatch(ctx context.Context, userID string, accounts []domain.Account) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) GetByLinkedItemIDIncludingRetained(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) CountByUserID(ctx context.Context, userID string) (int, error) {
	return 0, nil
}
func (m *mockAccountRepositoryForChainResolver) CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error) {
	return 0, nil
}
func (m *mockAccountRepositoryForChainResolver) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) Update(ctx context.Context, account *domain.Account, userID string) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts repository.AccountFilterOptions) ([]domain.Account, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*repository.FingerprintOwnership, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) ([]string, error) {
	return nil, nil
}
func (m *mockAccountRepositoryForChainResolver) MarkRemovedByLinkedItemID(ctx context.Context, linkedItemID string, reason domain.RemovalReason) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) MarkRemovedByProvider(ctx context.Context, accountID, userID string) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) RestoreProviderRemovedAccount(ctx context.Context, accountID, userID string) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) PurgeAccount(ctx context.Context, accountID, userID string, auditLog repository.AuditLogRepository) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) GetByUserAccountIDAndUser(ctx context.Context, userAccountID, userID string) (*domain.Account, error) {
	return nil, nil
}

func (m *mockAccountRepositoryForChainResolver) GetForHouseholdScopeActiveNow(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error) {
	return nil, nil
}

func (m *mockAccountRepositoryForChainResolver) GetForHouseholdScopeLiveAsOf(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.Account, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// ListActiveByUser
// ---------------------------------------------------------------------------

func TestAccountChainResolver_ListActiveByUser(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	userAccountID1 := "ua-001"
	userAccountID2 := "ua-002"

	allAccounts := []domain.Account{
		// chain 1: one replaced + one active
		{
			ID:            "acct-old-1",
			UserID:        userID,
			UserAccountID: &userAccountID1,
			Status:        domain.AccountStatusReplaced,
		},
		{
			ID:            "acct-new-1",
			UserID:        userID,
			UserAccountID: &userAccountID1,
			Status:        domain.AccountStatusActive,
		},
		// chain 2: standalone active
		{
			ID:            "acct-new-2",
			UserID:        userID,
			UserAccountID: &userAccountID2,
			Status:        domain.AccountStatusActive,
		},
	}

	t.Run("returns only active accounts", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{
			getByUserIDFn: func(ctx context.Context, uid string) ([]domain.Account, error) {
				assert.Equal(t, userID, uid)
				return allAccounts, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		result, err := svc.ListActiveByUser(ctx, userID)
		require.NoError(t, err)
		require.Len(t, result, 2)
		for _, a := range result {
			assert.Equal(t, domain.AccountStatusActive, a.Status)
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		expectedErr := errors.New("db error")
		mock := &mockAccountRepositoryForChainResolver{
			getByUserIDFn: func(ctx context.Context, uid string) ([]domain.Account, error) {
				return nil, expectedErr
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ListActiveByUser(ctx, userID)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("empty result when no active accounts", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{
			getByUserIDFn: func(ctx context.Context, uid string) ([]domain.Account, error) {
				return []domain.Account{
					{ID: "old", Status: domain.AccountStatusReplaced},
				}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		result, err := svc.ListActiveByUser(ctx, userID)
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

// ---------------------------------------------------------------------------
// ResolveActiveID
// ---------------------------------------------------------------------------

func TestAccountChainResolver_ResolveActiveID(t *testing.T) {
	ctx := context.Background()
	userAccountID := "ua-001"
	userID := "user-abc"

	t.Run("returns active account ID", func(t *testing.T) {
		activeID := "acct-active-1"
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status, uID string) ([]domain.Account, error) {
				assert.Equal(t, userAccountID, uaID)
				assert.Equal(t, string(domain.AccountStatusActive), status)
				assert.Equal(t, userID, uID, "ResolveActiveID must thread userID through to the repository (#1569 R11 T2)")
				return []domain.Account{{ID: activeID, Status: domain.AccountStatusActive}}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		resultID, err := svc.ResolveActiveID(ctx, userAccountID, userID)
		require.NoError(t, err)
		assert.Equal(t, activeID, resultID)
	})

	t.Run("returns error when no active account found", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status, uID string) ([]domain.Account, error) {
				return []domain.Account{}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, userAccountID, userID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoActiveAccount)
	})

	// #1569 R11 T2: two live siblings sharing one user_account_id is an ordinary Plaid shape
	// (masked accounts at the same institution), not corruption. Before this fix,
	// GetByUserAccountAndStatus's ORDER BY created_at ASC meant accounts[0] was deterministically
	// the OLDER of the two, discarded silently. This test uses DIFFERENT balances on the two
	// siblings so a regression that silently resolves to either one is distinguishable from a
	// correct refusal — a fixture with equal balances proved nothing (see round 9-10's
	// $1,500/$1,500 mistake).
	t.Run("returns ErrAmbiguousActiveAccount when more than one active account shares the chain, and does not pick either", func(t *testing.T) {
		olderBalance := 1500.00
		newerBalance := 9999.00
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status, uID string) ([]domain.Account, error) {
				return []domain.Account{
					{ID: "acct-older-sibling", Status: domain.AccountStatusActive, CurrentBalance: &olderBalance},
					{ID: "acct-newer-sibling", Status: domain.AccountStatusActive, CurrentBalance: &newerBalance},
				}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		resultID, err := svc.ResolveActiveID(ctx, userAccountID, userID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAmbiguousActiveAccount)
		assert.Empty(t, resultID, "an ambiguous chain must not return either sibling's id")
	})

	t.Run("returns error for empty userAccountID", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, "", userID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrEmptyUserAccountID)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		expectedErr := errors.New("db error")
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status, uID string) ([]domain.Account, error) {
				return nil, expectedErr
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, userAccountID, userID)
		require.ErrorIs(t, err, expectedErr)
	})
}

// ---------------------------------------------------------------------------
// ChainAccountIDs
// ---------------------------------------------------------------------------

func TestAccountChainResolver_ChainAccountIDs(t *testing.T) {
	ctx := context.Background()
	userAccountID := "ua-001"

	t.Run("returns all IDs in chain (replaced + active)", func(t *testing.T) {
		// depth-2 chain: old -> mid -> active
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountIDFn: func(ctx context.Context, uaID string) ([]domain.Account, error) {
				assert.Equal(t, userAccountID, uaID)
				return []domain.Account{
					{ID: "acct-old", Status: domain.AccountStatusReplaced},
					{ID: "acct-mid", Status: domain.AccountStatusReplaced},
					{ID: "acct-new", Status: domain.AccountStatusActive},
				}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		ids, err := svc.ChainAccountIDs(ctx, userAccountID)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"acct-old", "acct-mid", "acct-new"}, ids)
	})

	t.Run("returns empty slice for empty chain", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountIDFn: func(ctx context.Context, uaID string) ([]domain.Account, error) {
				return []domain.Account{}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		ids, err := svc.ChainAccountIDs(ctx, userAccountID)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("returns error for empty userAccountID", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ChainAccountIDs(ctx, "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrEmptyUserAccountID)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		expectedErr := errors.New("db error")
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountIDFn: func(ctx context.Context, uaID string) ([]domain.Account, error) {
				return nil, expectedErr
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ChainAccountIDs(ctx, userAccountID)
		require.ErrorIs(t, err, expectedErr)
	})
}
