package domain

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccountBalanceDetail_NeverCarriesMask is a regression guard, not a
// behavior test — nothing is broken today. Terms of Service §9.3 and Privacy
// Policy §9.1 (Issue #1372) promise household members that account numbers
// or masks are NEVER shared, regardless of household sharing settings.
//
// AccountBalanceDetail is the struct that carries per-account balance info in
// BOTH the personal and household-scope balance-summary/net-worth responses
// (see dashboard_service.go's getBalanceSummaryHousehold/getNetWorthHousehold,
// which construct it identically to the personal path — ID, Name, Balance
// only). The repository layer (account_repository.go GetForHouseholdScopeActiveNow)
// does select accounts.mask from the database, but that value is never
// threaded into this struct today.
//
// This test uses reflection so it fails loudly if ANY field is ever added
// whose name or `json` tag mentions "mask" — under any casing or naming
// convention (Mask, AccountMask, MaskedNumber, etc.) — not just one guessed
// exact name. If this test starts failing, it means someone extended this
// struct with mask data reachable from a household-scoped response, silently
// breaking the legal promise above. Do NOT silence this test by relaxing the
// assertion — escalate for a product/legal decision instead.
func TestAccountBalanceDetail_NeverCarriesMask(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(AccountBalanceDetail{})

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		fieldNameLower := strings.ToLower(field.Name)
		assert.NotContains(t, fieldNameLower, "mask",
			"AccountBalanceDetail.%s: field name must not reference an account mask — "+
				"household members must never receive account numbers/masks (ToS §9.3, Privacy Policy §9.1)",
			field.Name)

		jsonTag := field.Tag.Get("json")
		jsonTagLower := strings.ToLower(jsonTag)
		assert.NotContains(t, jsonTagLower, "mask",
			"AccountBalanceDetail.%s: json tag %q must not reference an account mask — "+
				"household members must never receive account numbers/masks (ToS §9.3, Privacy Policy §9.1)",
			field.Name, jsonTag)
	}
}
