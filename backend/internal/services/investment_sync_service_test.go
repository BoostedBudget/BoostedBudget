package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Repositories
// ============================================================================

type MockSecurityRepository struct {
	mock.Mock
}

func (m *MockSecurityRepository) UpsertBatch(ctx context.Context, securities []domain.Security) (map[string]string, error) {
	args := m.Called(ctx, securities)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

// FindForkedIdentities and CountForkedIdentityGroups (#1631 M8b) are unused by the sync path this
// mock backs — the sync service never calls them — so they are stubbed to satisfy
// repository.SecurityRepository rather than exercised here. See
// system_health_service_forked_identity_test.go for their real coverage.
func (m *MockSecurityRepository) FindForkedIdentities(ctx context.Context) ([]domain.ForkedSecurityIdentity, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.ForkedSecurityIdentity), args.Error(1)
}

func (m *MockSecurityRepository) CountForkedIdentityGroups(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

type MockHoldingRepository struct {
	mock.Mock
}

// ReplaceForAccount returns the UTC day the history capture filed under. Existing tests set only
// an error expectation, so a missing first return value falls back to the zero Time — which is
// exactly what TestInvestmentSyncService_SyncHoldings_FilesTheAggregateUnderTheCapturedDay relies
// on being visible rather than papered over.
func (m *MockHoldingRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) (time.Time, error) {
	args := m.Called(ctx, accountID, userID, holdings)
	day, _ := args.Get(0).(time.Time)
	if len(args) > 1 {
		return day, args.Error(1)
	}
	return day, nil
}

func (m *MockHoldingRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Holding), args.Error(1)
}

func (m *MockHoldingRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Holding), args.Error(1)
}

func (m *MockHoldingRepository) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Holding, error) {
	args := m.Called(ctx, userAccountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Holding), args.Error(1)
}

func (m *MockHoldingRepository) GetByUserIDWithSecurities(ctx context.Context, userID string, accountID *string) ([]domain.Holding, error) {
	args := m.Called(ctx, userID, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Holding), args.Error(1)
}

func (m *MockHoldingRepository) GetDistinctUserIDsWithHoldings(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// MockPortfolioSnapshotRepository implements repository.PortfolioSnapshotRepository for testing.
type MockPortfolioSnapshotRepository struct {
	mock.Mock
}

func (m *MockPortfolioSnapshotRepository) Upsert(ctx context.Context, userID string, snapshotDate time.Time) (*domain.PortfolioSnapshot, error) {
	args := m.Called(ctx, userID, snapshotDate)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PortfolioSnapshot), args.Error(1)
}

func (m *MockPortfolioSnapshotRepository) GetByUserDateRange(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioSnapshot, error) {
	args := m.Called(ctx, userID, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PortfolioSnapshot), args.Error(1)
}

func (m *MockPortfolioSnapshotRepository) GetLatestBefore(ctx context.Context, userID string, date time.Time) (*domain.PortfolioSnapshot, error) {
	args := m.Called(ctx, userID, date)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PortfolioSnapshot), args.Error(1)
}

// #1569 Task 11: the coverage-limited repair of past portfolio_snapshots days. Not exercised by
// the sync service, which only ever writes TODAY.
func (m *MockPortfolioSnapshotRepository) RepairUserFromHistory(ctx context.Context, userID string) (domain.PortfolioRepairSummary, error) {
	args := m.Called(ctx, userID)
	sum, _ := args.Get(0).(domain.PortfolioRepairSummary)
	return sum, args.Error(1)
}

func (m *MockPortfolioSnapshotRepository) UserIDsWithUnexaminedSnapshots(ctx context.Context, limit int) ([]string, error) {
	args := m.Called(ctx, limit)
	ids, _ := args.Get(0).([]string)
	return ids, args.Error(1)
}

type MockInvestmentTransactionRepository struct {
	mock.Mock
}

func (m *MockInvestmentTransactionRepository) UpsertBatch(ctx context.Context, txns []domain.InvestmentTransaction) (int, error) {
	args := m.Called(ctx, txns)
	return args.Int(0), args.Error(1)
}

func (m *MockInvestmentTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InvestmentTransaction), args.Error(1)
}

func (m *MockInvestmentTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InvestmentTransaction), args.Error(1)
}

func (m *MockInvestmentTransactionRepository) GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	args := m.Called(ctx, userAccountID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InvestmentTransaction), args.Error(1)
}

func (m *MockInvestmentTransactionRepository) GetByUserIDKeyset(ctx context.Context, userID string, filters domain.InvestmentTransactionFilters, cursor *domain.InvestmentTransactionCursor, limit int) ([]domain.InvestmentTransaction, *domain.InvestmentTransactionCursor, error) {
	args := m.Called(ctx, userID, filters, cursor, limit)
	txns, _ := args.Get(0).([]domain.InvestmentTransaction)
	cur, _ := args.Get(1).(*domain.InvestmentTransactionCursor)
	return txns, cur, args.Error(2)
}

// ============================================================================
// Plaid SDK Helpers
// ============================================================================

func plaidNullStr(s string) plaid.NullableString {
	return *plaid.NewNullableString(&s)
}

func plaidNilStr() plaid.NullableString {
	return *plaid.NewNullableString(nil)
}

func plaidNullFloat(f float64) plaid.NullableFloat64 {
	return *plaid.NewNullableFloat64(&f)
}

func plaidNilFloat() plaid.NullableFloat64 {
	return *plaid.NewNullableFloat64(nil)
}

func plaidNullBool(b bool) plaid.NullableBool {
	return *plaid.NewNullableBool(&b)
}

// makePlaidSecurity creates a minimal plaid.Security for testing.
func makePlaidSecurity(securityID, name, ticker string) plaid.Security {
	return plaid.Security{
		SecurityId:             securityID,
		Name:                   plaidNullStr(name),
		TickerSymbol:           plaidNullStr(ticker),
		Type:                   plaidNullStr("equity"),
		Isin:                   plaidNilStr(),
		Cusip:                  plaidNilStr(),
		IsCashEquivalent:       plaidNullBool(false),
		ClosePrice:             plaidNullFloat(150.00),
		ClosePriceAsOf:         plaidNilStr(),
		IsoCurrencyCode:        plaidNullStr("USD"),
		UnofficialCurrencyCode: plaidNilStr(),
		MarketIdentifierCode:   plaidNilStr(),
	}
}

// TestMapPlaidSecuritiesToDomain_NameSynthesis covers #1631's root-cause fix: Plaid sending a
// null/blank security name used to leave domain.Security.Name as "", and nothing on the sync path
// ever called Security.Validate() to catch it — the NOT NULL column constraint is satisfied by an
// empty string.
// Synthesising a name (rather than skipping the row) was the chosen fix because UpsertBatch's ON
// CONFLICT never repairs security_id afterwards, so dropping the row would strand any investment
// transaction that already references it.
func TestMapPlaidSecuritiesToDomain_NameSynthesis(t *testing.T) {
	t.Run("keeps the provided name when non-blank", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-1", "Apple Inc.", "AAPL")
		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "Apple Inc.", result[0].Name)
	})

	t.Run("falls back to the ticker symbol when name is a null Plaid string", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-2", "", "VCIT")
		sec.Name = plaidNilStr()
		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "VCIT", result[0].Name)
	})

	t.Run("falls back to the ticker symbol when name is an empty string", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-3", "", "IEMG")
		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "IEMG", result[0].Name)
	})

	t.Run("falls back to a synthesised label from the plaid security id when both name and ticker are blank", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-4", "", "")
		sec.TickerSymbol = plaidNilStr()
		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "Security plaid-sec-4", result[0].Name)
	})

	t.Run("treats a whitespace-only name as blank", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-5", "   ", "")
		sec.TickerSymbol = plaidNilStr()
		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "Security plaid-sec-5", result[0].Name)
	})
}

// TestMapPlaidSecuritiesToDomain_ConsolidationIdentifiers covers #1631: Plaid gives us two
// identifiers beyond plaid_security_id for consolidating a security's forked identities —
// institution_security_id and proxy_security_id. Before this, mapPlaidSecuritiesToDomain dropped
// both on the floor even though the Plaid SDK response carries them.
func TestMapPlaidSecuritiesToDomain_ConsolidationIdentifiers(t *testing.T) {
	t.Run("threads institution_security_id and proxy_security_id through when present", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-1", "iShares Core U.S. Aggregate Bond ETF", "AGG")
		sec.InstitutionSecurityId = plaidNullStr("INST-AGG-1")
		sec.ProxySecurityId = plaidNullStr("PROXY-BOND-1")

		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)

		require.Len(t, result, 1)
		require.NotNil(t, result[0].InstitutionSecurityID)
		assert.Equal(t, "INST-AGG-1", *result[0].InstitutionSecurityID)
		require.NotNil(t, result[0].ProxySecurityID)
		assert.Equal(t, "PROXY-BOND-1", *result[0].ProxySecurityID)
	})

	t.Run("leaves both nil when Plaid sends neither", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-2", "Tesla Inc.", "TSLA")

		result := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)

		require.Len(t, result, 1)
		assert.Nil(t, result[0].InstitutionSecurityID)
		assert.Nil(t, result[0].ProxySecurityID)
	})
}

// makePlaidHolding creates a minimal plaid.Holding for testing.
func makePlaidHolding(plaidAccountID, plaidSecurityID string, qty, price, value, costBasis float64) plaid.Holding {
	return plaid.Holding{
		AccountId:              plaidAccountID,
		SecurityId:             plaidSecurityID,
		Quantity:               qty,
		InstitutionPrice:       price,
		InstitutionValue:       value,
		CostBasis:              plaidNullFloat(costBasis),
		IsoCurrencyCode:        plaidNullStr("USD"),
		UnofficialCurrencyCode: plaidNilStr(),
	}
}

// makePlaidInvTxn creates a minimal plaid.InvestmentTransaction for testing.
func makePlaidInvTxn(txnID, accountID, securityID string, amount float64, date string) plaid.InvestmentTransaction {
	return plaid.InvestmentTransaction{
		InvestmentTransactionId: txnID,
		AccountId:               accountID,
		SecurityId:              plaidNullStr(securityID),
		Date:                    date,
		Name:                    "Test Transaction",
		Quantity:                10.0,
		Amount:                  amount,
		Price:                   15.0,
		Fees:                    plaidNullFloat(0.5),
		Type:                    plaid.INVESTMENTTRANSACTIONTYPE_BUY,
		Subtype:                 "buy",
		IsoCurrencyCode:         plaidNullStr("USD"),
		UnofficialCurrencyCode:  plaidNilStr(),
	}
}

// makePlaidCashTxn creates a cash investment transaction (nil security_id) for testing.
func makePlaidCashTxn(txnID, accountID string, amount float64, date string) plaid.InvestmentTransaction {
	return plaid.InvestmentTransaction{
		InvestmentTransactionId: txnID,
		AccountId:               accountID,
		SecurityId:              plaidNilStr(), // nil for cash transactions
		Date:                    date,
		Name:                    "Cash Dividend",
		Quantity:                0.0,
		Amount:                  amount,
		Price:                   0.0,
		Fees:                    plaidNilFloat(),
		Type:                    plaid.INVESTMENTTRANSACTIONTYPE_CASH,
		Subtype:                 "dividend",
		IsoCurrencyCode:         plaidNullStr("USD"),
		UnofficialCurrencyCode:  plaidNilStr(),
	}
}

func makeMinimalPlaidItem() plaid.Item {
	return *plaid.NewItemWithDefaults()
}

// ============================================================================
// Service Factory Helper
// ============================================================================

type investmentSyncDeps struct {
	webhookRepo    *MockWebhookRepository
	linkedItemRepo *MockLinkedItemRepository
	accountRepo    *MockAccountRepository
	securityRepo   *MockSecurityRepository
	holdingRepo    *MockHoldingRepository
	invTxnRepo     *MockInvestmentTransactionRepository
	snapshotRepo   *MockPortfolioSnapshotRepository
	// userAccountRepo carries the #1631 investment backfill obligation. Tests that are not about
	// the backfill register expectNoBackfillOwed (investment_sync_exclusion_test.go).
	userAccountRepo *MockUserAccountRepository
	plaidSvc        *MockPlaidService
}

func setupInvestmentSyncService(t *testing.T) (InvestmentSyncService, investmentSyncDeps) {
	t.Helper()
	deps := investmentSyncDeps{
		webhookRepo:     new(MockWebhookRepository),
		linkedItemRepo:  new(MockLinkedItemRepository),
		accountRepo:     new(MockAccountRepository),
		securityRepo:    new(MockSecurityRepository),
		holdingRepo:     new(MockHoldingRepository),
		invTxnRepo:      new(MockInvestmentTransactionRepository),
		snapshotRepo:    new(MockPortfolioSnapshotRepository),
		userAccountRepo: new(MockUserAccountRepository),
		plaidSvc:        new(MockPlaidService),
	}
	// Default permissive "nothing owed" answer: the overwhelming majority of these tests predate
	// the backfill obligation and are not about it.
	//
	// ⚠ A test that IS about the obligation must NOT add a second expectation on top of this one.
	// testify matches expectations in REGISTRATION order, so this mock.Anything default wins and
	// the specific one is never consulted — which silently turns a backfill test into an
	// incremental-sync test. Use newSyncServiceWithUserAccountRepo (investment_sync_exclusion_test.go)
	// to swap in a fresh mock instead.
	deps.userAccountRepo.On("PendingInvestmentBackfills", mock.Anything, mock.Anything, mock.Anything).
		Return(map[string]time.Time{}, nil).Maybe()
	// Default permissive expectation for snapshot upsert.
	// Tests that need to verify snapshot behavior set up specific expectations instead.
	deps.snapshotRepo.On("Upsert", mock.Anything, mock.Anything, mock.Anything).
		Return((*domain.PortfolioSnapshot)(nil), nil).Maybe()
	svc := NewInvestmentSyncService(
		deps.webhookRepo,
		deps.linkedItemRepo,
		deps.accountRepo,
		deps.securityRepo,
		deps.holdingRepo,
		deps.invTxnRepo,
		deps.snapshotRepo,
		deps.userAccountRepo,
		deps.plaidSvc,
		nil, // logger
	)
	return svc, deps
}

// makeLinkedItem creates a test LinkedItem with AccessToken already decrypted (repo-layer encryption).
func makeLinkedItem(itemID, userID string) *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:          uuid.New().String(),
		ItemID:      itemID,
		UserID:      userID,
		AccessToken: "tok", // repo layer provides decrypted token
	}
}

// ============================================================================
// SyncHoldings Tests
// ============================================================================

// TestInvestmentSyncService_SyncHoldings_HappyPath verifies that securities are
// upserted and holdings are replaced per account on a normal sync.
func TestInvestmentSyncService_SyncHoldings_HappyPath(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-001"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)
	// AccessToken is already decrypted by the repository layer
	accessToken := linkedItem.AccessToken

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)

	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{holding},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, accessToken).Return(holdingsResp, nil)

	// Securities upserted → map returned
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
	}, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.SecuritiesUpserted)
	assert.Equal(t, 1, result.HoldingsReplaced)
	assert.Equal(t, 1, result.AccountsProcessed)
	deps.securityRepo.AssertExpectations(t)
	deps.holdingRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncHoldings_SecurityMappingCorrect verifies that
// the plaid_security_id is correctly resolved to the internal UUID when storing holdings.
func TestInvestmentSyncService_SyncHoldings_SecurityMappingCorrect(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-AAPL"
	internalSecurityID := "internal-uuid-AAPL-" + uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 5.0, 165.0, 825.0, 750.0)

	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{holding},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
	}, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Critical assertion: holding must have the correct internal SecurityID
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.MatchedBy(func(h []domain.Holding) bool {
		if len(h) != 1 {
			return false
		}
		return h[0].SecurityID == internalSecurityID
	})).Return(nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.HoldingsReplaced)
	deps.holdingRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncHoldings_EmptyHoldings verifies that an account
// with no holdings (fully liquidated) still triggers a delete (ReplaceForAccount with empty slice).
func TestInvestmentSyncService_SyncHoldings_EmptyHoldings(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	internalAccountID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	// No securities, no holdings in response (account was fully liquidated) — but the account
	// IS present in the response's `accounts` array, confirming Plaid enumerated it this call
	// and genuinely reports zero holdings, not a partial/incomplete response for it.
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{{AccountId: plaidAccountID}},
		[]plaid.Holding{},
		[]plaid.Security{},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	deps.securityRepo.On("UpsertBatch", ctx, []domain.Security{}).Return(map[string]string{}, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// ReplaceForAccount must be called with empty slice (clears the account)
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, []domain.Holding{}).Return(nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 0, result.HoldingsReplaced)
	assert.Equal(t, 1, result.AccountsProcessed) // account still processed (cleared)
	deps.holdingRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncHoldings_PartialResponse_PreservesHoldings verifies the Issue
// #1569 Task 4 fix: an account with no holdings in the response AND absent from the response's
// `accounts` array (the only per-account completeness signal Plaid exposes, see
// plaid_completeness.go) must NOT be treated as "fully liquidated." Existing holdings rows must
// be preserved, and the sync must surface the skip on SkippedIncomplete (NOT Errors — fix round
// 1 caught that Errors also gates the portfolio snapshot upsert, and a permanently-absent account
// would otherwise suppress the user's snapshot forever) so the caller (ProcessWebhook) retries
// instead of silently accepting a partial response as final.
func TestInvestmentSyncService_SyncHoldings_PartialResponse_PreservesHoldings(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	before := testutil.ToFloat64(observability.PlaidWipeSkippedIncompleteResponse.WithLabelValues("holdings"))

	internalAccountID := uuid.New().String()
	plaidAccountID := "plaid-acct-partial-holdings"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	// No holdings for this account AND the account itself is absent from `accounts` — Plaid's
	// response says nothing about this account at all this call (a partial/degraded response),
	// which is NOT the same as "confirmed fully liquidated."
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{},
		[]plaid.Security{},
		makeMinimalPlaidItem(),
		"req-partial-holdings",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	deps.securityRepo.On("UpsertBatch", ctx, []domain.Security{}).Return(map[string]string{}, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	// ReplaceForAccount must NEVER be called for this account — existing holdings are preserved.
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", ctx, internalAccountID, userID, []domain.Holding{})
	assert.Equal(t, 0, result.AccountsProcessed)
	// A partial response must surface on SkippedIncomplete (so ProcessWebhook still retries) —
	// but NOT on Errors, which gates the portfolio snapshot upsert.
	assert.Empty(t, result.Errors, "a partial/incomplete response is not a correctness failure — "+
		"putting it on Errors would suppress the portfolio snapshot")
	require.Len(t, result.SkippedIncomplete, 1)
	assert.Contains(t, result.SkippedIncomplete[0], plaidAccountID)
	assert.Equal(t, before+1, testutil.ToFloat64(observability.PlaidWipeSkippedIncompleteResponse.WithLabelValues("holdings")),
		"a refactor that silently drops the .Inc() call must be visible here, not just in the log line")
}

// TestInvestmentSyncService_SyncHoldings_UnmappedAccount verifies that holdings for
// a Plaid account that has no matching internal account are gracefully skipped.
func TestInvestmentSyncService_SyncHoldings_UnmappedAccount(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	// Plaid returns a holding for an account we don't have internally
	plaidSecurityID := "plaid-sec-001"
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{
			makePlaidHolding("unknown-plaid-acct", plaidSecurityID, 5.0, 100.0, 500.0, 400.0),
		},
		[]plaid.Security{makePlaidSecurity(plaidSecurityID, "Test Security", "TST")},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: uuid.New().String(),
	}, nil)

	// No matching internal accounts → accountMap is empty
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// holdingRepo.ReplaceForAccount should NOT be called (no mapped accounts)
	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 0, result.AccountsProcessed)
	assertNoCallTo(t, &deps.holdingRepo.Mock, "ReplaceForAccount")
}

// TestInvestmentSyncService_SyncHoldings_PlaidAPIError verifies that Plaid API
// errors are propagated without crashing.
func TestInvestmentSyncService_SyncHoldings_PlaidAPIError(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	linkedItem := makeLinkedItem("item-001", "user-001")

	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").
		Return(nil, errors.New("plaid: PRODUCT_NOT_READY"))

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRODUCT_NOT_READY")
	assert.Nil(t, result)
}

// TestInvestmentSyncService_SyncHoldings_SnapshotUpsertedOnSuccess verifies that a
// portfolio snapshot is upserted after a fully successful holdings sync (no skipped accounts).
func TestInvestmentSyncService_SyncHoldings_SnapshotUpsertedOnSuccess(t *testing.T) {
	_, baseDeps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-snap"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-snap"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-snap", userID)

	sec := makePlaidSecurity(plaidSecurityID, "Snap Inc.", "SNAP")
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 15.0, 150.0, 120.0)
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{holding},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-snap",
	)
	baseDeps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	baseDeps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
	}, nil)
	baseDeps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)
	baseDeps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)

	// Use a fresh snapshot mock (no Maybe default) to assert Upsert is called exactly once.
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	snap := &domain.PortfolioSnapshot{ID: uuid.New().String(), UserID: userID}
	snapshotRepo.On("Upsert", ctx, userID, mock.AnythingOfType("time.Time")).Return(snap, nil)

	svc := NewInvestmentSyncService(
		baseDeps.webhookRepo,
		baseDeps.linkedItemRepo,
		baseDeps.accountRepo,
		baseDeps.securityRepo,
		baseDeps.holdingRepo,
		baseDeps.invTxnRepo,
		snapshotRepo,
		baseDeps.userAccountRepo,
		baseDeps.plaidSvc,
		nil,
	)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.AccountsProcessed)
	assert.Empty(t, result.Errors, "no errors → snapshot must be upserted")
	snapshotRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncHoldings_SnapshotSkippedOnPartialError verifies that
// the portfolio snapshot upsert is skipped when accounts are skipped due to unmapped securities.
// A partial sync result must not become a day-change baseline.
func TestInvestmentSyncService_SyncHoldings_SnapshotSkippedOnPartialError(t *testing.T) {
	_, baseDeps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-partial"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-partial", userID)

	// Plaid returns a holding for a security that is NOT in the security map → skippedAny = true
	unknownSecurityID := "plaid-sec-unknown"
	holding := makePlaidHolding(plaidAccountID, unknownSecurityID, 5.0, 100.0, 500.0, 400.0)
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{holding},
		[]plaid.Security{}, // security not returned → not in map
		makeMinimalPlaidItem(),
		"req-partial",
	)
	baseDeps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	// UpsertBatch returns empty map → the security lookup fails
	baseDeps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{}, nil)
	baseDeps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Snapshot repo must NOT be called when accounts are skipped.
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	// No On("Upsert", ...) registered — unexpected call would fail the test.

	svc := NewInvestmentSyncService(
		baseDeps.webhookRepo,
		baseDeps.linkedItemRepo,
		baseDeps.accountRepo,
		baseDeps.securityRepo,
		baseDeps.holdingRepo,
		baseDeps.invTxnRepo,
		snapshotRepo,
		baseDeps.userAccountRepo,
		baseDeps.plaidSvc,
		nil,
	)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Errors, "should have errors for skipped account")
	snapshotRepo.AssertNotCalled(t, "Upsert")
}

// ============================================================================
// SyncInvestmentTransactions Tests
// ============================================================================

// TestInvestmentSyncService_SyncInvestmentTransactions_FirstSync_NoLastSyncedDate verifies
// that on first sync (no lastSyncedDate), a 2-year lookback is used as the start date.
func TestInvestmentSyncService_SyncInvestmentTransactions_FirstSync_NoLastSyncedDate(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)
	// No InvestmentsLastSyncedDate → first sync
	// AccessToken is already decrypted by the repository layer

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	expectedStartDate := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")

	txn := makePlaidCashTxn("txn-001", plaidAccountID, 500.0, "2024-01-15")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{},
		[]plaid.InvestmentTransaction{txn},
		1, // totalInvestmentTransactions
		"req-001",
	)

	// Verify start date is 2-year lookback
	deps.plaidSvc.On("InvestmentsTransactionsGet",
		ctx, linkedItem.AccessToken,
		mock.MatchedBy(func(sd string) bool { return sd == expectedStartDate }),
		mock.AnythingOfType("string"), // endDate
		int32(0), int32(500),
	).Return(txnResp, nil)

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	assert.Equal(t, 1, result.PagesProcessed)
	deps.plaidSvc.AssertExpectations(t)
	deps.linkedItemRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_RetainedAccount_StillStored is THE
// regression test for Issue #1569 round 11 task 1: the exact defect task 1b already fixed for
// banking (transaction_sync_service.go:915, GetByLinkedItemIDIncludingRetained), left in place
// one file over for investment transactions.
//
// A user disconnects (or Plaid retires) one brokerage account inside a linked item that keeps
// syncing. The account row survives as 'removed' — investment_transactions carries no cascade
// or wipe on that path, and investmentTransactionRepository.UpsertBatch is a pure
// INSERT ... ON CONFLICT DO UPDATE (semantically identical to the banking path), so there is no
// wipe-and-reinsert reason to scope the account map to 'active' only here. Before this fix,
// SyncInvestmentTransactions built its account map from active-only GetByLinkedItemID, so the
// retained account never appears in accountMap and mapPlaidInvestmentTransactions silently
// Warn+skips every transaction Plaid delivers for it, forever.
//
// Mutation-check: reverting investment_sync_service.go:498 to GetByLinkedItemID must turn this
// RED, since the mock below only ever stubs GetByLinkedItemIDIncludingRetained.
func TestInvestmentSyncService_SyncInvestmentTransactions_RetainedAccount_StillStored(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-retained"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	// The account was disconnected but the linked item keeps syncing (sibling accounts
	// remain active); GetByLinkedItemIDIncludingRetained must be the source, not
	// GetByLinkedItemID, or this retained account never lands in the map.
	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID, Status: domain.AccountStatusRemoved},
	}, nil)

	txn := makePlaidCashTxn("txn-retained-001", plaidAccountID, 250.0, "2024-01-15")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{},
		[]plaid.InvestmentTransaction{txn},
		1,
		"req-001",
	)

	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, linkedItem.AccessToken, mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	// The transaction for the retained account must actually be stored (mapped to its
	// internal account ID), not silently dropped.
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		if len(txns) != 1 {
			return false
		}
		return txns[0].AccountID == internalAccountID
	})).Return(1, nil)

	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted,
		"a retained account's investment transaction must be stored, not permanently skipped")
	deps.accountRepo.AssertExpectations(t)
	deps.invTxnRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_IncrementalSync_UsesLastSyncedDate
// verifies that on incremental sync, startDate = lastSyncedDate - 1 day (overlap for safety).
func TestInvestmentSyncService_SyncInvestmentTransactions_IncrementalSync_UsesLastSyncedDate(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()

	lastSyncedDate := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	linkedItem := makeLinkedItem("item-001", userID)
	linkedItem.InvestmentsLastSyncedDate = &lastSyncedDate
	// AccessToken is already decrypted by the repository layer

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// CRITICAL: startDate must be lastSyncedDate - 1 day = "2024-01-09"
	expectedStartDate := "2024-01-09"

	txn := makePlaidCashTxn("txn-incremental-001", plaidAccountID, 250.0, "2024-01-10")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{},
		[]plaid.InvestmentTransaction{txn},
		1,
		"req-001",
	)

	deps.plaidSvc.On("InvestmentsTransactionsGet",
		ctx, linkedItem.AccessToken, expectedStartDate,
		mock.AnythingOfType("string"),
		int32(0), int32(500),
	).Return(txnResp, nil)

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	deps.plaidSvc.AssertExpectations(t) // asserts startDate matched
}

// TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_SinglePage verifies that
// a response with total <= 500 (one full page) results in exactly one API call.
func TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_SinglePage(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// 3 transactions, total=3 → one page, loop ends
	txns := []plaid.InvestmentTransaction{
		makePlaidCashTxn("txn-1", plaidAccountID, 100.0, "2024-01-01"),
		makePlaidCashTxn("txn-2", plaidAccountID, 200.0, "2024-01-02"),
		makePlaidCashTxn("txn-3", plaidAccountID, 300.0, "2024-01-03"),
	}
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), []plaid.AccountBase{}, []plaid.Security{},
		txns, 3, "req-001",
	)

	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil).Once() // exactly once

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(3, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 3, result.TransactionsInserted)
	assert.Equal(t, 1, result.PagesProcessed)
	deps.plaidSvc.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_MultiplePages verifies
// that offset-based pagination makes the correct API calls (offsets 0, 500, 1000) for
// total=1200 transactions, and all 1200 transactions are processed.
func TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_MultiplePages(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	totalTxns := int32(1200)

	// Page 1: offset=0, returns 500 txns
	page1Txns := make([]plaid.InvestmentTransaction, 500)
	for i := range page1Txns {
		page1Txns[i] = makePlaidCashTxn("txn-p1-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-01")
	}
	page1Resp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), []plaid.AccountBase{}, []plaid.Security{},
		page1Txns, totalTxns, "req-p1",
	)

	// Page 2: offset=500, returns 500 txns
	page2Txns := make([]plaid.InvestmentTransaction, 500)
	for i := range page2Txns {
		page2Txns[i] = makePlaidCashTxn("txn-p2-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-02")
	}
	page2Resp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), []plaid.AccountBase{}, []plaid.Security{},
		page2Txns, totalTxns, "req-p2",
	)

	// Page 3: offset=1000, returns 200 txns (partial last page, 1000+200=1200 → loop ends)
	page3Txns := make([]plaid.InvestmentTransaction, 200)
	for i := range page3Txns {
		page3Txns[i] = makePlaidCashTxn("txn-p3-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-03")
	}
	page3Resp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), []plaid.AccountBase{}, []plaid.Security{},
		page3Txns, totalTxns, "req-p3",
	)

	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(page1Resp, nil).Once()
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(500), int32(500)).
		Return(page2Resp, nil).Once()
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(1000), int32(500)).
		Return(page3Resp, nil).Once()

	// Each page's UpsertBatch returns inserted count
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		return len(txns) == 500
	})).Return(500, nil).Times(2)
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		return len(txns) == 200
	})).Return(200, nil).Times(1)

	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 3, result.PagesProcessed)
	assert.Equal(t, 1200, result.TransactionsInserted)
	deps.plaidSvc.AssertExpectations(t)
	deps.invTxnRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_ExactBoundary verifies
// that total=1000 results in exactly 2 pages (offsets 0 and 500) and the loop terminates
// correctly when offset equals total.
func TestInvestmentSyncService_SyncInvestmentTransactions_Pagination_ExactBoundary(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	totalTxns := int32(1000)

	page1Txns := make([]plaid.InvestmentTransaction, 500)
	for i := range page1Txns {
		page1Txns[i] = makePlaidCashTxn("txn-p1-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-01")
	}
	page2Txns := make([]plaid.InvestmentTransaction, 500)
	for i := range page2Txns {
		page2Txns[i] = makePlaidCashTxn("txn-p2-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-02")
	}

	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(plaid.NewInvestmentsTransactionsGetResponse(makeMinimalPlaidItem(), nil, nil, page1Txns, totalTxns, "r1"), nil).Once()
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(500), int32(500)).
		Return(plaid.NewInvestmentsTransactionsGetResponse(makeMinimalPlaidItem(), nil, nil, page2Txns, totalTxns, "r2"), nil).Once()

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(500, nil).Times(2)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 2, result.PagesProcessed)
	assert.Equal(t, 1000, result.TransactionsInserted)
	deps.plaidSvc.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_DuplicatesSkipped verifies that
// transactions already in the DB (ON CONFLICT DO NOTHING) return 0 inserted and are
// counted as skipped.
func TestInvestmentSyncService_SyncInvestmentTransactions_DuplicatesSkipped(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	txns := []plaid.InvestmentTransaction{
		makePlaidCashTxn("txn-dup-001", plaidAccountID, 100.0, "2024-01-01"),
		makePlaidCashTxn("txn-dup-002", plaidAccountID, 200.0, "2024-01-02"),
	}
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil, txns, 2, "req-001",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	// All duplicates — 0 inserted, 2 skipped
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(0, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 0, result.TransactionsInserted)
	assert.Equal(t, 2, result.TransactionsSkipped)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_UpdatesLastSyncedDate verifies
// that investments_last_synced_date is updated after a successful full sync.
func TestInvestmentSyncService_SyncInvestmentTransactions_UpdatesLastSyncedDate(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	txn := makePlaidCashTxn("txn-001", plaidAccountID, 100.0, "2024-01-01")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil, []plaid.InvestmentTransaction{txn}, 1, "req-001",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil)

	// CRITICAL: UpdateInvestmentsLastSyncedDate MUST be called on success
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.MatchedBy(func(d time.Time) bool {
		today := time.Now().Truncate(24 * time.Hour)
		// Allow a small window for test timing
		return !d.Before(today.Add(-time.Minute)) && !d.After(today.Add(time.Hour))
	})).Return(nil).Times(1)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	deps.linkedItemRepo.AssertExpectations(t) // verifies UpdateInvestmentsLastSyncedDate was called
}

// TestInvestmentSyncService_SyncInvestmentTransactions_PartialPageError_NoDateUpdate
// verifies that if pagination fails mid-way (page 2 errors), the sync date is NOT updated
// so the next retry starts from the same date range.
func TestInvestmentSyncService_SyncInvestmentTransactions_PartialPageError_NoDateUpdate(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Page 1: success, 500 txns out of 600 total
	page1Txns := make([]plaid.InvestmentTransaction, 500)
	for i := range page1Txns {
		page1Txns[i] = makePlaidCashTxn("txn-p1-"+uuid.New().String(), plaidAccountID, float64(i), "2024-01-01")
	}
	page1Resp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil, page1Txns, 600, "req-p1",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(page1Resp, nil).Once()

	// Page 2: error (e.g. Plaid rate limit)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(500), int32(500)).
		Return(nil, errors.New("plaid: RATE_LIMIT_EXCEEDED")).Once()

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(500, nil).Once()

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	// Error must be returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_EXCEEDED")

	// Partial results returned
	assert.Equal(t, 500, result.TransactionsInserted)
	assert.Equal(t, 1, result.PagesProcessed)

	// CRITICAL: investments_last_synced_date must NOT be updated on partial failure
	deps.linkedItemRepo.AssertNotCalled(t, "UpdateInvestmentsLastSyncedDate")

	deps.plaidSvc.AssertExpectations(t)
	deps.invTxnRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_NullSecurityID verifies that
// cash transactions with nil security_id are stored correctly (SecurityID remains nil).
func TestInvestmentSyncService_SyncInvestmentTransactions_NullSecurityID(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Cash transaction with nil security_id
	cashTxn := makePlaidCashTxn("cash-txn-001", plaidAccountID, 150.00, "2024-01-20")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil, []plaid.InvestmentTransaction{cashTxn}, 1, "req-001",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	// Domain transaction must have nil SecurityID for cash transactions
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		if len(txns) != 1 {
			return false
		}
		// Cash transaction must have nil SecurityID
		return txns[0].SecurityID == nil
	})).Return(1, nil)

	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	deps.invTxnRepo.AssertExpectations(t) // verifies SecurityID == nil
}

// TestInvestmentSyncService_SyncInvestmentTransactions_SecurityResolution verifies that
// a transaction referencing plaid_security_id is stored with the correct internal UUID.
func TestInvestmentSyncService_SyncInvestmentTransactions_SecurityResolution(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-AAPL"
	internalSecurityID := "internal-" + uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	txn := makePlaidInvTxn("buy-txn-001", plaidAccountID, plaidSecurityID, 1500.0, "2024-01-15")

	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{sec},
		[]plaid.InvestmentTransaction{txn},
		1, "req-001",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
	}, nil)

	// CRITICAL: transaction must reference the correct internal security UUID
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		if len(txns) != 1 {
			return false
		}
		return txns[0].SecurityID != nil && *txns[0].SecurityID == internalSecurityID
	})).Return(1, nil)

	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	assert.Equal(t, 1, result.SecuritiesUpserted)
	deps.invTxnRepo.AssertExpectations(t) // verifies SecurityID mapping
}

// ============================================================================
// ProcessWebhook Tests
// ============================================================================

// TestInvestmentSyncService_ProcessWebhook_RoutesHoldings verifies that HOLDINGS
// webhooks are routed to SyncHoldings.
func TestInvestmentSyncService_ProcessWebhook_RoutesHoldings(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	itemID := "item-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem(itemID, userID)

	webhookCode := "DEFAULT_UPDATE"
	webhook := &domain.WebhookEvent{
		ID:          "wh-001",
		WebhookType: "HOLDINGS",
		WebhookCode: &webhookCode,
		ItemID:      &itemID,
	}

	// ProcessWebhook marks start, gets item, calls sync, marks complete
	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil).Times(2) // started + completed
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)

	// SyncHoldings dependencies

	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(
		plaid.NewInvestmentsHoldingsGetResponse(
			nil, nil, nil, makeMinimalPlaidItem(), "req-001",
		), nil,
	)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	err := svc.ProcessWebhook(ctx, webhook)

	require.NoError(t, err)
	assert.True(t, webhook.Processed)
	deps.webhookRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_ProcessWebhook_RoutesInvestmentTransactions verifies that
// INVESTMENTS_TRANSACTIONS webhooks are routed to SyncInvestmentTransactions.
func TestInvestmentSyncService_ProcessWebhook_RoutesInvestmentTransactions(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	itemID := "item-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem(itemID, userID)

	webhookCode := "DEFAULT_UPDATE"
	webhook := &domain.WebhookEvent{
		ID:          "wh-002",
		WebhookType: "INVESTMENTS_TRANSACTIONS",
		WebhookCode: &webhookCode,
		ItemID:      &itemID,
	}

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil).Times(2)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)

	// SyncInvestmentTransactions dependencies (empty response)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(plaid.NewInvestmentsTransactionsGetResponse(
			makeMinimalPlaidItem(), nil, nil, []plaid.InvestmentTransaction{}, 0, "req-001",
		), nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	err := svc.ProcessWebhook(ctx, webhook)

	require.NoError(t, err)
	assert.True(t, webhook.Processed)
}

// TestInvestmentSyncService_ProcessWebhook_UnsupportedType verifies that unknown webhook
// types return an error without calling any sync functions.
func TestInvestmentSyncService_ProcessWebhook_UnsupportedType(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	itemID := "item-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem(itemID, userID)

	webhook := &domain.WebhookEvent{
		ID:          "wh-unknown",
		WebhookType: "TRANSACTIONS", // not an investment webhook type
		ItemID:      &itemID,
	}

	// ProcessWebhook marks start then fails on unsupported type
	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)

	err := svc.ProcessWebhook(ctx, webhook)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported investment webhook type")
	// Webhook should NOT be marked as processed
	assert.False(t, webhook.Processed)
}

// ============================================================================
// Issue #659: C3 Fix - Holdings data loss when security unmapped
// ============================================================================

// TestInvestmentSyncService_SyncHoldings_UnmappedSecurity_SkipsAccountReplace verifies
// that when ANY security in an account's holdings fails to resolve, the entire account
// replace is skipped (preserving existing holdings) rather than deleting them.
// RED: currently fails because the implementation still calls ReplaceForAccount even
// when some holdings were skipped due to missing security mapping.
func TestInvestmentSyncService_SyncHoldings_UnmappedSecurity_SkipsAccountReplace(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-001"
	unknownSecurityID := "plaid-sec-UNKNOWN"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	// Two holdings: one with a known security, one with an unknown security
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{
			makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0),
			makePlaidHolding(plaidAccountID, unknownSecurityID, 5.0, 50.0, 250.0, 200.0),
		},
		[]plaid.Security{
			makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL"),
			makePlaidSecurity(unknownSecurityID, "Unknown Corp", "UNK"),
		},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)

	// Only one security in map — the unknown security ID is missing
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
		// unknownSecurityID is intentionally absent from the map
	}, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	// Should succeed (not a hard error) but the account replace is skipped
	require.NoError(t, err)
	assert.NotNil(t, result)
	// ReplaceForAccount must NOT be called — existing holdings are preserved
	assertNoCallTo(t, &deps.holdingRepo.Mock, "ReplaceForAccount")
	// Error is recorded in result
	assert.Len(t, result.Errors, 1)
}

// TestInvestmentSyncService_ProcessWebhook_HoldingsPartialFailure verifies that when
// SyncHoldings has unmapped securities (result.Errors non-empty), ProcessWebhook returns
// an error so the job worker triggers a retry rather than marking the webhook as processed.
func TestInvestmentSyncService_ProcessWebhook_HoldingsPartialFailure(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	itemID := "item-partial-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem(itemID, userID)

	plaidAccountID := "plaid-acct-partial"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-known"
	unknownSecurityID := "plaid-sec-unknown"
	internalSecurityID := uuid.New().String()
	holdingsCode := "DEFAULT_UPDATE"

	webhook := &domain.WebhookEvent{
		ID:          "wh-partial",
		WebhookType: "HOLDINGS",
		WebhookCode: &holdingsCode,
		ItemID:      &itemID,
	}

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{
			makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 100.0, 1000.0, 900.0),
			makePlaidHolding(plaidAccountID, unknownSecurityID, 5.0, 50.0, 250.0, 200.0),
		},
		[]plaid.Security{
			makePlaidSecurity(plaidSecurityID, "Known Corp", "KNW"),
			makePlaidSecurity(unknownSecurityID, "Unknown Corp", "UNK"),
		},
		makeMinimalPlaidItem(),
		"req-partial",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
		// unknownSecurityID absent — will trigger skip
	}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	err := svc.ProcessWebhook(ctx, webhook)

	// ProcessWebhook must return an error so the job retries
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partial failure")
	// Webhook must NOT be marked as processed
	assert.False(t, webhook.Processed)
	assertNoCallTo(t, &deps.holdingRepo.Mock, "ReplaceForAccount")
}

// TestInvestmentSyncService_ProcessWebhook_HoldingsSkippedIncomplete verifies that when
// SyncHoldings skips an account's clear because it's absent from Plaid's response (Issue #1569
// Task 4 — result.SkippedIncomplete, NOT result.Errors), ProcessWebhook still returns an error so
// the job retries. This is a DIFFERENT code path from the unmapped-security test above — that one
// hits result.Errors, this one hits the newer result.SkippedIncomplete branch added in fix round 1.
func TestInvestmentSyncService_ProcessWebhook_HoldingsSkippedIncomplete(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	itemID := "item-skipped-001"
	userID := uuid.New().String()
	linkedItem := makeLinkedItem(itemID, userID)

	internalAccountID := uuid.New().String()
	plaidAccountID := "plaid-acct-absent"
	holdingsCode := "DEFAULT_UPDATE"

	webhook := &domain.WebhookEvent{
		ID:          "wh-skipped",
		WebhookType: "HOLDINGS",
		WebhookCode: &holdingsCode,
		ItemID:      &itemID,
	}

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)
	// The account is absent from `accounts` entirely and has no holdings in the response.
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{},
		[]plaid.Security{},
		makeMinimalPlaidItem(),
		"req-skipped",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	err := svc.ProcessWebhook(ctx, webhook)

	// ProcessWebhook must return an error so the job retries — the SkippedIncomplete branch,
	// not the Errors branch.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete")
	assert.False(t, webhook.Processed)
	assertNoCallTo(t, &deps.holdingRepo.Mock, "ReplaceForAccount")
}

// TestInvestmentSyncService_ProcessWebhook_NilWebhook verifies that nil input returns an error.
func TestInvestmentSyncService_ProcessWebhook_NilWebhook(t *testing.T) {
	svc, _ := setupInvestmentSyncService(t)
	ctx := context.Background()

	err := svc.ProcessWebhook(ctx, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestInvestmentSyncService_SyncInvestmentTransactions_CashTxnQuantityPriceAlwaysSet
// proves that Quantity and Price are always set (non-nil) even when Plaid reports 0.
// This is the contract lock for R2-3: a code-simplifier revert that conditionally
// set qty/price only when != 0 broke cash/dividend/fee rows (Go nil → JSON omit).
func TestInvestmentSyncService_SyncInvestmentTransactions_CashTxnQuantityPriceAlwaysSet(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-r23"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-r23", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Cash dividend: Quantity=0, Price=0 (Plaid standard for cash transactions)
	cashTxn := makePlaidCashTxn("cash-r23-001", plaidAccountID, 75.00, "2024-02-10")

	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil, []plaid.InvestmentTransaction{cashTxn}, 1, "req-r23",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	// KEY ASSERTION: Quantity and Price must be non-nil (pointer to 0) — not nil — for cash txns.
	// If R2-3 regresses, qty/price are only set when != 0, leaving them nil here.
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		if len(txns) != 1 {
			return false
		}
		txn := txns[0]
		// Quantity must be non-nil pointer to 0 (not nil)
		if txn.Quantity == nil {
			return false
		}
		if *txn.Quantity != 0.0 {
			return false
		}
		// Price must be non-nil pointer to 0 (not nil)
		if txn.Price == nil {
			return false
		}
		if *txn.Price != 0.0 {
			return false
		}
		return true
	})).Return(1, nil)

	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	deps.invTxnRepo.AssertExpectations(t) // verifies Quantity and Price are non-nil
}

// TestInvestmentSyncService_ProcessWebhook_MissingItemID verifies that a webhook
// without an item_id is rejected and the webhook is updated with an error.
func TestInvestmentSyncService_ProcessWebhook_MissingItemID(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	webhookCode := "DEFAULT_UPDATE"
	webhook := &domain.WebhookEvent{
		ID:          "wh-003",
		WebhookType: "HOLDINGS",
		WebhookCode: &webhookCode,
		ItemID:      nil, // missing!
	}

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)

	err := svc.ProcessWebhook(ctx, webhook)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "item_id")
	assert.NotNil(t, webhook.ProcessingError)
}

// mockAnalyticsForInvestmentSync is a minimal phclient.Analytics stub for setter tests.
type mockAnalyticsForInvestmentSync struct{}

func (m *mockAnalyticsForInvestmentSync) Capture(distinctID, event string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForInvestmentSync) Identify(distinctID string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForInvestmentSync) Close() {}

var _ phclient.Analytics = (*mockAnalyticsForInvestmentSync)(nil)

func TestInvestmentSyncService_SetAnalytics_DoesNotPanic(t *testing.T) {
	svc, _ := setupInvestmentSyncService(t)
	iss := svc.(*investmentSyncService)
	require.NotPanics(t, func() {
		svc.SetAnalytics(&mockAnalyticsForInvestmentSync{})
	}, "SetAnalytics with a valid analytics must not panic")
	assert.NotNil(t, iss.analytics, "analytics field should be set after SetAnalytics(non-nil)")
	require.NotPanics(t, func() {
		svc.SetAnalytics(nil)
	}, "SetAnalytics with nil must not panic")
	assert.Nil(t, iss.analytics, "analytics field should be nil after SetAnalytics(nil)")
}

// TestInvestmentSyncService_SyncHoldings_FilesTheAggregateUnderTheCapturedDay is #1569 T9.1 item C.
//
// The row-level holding_snapshots capture takes its as_of_date from POSTGRES (serverUTCDate, inside
// ReplaceForAccount's own transaction). This path used to take the portfolio_snapshots aggregate's
// day from the GO process's clock instead. The two clocks are independent, so a sync that crosses
// UTC midnight while Postgres has not — or the reverse — files the rows under day D and the
// aggregate under D+1: an aggregate with nothing behind it, and a day whose rows no aggregate
// names. portfolio_snapshot_repair.go excludes the current server day, so the orphan survives the
// next repair pass and is later refused for lack of coverage.
//
// The daily worker already solved this by using the day CaptureForUser returns; this path was the
// outlier. The fix is NOT to compare clocks — it is to stop consulting the second one.
func TestInvestmentSyncService_SyncHoldings_FilesTheAggregateUnderTheCapturedDay(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-clock"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-clock"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-clock", userID)

	// A day the Go clock cannot possibly produce, so "it used time.Now()" is unmistakable.
	capturedDay := time.Date(2031, 3, 7, 0, 0, 0, 0, time.UTC)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(
		plaid.NewInvestmentsHoldingsGetResponse(
			[]plaid.AccountBase{}, []plaid.Holding{holding}, []plaid.Security{sec},
			makeMinimalPlaidItem(), "req-clock"), nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).
		Return(map[string]string{plaidSecurityID: internalSecurityID}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).
		Return(capturedDay, nil)

	_, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	deps.snapshotRepo.AssertCalled(t, "Upsert", ctx, userID, capturedDay)
}

// TestInvestmentSyncService_SyncHoldings_StraddlingUTCMidnight_FilesUnderTheLatestCapturedDay pins
// WHICH captured day wins when one item's accounts do not all land on the same one.
//
// Each account's capture reads the server date inside its own transaction, so a sync that crosses
// UTC midnight mid-loop genuinely produces two different days. Neither day then has full coverage —
// which the repair reports honestly rather than silently rebuilding — and the aggregate names the
// LATER one: the more recent state, and the day the next sync and the daily whole-user capture will
// complete. Reversing this to the earliest day left the whole services package green.
func TestInvestmentSyncService_SyncHoldings_StraddlingUTCMidnight_FilesUnderTheLatestCapturedDay(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-straddle", userID)
	plaidSecurityID := "plaid-sec-straddle"
	internalSecurityID := uuid.New().String()
	acctA, acctB := "plaid-acct-A", "plaid-acct-B"
	internalA, internalB := uuid.New().String(), uuid.New().String()

	dayBefore := time.Date(2031, 3, 7, 0, 0, 0, 0, time.UTC)
	dayAfter := time.Date(2031, 3, 8, 0, 0, 0, 0, time.UTC)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(
		plaid.NewInvestmentsHoldingsGetResponse(
			[]plaid.AccountBase{},
			[]plaid.Holding{
				makePlaidHolding(acctA, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0),
				makePlaidHolding(acctB, plaidSecurityID, 5.0, 165.0, 825.0, 750.0),
			},
			[]plaid.Security{sec}, makeMinimalPlaidItem(), "req-straddle"), nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).
		Return(map[string]string{plaidSecurityID: internalSecurityID}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalA, PlaidAccountID: acctA, UserID: userID},
		{ID: internalB, PlaidAccountID: acctB, UserID: userID},
	}, nil)
	// The map iteration order over accountMap is not defined, so the days are attached per
	// account rather than per call order: whichever runs first, the LATER day must win.
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalA, userID, mock.Anything).Return(dayBefore, nil)
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalB, userID, mock.Anything).Return(dayAfter, nil)

	_, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	deps.snapshotRepo.AssertCalled(t, "Upsert", ctx, userID, dayAfter)
	deps.snapshotRepo.AssertNotCalled(t, "Upsert", ctx, userID, dayBefore)
}
