package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

// LiabilitySyncService syncs liability data from Plaid's /liabilities/get endpoint.
// Liabilities use a full-replace strategy (Plaid returns a complete snapshot, no delta).
//
// Issue #910: Plaid Liabilities — Credit, Mortgage, Student Loans
type LiabilitySyncService interface {
	// ProcessWebhook routes LIABILITIES webhooks to SyncLiabilities.
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error

	// SyncLiabilities fetches the complete liability snapshot from Plaid and atomically
	// replaces all liabilities for accounts belonging to the linked item.
	SyncLiabilities(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.LiabilitySyncResult, error)

	// SetAnalytics sets the PostHog analytics client (setter injection).
	SetAnalytics(a phclient.Analytics)
}

type liabilitySyncService struct {
	logger          *slog.Logger
	analytics       phclient.Analytics
	webhookRepo     repository.WebhookRepository
	linkedItemRepo  repository.LinkedItemRepository
	accountRepo     repository.AccountRepository
	creditRepo      repository.CreditLiabilityRepository
	mortgageRepo    repository.MortgageLiabilityRepository
	studentLoanRepo repository.StudentLoanLiabilityRepository
	plaidService    PlaidService
	encryptionSvc   EncryptionService
}

// SetAnalytics sets the PostHog analytics client.
func (s *liabilitySyncService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// NewLiabilitySyncService creates a new LiabilitySyncService.
func NewLiabilitySyncService(
	webhookRepo repository.WebhookRepository,
	linkedItemRepo repository.LinkedItemRepository,
	accountRepo repository.AccountRepository,
	creditRepo repository.CreditLiabilityRepository,
	mortgageRepo repository.MortgageLiabilityRepository,
	studentLoanRepo repository.StudentLoanLiabilityRepository,
	plaidService PlaidService,
	encryptionSvc EncryptionService,
	logger *slog.Logger,
) LiabilitySyncService {
	if logger == nil {
		logger = slog.Default()
	}
	return &liabilitySyncService{
		logger:          logger,
		webhookRepo:     webhookRepo,
		linkedItemRepo:  linkedItemRepo,
		accountRepo:     accountRepo,
		creditRepo:      creditRepo,
		mortgageRepo:    mortgageRepo,
		studentLoanRepo: studentLoanRepo,
		plaidService:    plaidService,
		encryptionSvc:   encryptionSvc,
	}
}

// ProcessWebhook routes LIABILITIES webhooks to SyncLiabilities, following
// the same pattern as InvestmentSyncService.ProcessWebhook.
func (s *liabilitySyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	if webhook == nil {
		return errors.New("webhook event is nil")
	}

	if webhook.ItemID == nil || *webhook.ItemID == "" {
		errorMsg := "webhook missing item_id — cannot process liabilities webhook"
		webhook.ProcessingError = &errorMsg
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		return errors.New(errorMsg)
	}

	now := time.Now()
	webhook.ProcessingStartedAt = &now
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processing: %w", err)
	}

	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, *webhook.ItemID)
	if err != nil {
		errorMsg := fmt.Sprintf("linked item not found for item_id %s: %v", *webhook.ItemID, err)
		webhook.ProcessingError = &errorMsg
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		return fmt.Errorf("failed to get linked item: %w", err)
	}

	_, err = s.SyncLiabilities(ctx, linkedItem)

	if err != nil {
		plaidErr := domain.ClassifyPlaidError(err)
		errorMsg := plaidErr.Error()
		webhook.ProcessingError = &errorMsg
		if plaidErr.ErrorType != domain.PlaidErrorPermanent {
			webhook.RetryCount++
		}
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		if s.analytics != nil {
			s.analytics.Capture(linkedItem.UserID, phclient.EventLiabilitySyncFailed, map[string]interface{}{
				"error_type":   fmt.Sprintf("%d", plaidErr.ErrorType),
				"webhook_type": webhook.WebhookType,
			})
		}
		return fmt.Errorf("liability sync failed: %w", err)
	}

	completedAt := time.Now()
	webhook.Processed = true
	webhook.ProcessingCompletedAt = &completedAt
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processed: %w", err)
	}

	return nil
}

// SyncLiabilities fetches the complete liability snapshot from Plaid and replaces
// each account's liabilities atomically. Since Plaid returns a full snapshot (no delta),
// each category is fully replaced per account when present in the response.
func (s *liabilitySyncService) SyncLiabilities(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.LiabilitySyncResult, error) {
	accessToken, err := s.encryptionSvc.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	resp, err := s.plaidService.LiabilitiesGet(ctx, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch liabilities from plaid: %w", err)
	}

	// Build plaid_account_id → internal account ID map
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts for linked item: %w", err)
	}
	accountMap := make(map[string]string, len(accounts))
	for _, a := range accounts {
		accountMap[a.PlaidAccountID] = a.ID
	}

	result := &domain.LiabilitySyncResult{}
	liabilities := resp.Liabilities

	// Process credit liabilities
	for _, pc := range liabilities.Credit {
		plaidAccountID := pc.AccountId.Get()
		if plaidAccountID == nil || *plaidAccountID == "" {
			continue
		}
		internalAccountID, ok := accountMap[*plaidAccountID]
		if !ok {
			s.logger.Warn("no internal account for credit liability, skipping",
				"plaid_account_id", *plaidAccountID,
			)
			continue
		}

		credit := mapPlaidCreditLiability(pc, linkedItem.UserID, internalAccountID)
		if err := s.creditRepo.ReplaceForAccount(ctx, internalAccountID, linkedItem.UserID, credit); err != nil {
			return result, fmt.Errorf("failed to replace credit liability for account %s: %w", internalAccountID, err)
		}
		result.CreditProcessed++
		result.AccountsProcessed++
	}

	// Process mortgage liabilities
	for _, pm := range liabilities.Mortgage {
		internalAccountID, ok := accountMap[pm.AccountId]
		if !ok {
			s.logger.Warn("no internal account for mortgage liability, skipping",
				"plaid_account_id", pm.AccountId,
			)
			continue
		}

		mortgage := mapPlaidMortgageLiability(pm, linkedItem.UserID, internalAccountID)
		if err := s.mortgageRepo.ReplaceForAccount(ctx, internalAccountID, linkedItem.UserID, mortgage); err != nil {
			return result, fmt.Errorf("failed to replace mortgage liability for account %s: %w", internalAccountID, err)
		}
		result.MortgageProcessed++
		result.AccountsProcessed++
	}

	// Process student loan liabilities
	for _, ps := range liabilities.Student {
		plaidAccountID := ps.AccountId.Get()
		if plaidAccountID == nil || *plaidAccountID == "" {
			continue
		}
		internalAccountID, ok := accountMap[*plaidAccountID]
		if !ok {
			s.logger.Warn("no internal account for student loan liability, skipping",
				"plaid_account_id", *plaidAccountID,
			)
			continue
		}

		student := mapPlaidStudentLoan(ps, linkedItem.UserID, internalAccountID)
		if err := s.studentLoanRepo.ReplaceForAccount(ctx, internalAccountID, linkedItem.UserID, student); err != nil {
			return result, fmt.Errorf("failed to replace student loan liability for account %s: %w", internalAccountID, err)
		}
		result.StudentLoansProcessed++
		result.AccountsProcessed++
	}

	// Update liabilities_last_synced_at — non-fatal on failure
	syncedAt := time.Now().UTC()
	if err := s.linkedItemRepo.UpdateLiabilitiesLastSyncedAt(ctx, linkedItem.ID, syncedAt); err != nil {
		s.logger.Warn("failed to update liabilities_last_synced_at",
			"item_id", linkedItem.ID,
			"error", err,
		)
	}

	s.logger.Info("liabilities sync complete",
		"item_id", linkedItem.ID,
		"credit_processed", result.CreditProcessed,
		"mortgage_processed", result.MortgageProcessed,
		"student_loans_processed", result.StudentLoansProcessed,
		"accounts_processed", result.AccountsProcessed,
	)

	if s.analytics != nil {
		s.analytics.Capture(linkedItem.UserID, phclient.EventLiabilitiesSynced, map[string]interface{}{
			"credit_processed":        result.CreditProcessed,
			"mortgage_processed":      result.MortgageProcessed,
			"student_loans_processed": result.StudentLoansProcessed,
			"accounts_processed":      result.AccountsProcessed,
		})
	}

	return result, nil
}

// ============================================================================
// Plaid → Domain Mapping Helpers
// ============================================================================

func mapPlaidCreditLiability(pc plaid.CreditCardLiability, userID, accountID string) *domain.CreditLiability {
	credit := &domain.CreditLiability{
		UserID:    userID,
		AccountID: accountID,
		SyncedAt:  time.Now().UTC(),
	}

	if v := pc.IsOverdue.Get(); v != nil {
		credit.IsOverdue = v
	}
	if v := pc.LastPaymentAmount.Get(); v != nil {
		credit.LastPaymentAmount = v
	}
	if v := pc.LastPaymentDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			credit.LastPaymentDate = &t
		}
	}
	if v := pc.LastStatementIssueDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			credit.LastStatementIssueDate = &t
		}
	}
	if v := pc.LastStatementBalance.Get(); v != nil {
		credit.LastStatementBalance = v
	}
	if v := pc.MinimumPaymentAmount.Get(); v != nil {
		credit.MinimumPaymentAmount = v
	}
	if v := pc.NextPaymentDueDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			credit.NextPaymentDueDate = &t
		}
	}

	credit.APRs = mapPlaidAPRs(pc.Aprs)
	return credit
}

func mapPlaidAPRs(plaidAPRs []plaid.APR) []domain.CreditLiabilityAPR {
	aprs := make([]domain.CreditLiabilityAPR, 0, len(plaidAPRs))
	for _, pa := range plaidAPRs {
		apr := domain.CreditLiabilityAPR{
			RatePercentage: pa.AprPercentage,
			RateType:       pa.AprType,
		}
		if v := pa.BalanceSubjectToApr.Get(); v != nil {
			apr.BalanceSubjectToRate = v
		}
		if v := pa.InterestChargeAmount.Get(); v != nil {
			apr.InterestChargeAmount = v
		}
		aprs = append(aprs, apr)
	}
	return aprs
}

func mapPlaidMortgageLiability(pm plaid.MortgageLiability, userID, accountID string) *domain.MortgageLiability {
	mortgage := &domain.MortgageLiability{
		UserID:    userID,
		AccountID: accountID,
		SyncedAt:  time.Now().UTC(),
	}
	mortgage.AccountNumber = &pm.AccountNumber

	if v := pm.CurrentLateFee.Get(); v != nil {
		mortgage.CurrentLateFee = v
	}
	if v := pm.EscrowBalance.Get(); v != nil {
		mortgage.EscrowBalance = v
	}
	if v := pm.HasPmi.Get(); v != nil {
		mortgage.HasPMI = v
	}
	if v := pm.HasPrepaymentPenalty.Get(); v != nil {
		mortgage.HasPrepaymentPenalty = v
	}

	// Flatten interest rate
	if v := pm.InterestRate.Percentage.Get(); v != nil {
		mortgage.InterestRatePercentage = v
	}
	if v := pm.InterestRate.Type.Get(); v != nil {
		mortgage.InterestRateType = v
	}

	if v := pm.LastPaymentAmount.Get(); v != nil {
		mortgage.LastPaymentAmount = v
	}
	if v := pm.LastPaymentDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			mortgage.LastPaymentDate = &t
		}
	}
	if v := pm.LoanTypeDescription.Get(); v != nil {
		mortgage.LoanTypeDescription = v
	}
	if v := pm.LoanTerm.Get(); v != nil {
		mortgage.LoanTerm = v
	}
	if v := pm.MaturityDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			mortgage.MaturityDate = &t
		}
	}
	if v := pm.NextMonthlyPayment.Get(); v != nil {
		mortgage.NextMonthlyPayment = v
	}
	if v := pm.NextPaymentDueDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			mortgage.NextPaymentDueDate = &t
		}
	}
	if v := pm.OriginationDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			mortgage.OriginationDate = &t
		}
	}
	if v := pm.OriginationPrincipalAmount.Get(); v != nil {
		mortgage.OriginationPrincipalAmount = v
	}
	if v := pm.PastDueAmount.Get(); v != nil {
		mortgage.PastDueAmount = v
	}

	// Flatten property address
	if v := pm.PropertyAddress.Street.Get(); v != nil {
		mortgage.PropertyAddressStreet = v
	}
	if v := pm.PropertyAddress.City.Get(); v != nil {
		mortgage.PropertyAddressCity = v
	}
	if v := pm.PropertyAddress.Region.Get(); v != nil {
		mortgage.PropertyAddressRegion = v
	}
	if v := pm.PropertyAddress.PostalCode.Get(); v != nil {
		mortgage.PropertyAddressPostalCode = v
	}
	if v := pm.PropertyAddress.Country.Get(); v != nil {
		mortgage.PropertyAddressCountry = v
	}

	if v := pm.YtdInterestPaid.Get(); v != nil {
		mortgage.YTDInterestPaid = v
	}
	if v := pm.YtdPrincipalPaid.Get(); v != nil {
		mortgage.YTDPrincipalPaid = v
	}

	return mortgage
}

func mapPlaidStudentLoan(ps plaid.StudentLoan, userID, accountID string) *domain.StudentLoanLiability {
	interestRate := ps.InterestRatePercentage
	student := &domain.StudentLoanLiability{
		UserID:                 userID,
		AccountID:              accountID,
		InterestRatePercentage: &interestRate,
		DisbursementDates:      ps.DisbursementDates,
		SyncedAt:               time.Now().UTC(),
	}

	if v := ps.AccountNumber.Get(); v != nil {
		student.AccountNumber = v
	}
	if v := ps.ExpectedPayoffDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.ExpectedPayoffDate = &t
		}
	}
	if v := ps.Guarantor.Get(); v != nil {
		student.Guarantor = v
	}
	if v := ps.IsOverdue.Get(); v != nil {
		student.IsOverdue = v
	}
	if v := ps.LastPaymentAmount.Get(); v != nil {
		student.LastPaymentAmount = v
	}
	if v := ps.LastPaymentDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.LastPaymentDate = &t
		}
	}
	if v := ps.LastStatementIssueDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.LastStatementIssueDate = &t
		}
	}
	if v := ps.LoanName.Get(); v != nil {
		student.LoanName = v
	}

	// Flatten loan_status
	if v := ps.LoanStatus.Type.Get(); v != nil {
		student.LoanStatusType = v
	}
	if v := ps.LoanStatus.EndDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.LoanStatusEndDate = &t
		}
	}

	if v := ps.MinimumPaymentAmount.Get(); v != nil {
		student.MinimumPaymentAmount = v
	}
	if v := ps.NextPaymentDueDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.NextPaymentDueDate = &t
		}
	}
	if v := ps.OriginationDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.OriginationDate = &t
		}
	}
	if v := ps.OriginationPrincipalAmount.Get(); v != nil {
		student.OriginationPrincipalAmount = v
	}
	if v := ps.OutstandingInterestAmount.Get(); v != nil {
		student.OutstandingInterestAmount = v
	}
	if v := ps.PaymentReferenceNumber.Get(); v != nil {
		student.PaymentReferenceNumber = v
	}

	// Flatten pslf_status
	if v := ps.PslfStatus.EstimatedEligibilityDate.Get(); v != nil && *v != "" {
		if t, err := time.Parse("2006-01-02", *v); err == nil {
			student.PSLFEstimatedEligibilityDate = &t
		}
	}
	if v := ps.PslfStatus.PaymentsMade.Get(); v != nil {
		n := int(*v)
		student.PSLFPaymentsMade = &n
	}
	if v := ps.PslfStatus.PaymentsRemaining.Get(); v != nil {
		n := int(*v)
		student.PSLFPaymentsRemaining = &n
	}

	// Flatten repayment_plan
	if v := ps.RepaymentPlan.Type.Get(); v != nil {
		student.RepaymentPlanType = v
	}
	if v := ps.RepaymentPlan.Description.Get(); v != nil {
		student.RepaymentPlanDescription = v
	}

	if v := ps.SequenceNumber.Get(); v != nil {
		student.SequenceNumber = v
	}

	// Flatten servicer_address
	if v := ps.ServicerAddress.Street.Get(); v != nil {
		student.ServicerAddressStreet = v
	}
	if v := ps.ServicerAddress.City.Get(); v != nil {
		student.ServicerAddressCity = v
	}
	if v := ps.ServicerAddress.Region.Get(); v != nil {
		student.ServicerAddressRegion = v
	}
	if v := ps.ServicerAddress.PostalCode.Get(); v != nil {
		student.ServicerAddressPostalCode = v
	}
	if v := ps.ServicerAddress.Country.Get(); v != nil {
		student.ServicerAddressCountry = v
	}

	if v := ps.YtdInterestPaid.Get(); v != nil {
		student.YTDInterestPaid = v
	}
	if v := ps.YtdPrincipalPaid.Get(); v != nil {
		student.YTDPrincipalPaid = v
	}

	return student
}
