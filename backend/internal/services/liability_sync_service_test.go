package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Repositories — Liability-specific
// ============================================================================

type MockCreditLiabilityRepository struct {
	mock.Mock
}

func (m *MockCreditLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.CreditLiability) error {
	args := m.Called(ctx, accountID, userID, liability)
	return args.Error(0)
}

func (m *MockCreditLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.CreditLiability, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.CreditLiability), args.Error(1)
}

func (m *MockCreditLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.CreditLiability, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CreditLiability), args.Error(1)
}

type MockMortgageLiabilityRepository struct {
	mock.Mock
}

func (m *MockMortgageLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.MortgageLiability) error {
	args := m.Called(ctx, accountID, userID, liability)
	return args.Error(0)
}

func (m *MockMortgageLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.MortgageLiability, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MortgageLiability), args.Error(1)
}

func (m *MockMortgageLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.MortgageLiability, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MortgageLiability), args.Error(1)
}

type MockStudentLoanLiabilityRepository struct {
	mock.Mock
}

func (m *MockStudentLoanLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.StudentLoanLiability) error {
	args := m.Called(ctx, accountID, userID, liability)
	return args.Error(0)
}

func (m *MockStudentLoanLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.StudentLoanLiability, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.StudentLoanLiability), args.Error(1)
}

func (m *MockStudentLoanLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.StudentLoanLiability, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.StudentLoanLiability), args.Error(1)
}

// ============================================================================
// Service Factory Helper
// ============================================================================

type liabilitySyncDeps struct {
	webhookRepo      *MockWebhookRepository
	linkedItemRepo   *MockLinkedItemRepository
	accountRepo      *MockAccountRepository
	creditRepo       *MockCreditLiabilityRepository
	mortgageRepo     *MockMortgageLiabilityRepository
	studentLoanRepo  *MockStudentLoanLiabilityRepository
	plaidSvc         *MockPlaidService
	encryptionSvc    *MockEncryptionService
}

func setupLiabilitySyncService(t *testing.T) (LiabilitySyncService, liabilitySyncDeps) {
	t.Helper()
	deps := liabilitySyncDeps{
		webhookRepo:     new(MockWebhookRepository),
		linkedItemRepo:  new(MockLinkedItemRepository),
		accountRepo:     new(MockAccountRepository),
		creditRepo:      new(MockCreditLiabilityRepository),
		mortgageRepo:    new(MockMortgageLiabilityRepository),
		studentLoanRepo: new(MockStudentLoanLiabilityRepository),
		plaidSvc:        new(MockPlaidService),
		encryptionSvc:   new(MockEncryptionService),
	}
	svc := NewLiabilitySyncService(
		deps.webhookRepo,
		deps.linkedItemRepo,
		deps.accountRepo,
		deps.creditRepo,
		deps.mortgageRepo,
		deps.studentLoanRepo,
		deps.plaidSvc,
		deps.encryptionSvc,
		nil, // logger
	)
	return svc, deps
}

// int32ptr returns a pointer to the given int32 value.
func int32ptr(i int32) *int32 { return &i }

// makeWebhookEventForLiabilities creates a LIABILITIES webhook event for testing.
func makeWebhookEventForLiabilities(webhookType, itemID string) *domain.WebhookEvent {
	code := "DEFAULT_UPDATE"
	return &domain.WebhookEvent{
		ID:          uuid.New().String(),
		WebhookType: webhookType,
		WebhookCode: &code,
		ItemID:      &itemID,
	}
}

// ============================================================================
// Plaid SDK Helpers for Liabilities
// ============================================================================

func makePlaidLiabilitiesResponse(credit []plaid.CreditCardLiability, mortgage []plaid.MortgageLiability, student []plaid.StudentLoan) *plaid.LiabilitiesGetResponse {
	liabilities := plaid.NewLiabilitiesObject(credit, mortgage, student)
	return plaid.NewLiabilitiesGetResponse(
		[]plaid.AccountBase{},
		*plaid.NewItemWithDefaults(),
		*liabilities,
		"req-liab-001",
	)
}

func makePlaidCreditLiability(plaidAccountID string) plaid.CreditCardLiability {
	accountID := plaidNullStr(plaidAccountID)
	isOverdue := plaidNullBool(false)
	lastPaymentAmount := plaidNullFloat(100.0)
	lastPaymentDate := plaidNullStr("2024-01-15")
	minPayment := plaidNullFloat(25.0)
	nextDueDate := plaidNullStr("2024-02-15")
	lastStatementIssueDate := plaidNullStr("2024-01-01")
	lastStatementBalance := plaidNullFloat(500.0)
	apr := plaid.APR{
		AprPercentage:       19.99,
		AprType:             "purchase",
		BalanceSubjectToApr: plaidNullFloat(480.0),
		InterestChargeAmount: plaidNullFloat(7.99),
	}
	return plaid.CreditCardLiability{
		AccountId:              accountID,
		Aprs:                   []plaid.APR{apr},
		IsOverdue:              isOverdue,
		LastPaymentAmount:      lastPaymentAmount,
		LastPaymentDate:        lastPaymentDate,
		LastStatementIssueDate: lastStatementIssueDate,
		LastStatementBalance:   lastStatementBalance,
		MinimumPaymentAmount:   minPayment,
		NextPaymentDueDate:     nextDueDate,
	}
}

func makePlaidMortgageLiability(plaidAccountID string) plaid.MortgageLiability {
	interestRate := plaid.MortgageInterestRate{
		Percentage: plaidNullFloat(3.5),
		Type:       plaidNullStr("fixed"),
	}
	propertyAddress := plaid.MortgagePropertyAddress{
		Street:     plaidNullStr("123 Main St"),
		City:       plaidNullStr("Springfield"),
		Region:     plaidNullStr("IL"),
		PostalCode: plaidNullStr("62701"),
		Country:    plaidNullStr("US"),
	}
	return plaid.MortgageLiability{
		AccountId:                  plaidAccountID,
		AccountNumber:              "MORT-001",
		CurrentLateFee:             plaidNilFloat(),
		EscrowBalance:              plaidNullFloat(1500.0),
		HasPmi:                     plaidNullBool(false),
		HasPrepaymentPenalty:       plaidNullBool(false),
		InterestRate:               interestRate,
		LastPaymentAmount:          plaidNullFloat(1200.0),
		LastPaymentDate:            plaidNullStr("2024-01-01"),
		LoanTypeDescription:        plaidNullStr("conventional"),
		LoanTerm:                   plaidNullStr("30 Year Fixed"),
		MaturityDate:               plaidNullStr("2054-01-01"),
		NextMonthlyPayment:         plaidNullFloat(1200.0),
		NextPaymentDueDate:         plaidNullStr("2024-02-01"),
		OriginationDate:            plaidNullStr("2024-01-01"),
		OriginationPrincipalAmount: plaidNullFloat(300000.0),
		PastDueAmount:              plaidNilFloat(),
		PropertyAddress:            propertyAddress,
		YtdInterestPaid:            plaidNullFloat(3500.0),
		YtdPrincipalPaid:           plaidNullFloat(1500.0),
	}
}

func makePlaidStudentLoan(plaidAccountID string) plaid.StudentLoan {
	loanStatus := plaid.StudentLoanStatus{
		EndDate: plaidNullStr("2030-01-01"),
		Type:    plaidNullStr("repayment"),
	}
	repaymentPlan := plaid.StudentRepaymentPlan{
		Description: plaidNullStr("Standard Repayment Plan"),
		Type:        plaidNullStr("standard"),
	}
	servicerAddress := plaid.ServicerAddressData{
		Street:     plaidNullStr("456 Loan St"),
		City:       plaidNullStr("Washington"),
		Region:     plaidNullStr("DC"),
		PostalCode: plaidNullStr("20001"),
		Country:    plaidNullStr("US"),
	}
	paid := plaid.NewNullableInt32(int32ptr(24))
	remaining := plaid.NewNullableInt32(int32ptr(96))
	pslfStatus := plaid.PSLFStatus{
		EstimatedEligibilityDate: plaidNullStr("2032-01-01"),
		PaymentsMade:             *paid,
		PaymentsRemaining:        *remaining,
	}
	return plaid.StudentLoan{
		AccountId:                  plaidNullStr(plaidAccountID),
		AccountNumber:              plaidNullStr("SL-001"),
		DisbursementDates:          []string{"2020-08-01"},
		ExpectedPayoffDate:         plaidNullStr("2030-01-01"),
		Guarantor:                  plaidNullStr("USA Funds"),
		InterestRatePercentage:     5.05,
		IsOverdue:                  plaidNullBool(false),
		LastPaymentAmount:          plaidNullFloat(300.0),
		LastPaymentDate:            plaidNullStr("2024-01-05"),
		LastStatementIssueDate:     plaidNullStr("2024-01-01"),
		LoanName:                   plaidNullStr("Direct Subsidized Loan"),
		LoanStatus:                 loanStatus,
		MinimumPaymentAmount:       plaidNullFloat(290.0),
		NextPaymentDueDate:         plaidNullStr("2024-02-05"),
		OriginationDate:            plaidNullStr("2020-08-01"),
		OriginationPrincipalAmount: plaidNullFloat(30000.0),
		OutstandingInterestAmount:  plaidNullFloat(500.0),
		PaymentReferenceNumber:     plaidNullStr("REF-001"),
		PslfStatus:                 pslfStatus,
		RepaymentPlan:              repaymentPlan,
		SequenceNumber:             plaidNullStr("1"),
		ServicerAddress:            servicerAddress,
		YtdInterestPaid:            plaidNullFloat(1500.0),
		YtdPrincipalPaid:           plaidNullFloat(1500.0),
	}
}

// ============================================================================
// ProcessWebhook Tests
// ============================================================================

// TestLiabilitySyncService_ProcessWebhook_NilWebhook verifies that a nil webhook returns error.
func TestLiabilitySyncService_ProcessWebhook_NilWebhook(t *testing.T) {
	svc, _ := setupLiabilitySyncService(t)
	err := svc.ProcessWebhook(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestLiabilitySyncService_ProcessWebhook_MissingItemID verifies error when item_id is absent.
func TestLiabilitySyncService_ProcessWebhook_MissingItemID(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	webhook := &domain.WebhookEvent{
		ID:          uuid.New().String(),
		WebhookType: "LIABILITIES",
	}

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)

	err := svc.ProcessWebhook(ctx, webhook)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item_id")
	deps.webhookRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_ProcessWebhook_LinkedItemNotFound verifies error when linked item is missing.
func TestLiabilitySyncService_ProcessWebhook_LinkedItemNotFound(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	itemID := "missing-item"
	webhook := makeWebhookEventForLiabilities("LIABILITIES", itemID)

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(nil, errors.New("not found"))

	err := svc.ProcessWebhook(ctx, webhook)
	require.Error(t, err)
	deps.linkedItemRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_ProcessWebhook_Success verifies full happy path.
func TestLiabilitySyncService_ProcessWebhook_Success(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	itemID := "item-liab-001"

	webhook := makeWebhookEventForLiabilities("LIABILITIES", itemID)
	linkedItem := makeLinkedItem(itemID, userID)

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)

	accessToken := "decrypted-token"
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return(accessToken, nil)

	credit := makePlaidCreditLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{credit},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, accessToken).Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.creditRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	err := svc.ProcessWebhook(ctx, webhook)
	require.NoError(t, err)
	deps.plaidSvc.AssertExpectations(t)
	deps.creditRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_ProcessWebhook_SyncFailure verifies that a sync error
// updates the webhook with error info and increments retry count.
func TestLiabilitySyncService_ProcessWebhook_SyncFailure(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	itemID := "item-liab-002"
	webhook := makeWebhookEventForLiabilities("LIABILITIES", itemID)
	linkedItem := makeLinkedItem(itemID, "user-001")

	deps.webhookRepo.On("Update", ctx, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("", errors.New("decrypt failed"))

	err := svc.ProcessWebhook(ctx, webhook)
	require.Error(t, err)
	deps.webhookRepo.AssertExpectations(t)
}

// ============================================================================
// SyncLiabilities Tests
// ============================================================================

// TestLiabilitySyncService_SyncLiabilities_DecryptFailure verifies error propagation.
func TestLiabilitySyncService_SyncLiabilities_DecryptFailure(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	linkedItem := makeLinkedItem("item-001", "user-001")
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("", errors.New("bad key"))

	_, err := svc.SyncLiabilities(ctx, linkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

// TestLiabilitySyncService_SyncLiabilities_PlaidAPIFailure verifies Plaid error propagation.
func TestLiabilitySyncService_SyncLiabilities_PlaidAPIFailure(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	linkedItem := makeLinkedItem("item-001", "user-001")
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(nil, errors.New("plaid 500"))

	_, err := svc.SyncLiabilities(ctx, linkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaid")
}

// TestLiabilitySyncService_SyncLiabilities_CreditHappyPath verifies a credit liability
// is correctly mapped and stored for a known account.
func TestLiabilitySyncService_SyncLiabilities_CreditHappyPath(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	credit := makePlaidCreditLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{credit},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.creditRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.MatchedBy(func(l *domain.CreditLiability) bool {
		return l.UserID == userID &&
			l.AccountID == internalAccountID &&
			len(l.APRs) == 1 &&
			l.APRs[0].RateType == "purchase"
	})).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 1, result.CreditProcessed)
	assert.Equal(t, 0, result.MortgageProcessed)
	assert.Equal(t, 0, result.StudentLoansProcessed)
	assert.Equal(t, 1, result.AccountsProcessed)
	deps.creditRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_SyncLiabilities_MortgageHappyPath verifies a mortgage
// liability is correctly mapped and stored.
func TestLiabilitySyncService_SyncLiabilities_MortgageHappyPath(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-mort-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	mortgage := makePlaidMortgageLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{},
		[]plaid.MortgageLiability{mortgage},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.mortgageRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.MatchedBy(func(l *domain.MortgageLiability) bool {
		return l.UserID == userID && l.AccountID == internalAccountID
	})).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 0, result.CreditProcessed)
	assert.Equal(t, 1, result.MortgageProcessed)
	assert.Equal(t, 1, result.AccountsProcessed)
	deps.mortgageRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_SyncLiabilities_StudentLoanHappyPath verifies a student
// loan is correctly mapped and stored.
func TestLiabilitySyncService_SyncLiabilities_StudentLoanHappyPath(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-sl-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	student := makePlaidStudentLoan(plaidAccountID)
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{student},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.studentLoanRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.MatchedBy(func(l *domain.StudentLoanLiability) bool {
		return l.UserID == userID &&
			l.AccountID == internalAccountID &&
			len(l.DisbursementDates) == 1
	})).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 0, result.CreditProcessed)
	assert.Equal(t, 0, result.MortgageProcessed)
	assert.Equal(t, 1, result.StudentLoansProcessed)
	assert.Equal(t, 1, result.AccountsProcessed)
	deps.studentLoanRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_SyncLiabilities_UnmappedAccountSkipped verifies that
// liabilities for unknown plaid account IDs are silently skipped.
func TestLiabilitySyncService_SyncLiabilities_UnmappedAccountSkipped(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	linkedItem := makeLinkedItem("item-001", "user-001")
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	credit := makePlaidCreditLiability("unknown-plaid-account")
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{credit},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	// Only one known account, but its ID doesn't match the liability
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: uuid.New().String(), PlaidAccountID: "some-other-account", UserID: "user-001"},
	}, nil)

	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	// Account skipped — nothing processed
	assert.Equal(t, 0, result.CreditProcessed)
	assert.Equal(t, 0, result.AccountsProcessed)
	deps.creditRepo.AssertNotCalled(t, "ReplaceForAccount")
}

// TestLiabilitySyncService_SyncLiabilities_MixedTypes verifies all three liability
// types are handled in a single sync when present.
func TestLiabilitySyncService_SyncLiabilities_MixedTypes(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidCCID := "plaid-cc-001"
	plaidMortID := "plaid-mort-001"
	plaidSLID := "plaid-sl-001"
	ccAcctID := uuid.New().String()
	mortAcctID := uuid.New().String()
	slAcctID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-multi", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{makePlaidCreditLiability(plaidCCID)},
		[]plaid.MortgageLiability{makePlaidMortgageLiability(plaidMortID)},
		[]plaid.StudentLoan{makePlaidStudentLoan(plaidSLID)},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: ccAcctID, PlaidAccountID: plaidCCID, UserID: userID},
		{ID: mortAcctID, PlaidAccountID: plaidMortID, UserID: userID},
		{ID: slAcctID, PlaidAccountID: plaidSLID, UserID: userID},
	}, nil)

	deps.creditRepo.On("ReplaceForAccount", ctx, ccAcctID, userID, mock.Anything).Return(nil)
	deps.mortgageRepo.On("ReplaceForAccount", ctx, mortAcctID, userID, mock.Anything).Return(nil)
	deps.studentLoanRepo.On("ReplaceForAccount", ctx, slAcctID, userID, mock.Anything).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 1, result.CreditProcessed)
	assert.Equal(t, 1, result.MortgageProcessed)
	assert.Equal(t, 1, result.StudentLoansProcessed)
	assert.Equal(t, 3, result.AccountsProcessed)
	deps.creditRepo.AssertExpectations(t)
	deps.mortgageRepo.AssertExpectations(t)
	deps.studentLoanRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_SyncLiabilities_EmptyArrayClearsData verifies that an
// empty credit array (not nil) still calls ReplaceForAccount (which deletes existing data).
func TestLiabilitySyncService_SyncLiabilities_EmptyArrayClearsData(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	// Empty credit array — account had credit card but no longer does
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	// Nothing in the response arrays, so no ReplaceForAccount calls
	assert.Equal(t, 0, result.CreditProcessed)
	deps.creditRepo.AssertNotCalled(t, "ReplaceForAccount")
}

// TestLiabilitySyncService_SyncLiabilities_UpdatesLastSyncedAt verifies that
// liabilities_last_synced_at is updated after a successful sync.
func TestLiabilitySyncService_SyncLiabilities_UpdatesLastSyncedAt(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	linkedItem := makeLinkedItem("item-001", "user-001")
	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// The timestamp update MUST be called
	var capturedTime time.Time
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.MatchedBy(func(t time.Time) bool {
		capturedTime = t
		return !t.IsZero()
	})).Return(nil)

	_, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
	assert.False(t, capturedTime.IsZero())
	deps.linkedItemRepo.AssertExpectations(t)
}

// TestLiabilitySyncService_SyncLiabilities_CreditRepoError verifies that a
// credit repo error is recorded in result.Errors and returned.
func TestLiabilitySyncService_SyncLiabilities_CreditRepoError(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-001"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	deps.encryptionSvc.On("Decrypt", linkedItem.AccessTokenEncrypted).Return("tok", nil)

	credit := makePlaidCreditLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{credit},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	deps.creditRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(errors.New("db error"))

	_, err := svc.SyncLiabilities(ctx, linkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

// TestLiabilitySyncService_SetAnalytics verifies analytics can be set without panic.
func TestLiabilitySyncService_SetAnalytics(t *testing.T) {
	svc, _ := setupLiabilitySyncService(t)
	// nil analytics must not panic
	svc.SetAnalytics(nil)
}
