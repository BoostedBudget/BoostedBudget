package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAccountRepositoryForChainResolver is a lightweight inline mock for
// AccountRepository that only stubs the methods used by AccountChainResolver.
type mockAccountRepositoryForChainResolver struct {
	getByUserIDFn               func(ctx context.Context, userID string) ([]domain.Account, error)
	getByUserAccountAndStatusFn func(ctx context.Context, userAccountID, status string) ([]domain.Account, error)
	getByUserAccountIDFn        func(ctx context.Context, userAccountID string) ([]domain.Account, error)
}

func (m *mockAccountRepositoryForChainResolver) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	if m.getByUserIDFn != nil {
		return m.getByUserIDFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockAccountRepositoryForChainResolver) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error) {
	if m.getByUserAccountAndStatusFn != nil {
		return m.getByUserAccountAndStatusFn(ctx, userAccountID, status)
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
func (m *mockAccountRepositoryForChainResolver) Create(ctx context.Context, account *domain.Account) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) CreateBatch(ctx context.Context, accounts []domain.Account) error {
	return nil
}
func (m *mockAccountRepositoryForChainResolver) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
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
func (m *mockAccountRepositoryForChainResolver) Delete(ctx context.Context, accountID, userID string) error {
	return nil
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
func (m *mockAccountRepositoryForChainResolver) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error {
	return nil
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

	t.Run("returns active account ID", func(t *testing.T) {
		activeID := "acct-active-1"
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status string) ([]domain.Account, error) {
				assert.Equal(t, userAccountID, uaID)
				assert.Equal(t, string(domain.AccountStatusActive), status)
				return []domain.Account{{ID: activeID, Status: domain.AccountStatusActive}}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		resultID, err := svc.ResolveActiveID(ctx, userAccountID)
		require.NoError(t, err)
		assert.Equal(t, activeID, resultID)
	})

	t.Run("returns error when no active account found", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status string) ([]domain.Account, error) {
				return []domain.Account{}, nil
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, userAccountID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoActiveAccount)
	})

	t.Run("returns error for empty userAccountID", func(t *testing.T) {
		mock := &mockAccountRepositoryForChainResolver{}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, "")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrEmptyUserAccountID)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		expectedErr := errors.New("db error")
		mock := &mockAccountRepositoryForChainResolver{
			getByUserAccountAndStatusFn: func(ctx context.Context, uaID, status string) ([]domain.Account, error) {
				return nil, expectedErr
			},
		}
		svc := NewAccountChainResolver(mock)
		_, err := svc.ResolveActiveID(ctx, userAccountID)
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
