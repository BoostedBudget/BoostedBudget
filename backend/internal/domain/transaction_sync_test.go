package domain

import (
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
)

// plaidAPIError builds an error shaped like plaid-go's GenericOpenAPIError,
// the same construction idiom used by
// services/plaid_orphan_retry_service_test.go's plaidAPIError helper.
func plaidAPIError(t *testing.T, code string) error {
	t.Helper()
	return plaid.MakeGenericOpenAPIError(
		[]byte(`{"error_type":"ITEM_ERROR","error_code":"`+code+`","error_message":"x"}`),
		"400 Bad Request",
		nil,
	)
}

func TestClassifyPlaidError_ProductsNotSupported_IsPermanent(t *testing.T) {
	err := plaidAPIError(t, "PRODUCTS_NOT_SUPPORTED")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorPermanent, result.ErrorType)
	assert.Equal(t, "PRODUCTS_NOT_SUPPORTED", result.ErrorCode)
}

// TestClassifyPlaidError_NoInvestmentAccounts_IsPermanent covers the audit-flagged missing case
// (Issue #1621, #1613's remaining edge): a supported-product / no-investment-account item — the
// investments product IS granted for this item, but it simply has zero investment accounts at
// this institution. Existing coverage above only exercises PRODUCTS_NOT_SUPPORTED (no product
// support at all) and INVALID_ACCESS_TOKEN (product-granted, but auth-broken); this is the
// distinct "product granted, nothing to sync" case, which previously fell through to Temporary
// and burned five River attempts into the DLQ for no benefit.
func TestClassifyPlaidError_NoInvestmentAccounts_IsPermanent(t *testing.T) {
	err := plaidAPIError(t, "NO_INVESTMENT_ACCOUNTS")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorPermanent, result.ErrorType)
	assert.Equal(t, "NO_INVESTMENT_ACCOUNTS", result.ErrorCode)
}

func TestClassifyPlaidError_InvalidAccessToken_IsPermanent(t *testing.T) {
	err := plaidAPIError(t, "INVALID_ACCESS_TOKEN")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorPermanent, result.ErrorType)
}

func TestClassifyPlaidError_RateLimitExceeded_IsRateLimit(t *testing.T) {
	err := plaidAPIError(t, "RATE_LIMIT_EXCEEDED")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorRateLimit, result.ErrorType)
}

func TestClassifyPlaidError_InvalidCursor_IsInvalidCursor(t *testing.T) {
	err := plaidAPIError(t, "INVALID_CURSOR")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorInvalidCursor, result.ErrorType)
}

func TestClassifyPlaidError_UnknownCode_IsTemporary(t *testing.T) {
	err := plaidAPIError(t, "SOME_UNKNOWN_CODE")

	result := ClassifyPlaidError(err)

	assert.Equal(t, PlaidErrorTemporary, result.ErrorType)
}
