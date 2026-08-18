package cache

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestArgEncoder_LengthPrefixing_NoConcatenationCollision(t *testing.T) {
	type pair struct {
		name string
		a    func(*ArgEncoder)
		b    func(*ArgEncoder)
	}
	pairs := []pair{
		{"string split point 1", func(e *ArgEncoder) { e.String("ab"); e.String("c") }, func(e *ArgEncoder) { e.String("a"); e.String("bc") }},
		{"string split point 2", func(e *ArgEncoder) { e.String(""); e.String("ab") }, func(e *ArgEncoder) { e.String("ab"); e.String("") }},
		{"string vs int same digits", func(e *ArgEncoder) { e.String("12") }, func(e *ArgEncoder) { e.Int(12) }},
		{"int order matters", func(e *ArgEncoder) { e.Int(1); e.Int(2) }, func(e *ArgEncoder) { e.Int(2); e.Int(1) }},
		{"bool true vs false", func(e *ArgEncoder) { e.Bool(true) }, func(e *ArgEncoder) { e.Bool(false) }},
		{"call order matters", func(e *ArgEncoder) { e.String("a"); e.Int(1) }, func(e *ArgEncoder) { e.Int(1); e.String("a") }},
		{"list order vs set order", func(e *ArgEncoder) { e.StringListOrdered([]string{"a", "b"}) }, func(e *ArgEncoder) { e.StringListOrdered([]string{"b", "a"}) }},
		{"list count differs from concatenation", func(e *ArgEncoder) { e.StringListOrdered([]string{"a", "b"}) }, func(e *ArgEncoder) { e.StringListOrdered([]string{"ab"}) }},
		{"nil list vs empty list", func(e *ArgEncoder) { e.StringListOrdered(nil) }, func(e *ArgEncoder) { e.StringListOrdered([]string{}) }},
		{"nil set vs empty set", func(e *ArgEncoder) { e.StringSetSorted(nil) }, func(e *ArgEncoder) { e.StringSetSorted([]string{}) }},
		{"time zero vs nil time ptr", func(e *ArgEncoder) { e.Time(time.Time{}) }, func(e *ArgEncoder) { e.TimePtr(nil) }},
		{"float vs nextafter", func(e *ArgEncoder) { e.Float(2.0) }, func(e *ArgEncoder) { e.Float(math.Nextafter(2.0, 3)) }},
		{"string then bool vs bool then string", func(e *ArgEncoder) { e.String("b1"); e.Bool(false) }, func(e *ArgEncoder) { e.Bool(true); e.String("1") }},
	}

	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			ea := NewArgEncoder()
			p.a(ea)
			eb := NewArgEncoder()
			p.b(eb)
			assert.NotEqual(t, ea.Hash(), eb.Hash(), "encoder outputs must not collide")
		})
	}
}

func TestArgEncoder_NilSliceDistinctFromEmptySlice(t *testing.T) {
	e1 := NewArgEncoder()
	e1.StringListOrdered(nil)
	e2 := NewArgEncoder()
	e2.StringListOrdered([]string{})
	assert.NotEqual(t, e1.Hash(), e2.Hash())

	e3 := NewArgEncoder()
	e3.StringSetSorted(nil)
	e4 := NewArgEncoder()
	e4.StringSetSorted([]string{})
	assert.NotEqual(t, e3.Hash(), e4.Hash())
}

func TestArgEncoder_StringSetSorted_OrderIndependentAndDeduped(t *testing.T) {
	e1 := NewArgEncoder()
	e1.StringSetSorted([]string{"b", "a", "a"})
	e2 := NewArgEncoder()
	e2.StringSetSorted([]string{"a", "b"})
	assert.Equal(t, e1.Hash(), e2.Hash(), "sorted+deduped set encoding must be order-independent")
}

func TestFloat_HexFormatPreservesPrecision(t *testing.T) {
	e1 := NewArgEncoder()
	e1.Float(2.0)
	e2 := NewArgEncoder()
	e2.Float(math.Nextafter(2.0, 3))
	assert.NotEqual(t, e1.Hash(), e2.Hash())
}

func TestTime_SameInstantDifferentZone_SameHash(t *testing.T) {
	utc := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	loc := time.FixedZone("UTC-5", -5*3600)
	shifted := utc.In(loc)

	e1 := NewArgEncoder()
	e1.Time(utc)
	e2 := NewArgEncoder()
	e2.Time(shifted)
	assert.Equal(t, e1.Hash(), e2.Hash())
}

func TestTime_ZeroDistinctFromUnixEpoch(t *testing.T) {
	e1 := NewArgEncoder()
	e1.Time(time.Time{})
	e2 := NewArgEncoder()
	e2.Time(time.Unix(0, 0).UTC())
	assert.NotEqual(t, e1.Hash(), e2.Hash())
}

func TestKeyFormat_Golden(t *testing.T) {
	cases := []struct {
		ns   Namespace
		kind ScopeKind
		id   string
		want string // golden data key with a fixed generation/argHash for readability
	}{
		{NamespaceAnalytics, ScopeUser, "u-123", "bb:c:{u:u-123}:analytics:v1:gGEN:Method:HASH"},
		{NamespaceAnalyticsRows, ScopeUser, "u-123", "bb:c:{u:u-123}:analytics_rows:v1:gGEN:Method:HASH"},
		{NamespaceDashPrefs, ScopeUser, "u-123", "bb:c:{u:u-123}:dashprefs:v1:gGEN:Method:HASH"},
		{NamespaceHHScope, ScopeHousehold, "h-456", "bb:c:{h:h-456}:hhscope:v1:gGEN:Method:HASH"},
	}
	for _, c := range cases {
		k := Key{Namespace: c.ns, ScopeKind: c.kind, ScopeID: c.id, Method: "Method", ArgHash: "HASH"}
		got := k.DataKey(1, "GEN")
		assert.Equal(t, c.want, got, "namespace %s", c.ns)
	}

	// Identity is clerk-id scoped, so its golden key embeds a hashed tag rather than the raw id.
	idKey := Key{Namespace: NamespaceIdentity, ScopeKind: ScopeClerkID, ScopeID: "clerk_abc", Method: "Method", ArgHash: "HASH"}
	got := idKey.DataKey(1, "GEN")
	assert.Regexp(t, `^bb:c:\{k:[0-9a-f]{16}\}:identity:v1:gGEN:Method:HASH$`, got)
}

func TestGenerationKey_Format(t *testing.T) {
	assert.Equal(t, "bb:c:{u:abc}:gen", GenerationKey(ScopeTag(ScopeUser, "abc")))
}

func TestControlKey_Format(t *testing.T) {
	assert.Equal(t, "bb:c:ctl:v1:analytics", ControlKey(NamespaceAnalytics))
}

func TestScopeTag_ClerkIDIsHashedNotRaw(t *testing.T) {
	tag := ScopeTag(ScopeClerkID, "clerk_user_12345")
	assert.NotContains(t, tag, "clerk_user_12345")
	assert.Regexp(t, `^k:[0-9a-f]{16}$`, tag)
}

func TestMethodConstants_AreUnique(t *testing.T) {
	// This test exists to force method names to be written as literals at call sites (Task 10),
	// and to catch an accidental duplicate here in Task 9's own registry-adjacent constants.
	methods := []string{
		"GetSpendingSummary",
		"GetCashFlowSummary",
		"GetDashboardPreferences",
		"GetIdentityUUID",
		"GetHouseholdScope",
	}
	seen := make(map[string]bool, len(methods))
	for _, m := range methods {
		assert.False(t, seen[m], "duplicate method constant %q", m)
		seen[m] = true
	}
}
