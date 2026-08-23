package services

import (
	"errors"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
)

// TestPlaidAccountRoster_ExtractsAccountId pins the exact field this codebase's completeness
// signal depends on. If a future plaid-go regeneration renames or restructures
// AccountBase.AccountId, this test breaks loudly instead of the roster silently coming back
// empty — which would make every liability/holdings account look "absent from the response"
// and revert Task 4 back to the old wipe-on-absence behavior.
func TestPlaidAccountRoster_ExtractsAccountId(t *testing.T) {
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "acct-1"},
		{AccountId: "acct-2"},
	}, nil)

	assert.True(t, roster.Present("acct-1"))
	assert.True(t, roster.Present("acct-2"))
	assert.False(t, roster.Present("acct-3"))
	assert.Equal(t, 2, roster.Len())
}

// TestPlaidAccountRoster_SkipsBlankAccountId guards against a zero-value AccountBase (e.g. a
// defensive append bug) silently registering as a present account with a blank id, which
// would make every real absent-account check accidentally match on an empty plaidAccountID.
func TestPlaidAccountRoster_SkipsBlankAccountId(t *testing.T) {
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: ""},
		{AccountId: "acct-real"},
	}, nil)

	assert.False(t, roster.Present(""))
	assert.True(t, roster.Present("acct-real"))
	assert.Equal(t, 1, roster.Len())
}

// TestPlaidAccountRoster_EmptyResponseIsNotComplete is the structural half of Task 5's
// catastrophic case: an empty `accounts` array is indistinguishable from a degraded response,
// so the roster must refuse to certify ANY absence from it — Present stays a safe "no", and
// ConfirmedAbsent (the only door to a removal) must never open.
func TestPlaidAccountRoster_EmptyResponseIsNotComplete(t *testing.T) {
	for name, roster := range map[string]PlaidAccountRoster{
		"nil slice":   PlaidAccountRosterFrom(nil, nil),
		"empty slice": PlaidAccountRosterFrom([]plaid.AccountBase{}, nil),
		"all blank ids": PlaidAccountRosterFrom([]plaid.AccountBase{
			{AccountId: ""},
		}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, roster.Complete())
			assert.False(t, roster.Present("acct-1"))
			assert.False(t, roster.ConfirmedAbsent("acct-1"),
				"an empty accounts array must never prove an account is gone")
		})
	}
}

// TestPlaidAccountRoster_ErroredCallIsNotComplete: a call that returned an error cannot certify
// anything, even if it also handed back a partially-decoded accounts array.
func TestPlaidAccountRoster_ErroredCallIsNotComplete(t *testing.T) {
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "acct-1"},
	}, errors.New("plaid: 500"))

	assert.False(t, roster.Complete())
	assert.False(t, roster.ConfirmedAbsent("acct-2"))
	// Present remains honest about what was in the payload — it is only ever used to SKIP work.
	assert.True(t, roster.Present("acct-1"))
}

// TestPlaidAccountRoster_ConfirmedAbsent is the affirmative case: a successful, non-empty
// response is the only thing that proves an account Plaid did not enumerate is gone.
func TestPlaidAccountRoster_ConfirmedAbsent(t *testing.T) {
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "acct-1"},
	}, nil)

	assert.True(t, roster.Complete())
	assert.True(t, roster.ConfirmedAbsent("acct-gone"))
	assert.False(t, roster.ConfirmedAbsent("acct-1"))
	assert.False(t, roster.ConfirmedAbsent(""), "a blank plaid_account_id proves nothing")
}

// TestPlaidAccountRoster_ConfirmedAbsentIn is the affordance that means no caller ever has to
// hand-write "known accounts minus what Plaid returned" — the loop that, written naively against
// an empty or errored response, marks every account removed.
func TestPlaidAccountRoster_ConfirmedAbsentIn(t *testing.T) {
	known := []string{"acct-1", "acct-gone", "acct-2", "acct-gone"}

	t.Run("complete roster reports only the provably gone, once each", func(t *testing.T) {
		roster := PlaidAccountRosterFrom([]plaid.AccountBase{
			{AccountId: "acct-1"},
			{AccountId: "acct-2"},
		}, nil)
		assert.Equal(t, []string{"acct-gone"}, roster.ConfirmedAbsentIn(known))
	})

	t.Run("empty response reports nothing", func(t *testing.T) {
		assert.Empty(t, PlaidAccountRosterFrom(nil, nil).ConfirmedAbsentIn(known),
			"a zero-account response must not report every known account as gone")
	})

	t.Run("errored call reports nothing", func(t *testing.T) {
		roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "acct-1"}}, errors.New("plaid: 500"))
		assert.Empty(t, roster.ConfirmedAbsentIn(known))
	})

	t.Run("blank ids are never reported gone", func(t *testing.T) {
		roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "acct-1"}}, nil)
		assert.Empty(t, roster.ConfirmedAbsentIn([]string{""}))
	})

	t.Run("nothing known, nothing gone", func(t *testing.T) {
		roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "acct-1"}}, nil)
		assert.Empty(t, roster.ConfirmedAbsentIn(nil))
	})
}
