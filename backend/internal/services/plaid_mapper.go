package services

import (
	"encoding/json"
	"time"

	"github.com/plaid/plaid-go/v23/plaid"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// PlaidPFCFields holds extracted PFC v2 fields from a Plaid transaction.
type PlaidPFCFields struct {
	Primary        *string
	Detailed       *string
	ConfidenceLevel *string
	CategoryIconURL *string
}

// ExtractPlaidPFCFields reads PFC v2 categorization data from a Plaid transaction.
// Returns nil pointers for fields that are not set on the transaction.
func ExtractPlaidPFCFields(plaidTxn plaid.Transaction) PlaidPFCFields {
	var fields PlaidPFCFields

	if plaidTxn.PersonalFinanceCategory.IsSet() {
		pfcData := plaidTxn.PersonalFinanceCategory.Get()
		if pfcData != nil {
			if pfcData.Primary != "" {
				fields.Primary = &pfcData.Primary
			}
			if pfcData.Detailed != "" {
				fields.Detailed = &pfcData.Detailed
			}
			if pfcData.ConfidenceLevel.IsSet() && pfcData.ConfidenceLevel.Get() != nil && *pfcData.ConfidenceLevel.Get() != "" {
				c := *pfcData.ConfidenceLevel.Get()
				fields.ConfidenceLevel = &c
			}
		}
	}

	if plaidTxn.PersonalFinanceCategoryIconUrl != nil && *plaidTxn.PersonalFinanceCategoryIconUrl != "" {
		fields.CategoryIconURL = plaidTxn.PersonalFinanceCategoryIconUrl
	}

	return fields
}

// BuildPlaidExternalMetadata serializes Plaid-specific fields that don't have
// direct domain equivalents into a JSON string for storage in external_metadata.
func BuildPlaidExternalMetadata(plaidTxn plaid.Transaction) (*string, error) {
	metadata := make(map[string]interface{})

	if plaidTxn.Website.Get() != nil {
		metadata["website"] = *plaidTxn.Website.Get()
	}

	location := plaidTxn.Location
	if location.City.Get() != nil || location.Region.Get() != nil {
		metadata["location"] = location
	}

	paymentMeta := plaidTxn.PaymentMeta
	if paymentMeta.ReferenceNumber.Get() != nil || paymentMeta.PpdId.Get() != nil {
		metadata["payment_meta"] = paymentMeta
	}

	if len(metadata) == 0 {
		return nil, nil
	}

	bytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	metadataStr := string(bytes)
	return &metadataStr, nil
}

// ParsePlaidDate parses a Plaid date string ("YYYY-MM-DD") into a time.Time.
func ParsePlaidDate(dateStr string) (time.Time, error) {
	return time.Parse("2006-01-02", dateStr)
}

// ParsePlaidNullableDate parses an optional Plaid date string.
// Returns nil for empty or unparseable values.
func ParsePlaidNullableDate(nullableDate plaid.NullableString) *time.Time {
	if nullableDate.Get() == nil {
		return nil
	}
	t, err := time.Parse("2006-01-02", *nullableDate.Get())
	if err != nil {
		return nil
	}
	return &t
}

// MapPlaidTransactionBase maps the common Plaid transaction fields to a
// partial domain.Transaction. The caller is responsible for setting
// UserID, AccountID, LinkedItemID, UserAccountID, and ExternalTransactionID.
func MapPlaidTransactionBase(plaidTxn plaid.Transaction) (*domain.Transaction, error) {
	date, err := ParsePlaidDate(plaidTxn.Date)
	if err != nil {
		return nil, err
	}

	authorizedDate := ParsePlaidNullableDate(plaidTxn.AuthorizedDate)

	externalMetadata, err := BuildPlaidExternalMetadata(plaidTxn)
	if err != nil {
		return nil, err
	}

	pfc := ExtractPlaidPFCFields(plaidTxn)

	var paymentChannel *string
	if plaidTxn.PaymentChannel != "" {
		paymentChannel = &plaidTxn.PaymentChannel
	}

	return &domain.Transaction{
		Source:             domain.SourcePlaid,
		ExternalMetadata:   externalMetadata,
		Amount:             plaidTxn.Amount,
		Date:               date,
		AuthorizedDate:     authorizedDate,
		Name:               plaidTxn.Name,
		MerchantName:       plaidTxn.MerchantName.Get(),
		PFCPrimary:         pfc.Primary,
		PFCDetailed:        pfc.Detailed,
		PFCConfidenceLevel: pfc.ConfidenceLevel,
		PFCCategoryIconURL: pfc.CategoryIconURL,
		PaymentChannel:     paymentChannel,
		Pending:            plaidTxn.GetPending(),
	}, nil
}
