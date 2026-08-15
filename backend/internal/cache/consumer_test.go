package cache

import (
	"reflect"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeFor_NilHouseholdScopeIsPersonal(t *testing.T) {
	s := ScopeFor("user-1", nil)
	assert.Equal(t, ScopeUser, s.Kind)
	assert.Equal(t, "user-1", s.ID)
}

func TestScopeFor_NonNilHouseholdScopeIsHousehold(t *testing.T) {
	s := ScopeFor("user-1", &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-1", "user-2"}})
	assert.Equal(t, ScopeHousehold, s.Kind)
	assert.Equal(t, "hh-1", s.ID)
}

func TestKeyEnc_SetsArgHash(t *testing.T) {
	k := NewKey(NamespaceAnalytics, Scope{Kind: ScopeUser, ID: "u1"}, "SomeMethod").
		Enc(func(e *ArgEncoder) { e.String("x") })
	assert.NotEmpty(t, k.ArgHash)
}

// hashSpendingFilters/hashCashFlowFilters/hashHouseholdScope are tiny local helpers so the
// coverage/mutation tests below can assert on the encoder's actual output without going through
// a full Key.
func hashSpendingFilters(requesterID string, f domain.SpendingFilters) string {
	e := NewArgEncoder()
	EncodeSpendingFilters(e, requesterID, f)
	return e.Hash()
}

func hashCashFlowFilters(requesterID string, f domain.CashFlowFilters) string {
	e := NewArgEncoder()
	EncodeCashFlowFilters(e, requesterID, f)
	return e.Hash()
}

func hashHouseholdScope(requesterID string, hh *domain.HouseholdScope) string {
	e := NewArgEncoder()
	EncodeHouseholdScope(e, requesterID, hh)
	return e.Hash()
}

// TestSpendingFiltersEncoder_CoversEveryStructField fails the moment a field is added to
// domain.SpendingFilters without a corresponding entry here (and, in practice, without a
// corresponding EncodeSpendingFilters call) — this is what stops a future field addition from
// silently creating key collisions across semantically different queries.
func TestSpendingFiltersEncoder_CoversEveryStructField(t *testing.T) {
	encodedFields := map[string]bool{
		"StartDate":         true,
		"EndDate":           true,
		"AccountTypes":      true,
		"IncludeTransfers":  true,
		"Category":          true,
		"CustomCategoryIDs": true,
		"RollUpChildren":    true,
		"HouseholdScope":    true,
	}
	typ := reflect.TypeOf(domain.SpendingFilters{})
	require.Equal(t, len(encodedFields), typ.NumField(), "encodedFields is out of sync with domain.SpendingFilters's field count")
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		assert.True(t, encodedFields[name], "domain.SpendingFilters field %q is not covered by EncodeSpendingFilters's encodedFields set", name)
	}
}

// TestCashFlowFiltersEncoder_CoversEveryStructField is the CashFlowFilters analogue of the above.
func TestCashFlowFiltersEncoder_CoversEveryStructField(t *testing.T) {
	encodedFields := map[string]bool{
		"StartDate":        true,
		"EndDate":          true,
		"AccountTypes":     true,
		"IncludeTransfers": true,
		"Grouping":         true,
		"HouseholdScope":   true,
	}
	typ := reflect.TypeOf(domain.CashFlowFilters{})
	require.Equal(t, len(encodedFields), typ.NumField(), "encodedFields is out of sync with domain.CashFlowFilters's field count")
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		assert.True(t, encodedFields[name], "domain.CashFlowFilters field %q is not covered by EncodeCashFlowFilters's encodedFields set", name)
	}
}

// TestHouseholdScopeEncoder_CoversEveryStructField is the HouseholdScope analogue.
func TestHouseholdScopeEncoder_CoversEveryStructField(t *testing.T) {
	encodedFields := map[string]bool{
		"HouseholdID":   true,
		"MemberUserIDs": true,
	}
	typ := reflect.TypeOf(domain.HouseholdScope{})
	require.Equal(t, len(encodedFields), typ.NumField(), "encodedFields is out of sync with domain.HouseholdScope's field count")
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		assert.True(t, encodedFields[name], "domain.HouseholdScope field %q is not covered by EncodeHouseholdScope's encodedFields set", name)
	}
}

func baseSpendingFilters() domain.SpendingFilters {
	return domain.SpendingFilters{
		StartDate:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:           time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
		AccountTypes:      []string{"checking"},
		IncludeTransfers:  false,
		Category:          "food",
		CustomCategoryIDs: []string{"cat-1"},
		RollUpChildren:    false,
		HouseholdScope:    &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u1"}},
	}
}

func baseCashFlowFilters() domain.CashFlowFilters {
	return domain.CashFlowFilters{
		StartDate:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:          time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
		AccountTypes:     []string{"checking"},
		IncludeTransfers: false,
		Grouping:         "daily",
		HouseholdScope:   &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u1"}},
	}
}

// TestEveryEncodedField_MutationChangesHash is a property test: for every field listed in each
// encoder's coverage set, mutating just that field (holding every other field fixed) must change
// the resulting hash. This is what catches a field that IS listed in encodedFields but whose
// e.XXX(...) call was accidentally deleted from the encoder body — the coverage test alone can't
// catch that, because the field name is still present in the encodedFields set.
func TestEveryEncodedField_MutationChangesHash(t *testing.T) {
	t.Run("SpendingFilters", func(t *testing.T) {
		base := baseSpendingFilters()
		baseHash := hashSpendingFilters("requester-1", base)

		mutations := map[string]func(*domain.SpendingFilters){
			"StartDate":         func(f *domain.SpendingFilters) { f.StartDate = f.StartDate.Add(time.Hour) },
			"EndDate":           func(f *domain.SpendingFilters) { f.EndDate = f.EndDate.Add(time.Hour) },
			"AccountTypes":      func(f *domain.SpendingFilters) { f.AccountTypes = []string{"savings"} },
			"IncludeTransfers":  func(f *domain.SpendingFilters) { f.IncludeTransfers = true },
			"Category":          func(f *domain.SpendingFilters) { f.Category = "travel" },
			"CustomCategoryIDs": func(f *domain.SpendingFilters) { f.CustomCategoryIDs = []string{"cat-2"} },
			"RollUpChildren":    func(f *domain.SpendingFilters) { f.RollUpChildren = true },
			"HouseholdScope": func(f *domain.SpendingFilters) {
				f.HouseholdScope = &domain.HouseholdScope{HouseholdID: "hh-2", MemberUserIDs: []string{"u2"}}
			},
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				mutated := baseSpendingFilters()
				mutate(&mutated)
				assert.NotEqual(t, baseHash, hashSpendingFilters("requester-1", mutated), "mutating field %q did not change the hash", name)
			})
		}

		// Issue #1531 H1: the REQUESTER ID must also be part of the hash when a household scope is
		// present — this is the regression coverage for the cross-member cache leak (see
		// household_scope_cache_parity_integration_test.go's
		// TestAnalyticsCacheParity_HouseholdScope_CrossMemberRequesterIsolation for the end-to-end
		// proof against real household-scoped SQL).
		t.Run("RequesterID", func(t *testing.T) {
			assert.NotEqual(t, baseHash, hashSpendingFilters("requester-2", base),
				"changing the requesting userID must change the hash for a household-scoped query")
		})
	})

	t.Run("CashFlowFilters", func(t *testing.T) {
		base := baseCashFlowFilters()
		baseHash := hashCashFlowFilters("requester-1", base)

		mutations := map[string]func(*domain.CashFlowFilters){
			"StartDate":        func(f *domain.CashFlowFilters) { f.StartDate = f.StartDate.Add(time.Hour) },
			"EndDate":          func(f *domain.CashFlowFilters) { f.EndDate = f.EndDate.Add(time.Hour) },
			"AccountTypes":     func(f *domain.CashFlowFilters) { f.AccountTypes = []string{"savings"} },
			"IncludeTransfers": func(f *domain.CashFlowFilters) { f.IncludeTransfers = true },
			"Grouping":         func(f *domain.CashFlowFilters) { f.Grouping = "weekly" },
			"HouseholdScope": func(f *domain.CashFlowFilters) {
				f.HouseholdScope = &domain.HouseholdScope{HouseholdID: "hh-2", MemberUserIDs: []string{"u2"}}
			},
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				mutated := baseCashFlowFilters()
				mutate(&mutated)
				assert.NotEqual(t, baseHash, hashCashFlowFilters("requester-1", mutated), "mutating field %q did not change the hash", name)
			})
		}

		t.Run("RequesterID", func(t *testing.T) {
			assert.NotEqual(t, baseHash, hashCashFlowFilters("requester-2", base),
				"changing the requesting userID must change the hash for a household-scoped query")
		})
	})

	t.Run("HouseholdScope", func(t *testing.T) {
		base := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u1"}}
		baseHash := hashHouseholdScope("requester-1", base)

		mutations := map[string]func() *domain.HouseholdScope{
			"HouseholdID": func() *domain.HouseholdScope {
				return &domain.HouseholdScope{HouseholdID: "hh-2", MemberUserIDs: []string{"u1"}}
			},
			"MemberUserIDs": func() *domain.HouseholdScope {
				return &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u2"}}
			},
		}
		for name, build := range mutations {
			t.Run(name, func(t *testing.T) {
				assert.NotEqual(t, baseHash, hashHouseholdScope("requester-1", build()), "mutating field %q did not change the hash", name)
			})
		}

		t.Run("NilVsNonNil", func(t *testing.T) {
			assert.NotEqual(t, baseHash, hashHouseholdScope("requester-1", nil))
		})

		// Issue #1531 H1's core regression coverage at the encoder level: two DIFFERENT requesters
		// reading the identical household scope must hash differently, even though hh itself
		// (HouseholdID + MemberUserIDs) is byte-identical for both.
		t.Run("RequesterID", func(t *testing.T) {
			assert.NotEqual(t, baseHash, hashHouseholdScope("requester-2", base),
				"changing the requesting userID must change the hash even though the household scope itself is unchanged")
		})

		// A nil household scope (personal mode) must hash identically regardless of requester —
		// the requester is folded in only inside the hh != nil branch. Personal-scope cache keys
		// already carry the requester via cache.ScopeFor's ScopeID, so this would be a redundant
		// (and behavior-changing) hash bump for every non-household call site.
		t.Run("RequesterID_IrrelevantWhenPersonal", func(t *testing.T) {
			assert.Equal(t, hashHouseholdScope("requester-1", nil), hashHouseholdScope("requester-2", nil))
		})
	})
}

// TestEncodeHouseholdScope_MemberOrderAndDuplicatesDoNotMatter documents the deliberate
// order/duplicate-insensitivity of MemberUserIDs (StringSetSorted, matching the `= ANY()` SQL).
func TestEncodeHouseholdScope_MemberOrderAndDuplicatesDoNotMatter(t *testing.T) {
	a := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u1", "u2"}}
	b := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"u2", "u1", "u2"}}
	assert.Equal(t, hashHouseholdScope("requester-1", a), hashHouseholdScope("requester-1", b))
}
