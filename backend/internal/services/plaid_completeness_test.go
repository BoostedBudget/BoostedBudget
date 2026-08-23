package services

import (
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
)

// TestPlaidResponseAccountIDs_ExtractsAccountId pins the exact field this codebase's
// completeness signal depends on. If a future plaid-go regeneration renames or restructures
// AccountBase.AccountId, this test breaks loudly instead of PlaidResponseAccountIDs silently
// starting to return an empty set — which would make every liability/holdings account look
// "absent from the response" and revert Task 4 back to the old wipe-on-absence behavior.
func TestPlaidResponseAccountIDs_ExtractsAccountId(t *testing.T) {
	accounts := []plaid.AccountBase{
		{AccountId: "acct-1"},
		{AccountId: "acct-2"},
	}

	ids := PlaidResponseAccountIDs(accounts)

	assert.Len(t, ids, 2)
	_, hasOne := ids["acct-1"]
	_, hasTwo := ids["acct-2"]
	_, hasThree := ids["acct-3"]
	assert.True(t, hasOne)
	assert.True(t, hasTwo)
	assert.False(t, hasThree)
}

// TestPlaidResponseAccountIDs_EmptyInput verifies the zero-account case returns an empty,
// non-nil set (so callers can safely index into it without a nil-map read).
func TestPlaidResponseAccountIDs_EmptyInput(t *testing.T) {
	ids := PlaidResponseAccountIDs(nil)
	assert.NotNil(t, ids)
	assert.Empty(t, ids)
}

// TestPlaidResponseAccountIDs_SkipsBlankAccountId guards against a zero-value AccountBase
// (e.g. a defensive append bug) silently registering as "account '' is present," which would
// make every real absent-account check accidentally match on an empty plaidAccountID.
func TestPlaidResponseAccountIDs_SkipsBlankAccountId(t *testing.T) {
	accounts := []plaid.AccountBase{
		{AccountId: ""},
		{AccountId: "acct-real"},
	}

	ids := PlaidResponseAccountIDs(accounts)

	assert.Len(t, ids, 1)
	_, hasBlank := ids[""]
	assert.False(t, hasBlank)
}
