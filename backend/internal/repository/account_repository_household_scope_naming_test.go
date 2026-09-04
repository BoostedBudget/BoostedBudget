package repository

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccountRepository_HouseholdScopeReadNamesDoNotLieAboutAsOf is the API-shape half of #1569
// AC2. The acceptance criterion was that the bare "GetForHouseholdScope" read must either HONOUR
// its asOf argument or be renamed so its signature stops implying it does. Honouring it is not
// available: task 12's ruling (account_liveness_sql.go) keeps the current-state and historical
// universes deliberately separate, and GetForHouseholdScopeLiveAsOf already IS the historical
// answer — so widening the current-state read to recorded liveness would not close a gap, it
// would merge two universes the ruling forbids merging and leave the codebase with two spellings
// of one query.
//
// The rename is therefore the closure, and this test is what makes it stick. It asserts on the
// INTERFACE rather than on behaviour because the defect was never behavioural: the read did
// exactly what its body says, under a name that promised something else. A behavioural test
// cannot fail on a misleading name; a name test can.
//
// The date argument is NOT dead in the renamed method — householdAccountsCTEForBalancesAsOf binds
// it as $4 to resolve share grants and the joint-account fingerprint winner AT OR BEFORE that
// instant (pinned by
// TestAccountRepository_GetForHouseholdScopeActiveNow_JointAccountWinnerRespectsAsOfCutoff). What
// it never scoped, and now never claims to scope, is account STATUS.
func TestAccountRepository_HouseholdScopeReadNamesDoNotLieAboutAsOf(t *testing.T) {
	iface := reflect.TypeOf((*AccountRepository)(nil)).Elem()

	// Spelled in pieces so this literal cannot itself be swept by a future rename that is
	// supposed to make it fail.
	banned := "GetForHousehold" + "Scope"
	_, bare := iface.MethodByName(banned)
	assert.False(t, bare,
		"%s takes a date and then filters on CURRENT status — #1569 AC2 required that name to go "+
			"away. Use GetForHouseholdScopeActiveNow (current status, grants resolved as of the "+
			"date) or GetForHouseholdScopeLiveAsOf (recorded liveness at the date).", banned)

	for _, name := range []string{"GetForHouseholdScopeActiveNow", "GetForHouseholdScopeLiveAsOf"} {
		m, ok := iface.MethodByName(name)
		if !assert.True(t, ok, "%s must exist on AccountRepository", name) {
			continue
		}
		// Same shape for both, so a caller cannot pick the wrong one by accident of signature —
		// only deliberately, by name.
		assert.Equal(t, 4, m.Type.NumIn(), "%s: ctx, requestingUserID, scope, date", name)
	}
}
