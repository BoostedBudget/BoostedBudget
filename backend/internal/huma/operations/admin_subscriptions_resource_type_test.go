package operations

import (
	"reflect"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResourceTypeEnumMatchesConstants guards against the admin override resource_type
// enum tag drifting from the real domain.ResourceType constants (issue #1288).
//
// It catches two failure modes:
//  1. A bogus value in the enum (e.g. "automation_rules") that has no matching constant.
//  2. A real constant (e.g. "transaction_history_days") missing from the enum.
//
// The SetLimit/GetLimit round-trip additionally proves each constant is wired in both
// switch statements — SetLimit has no default branch, so an unwired type is a silent no-op.
func TestResourceTypeEnumMatchesConstants(t *testing.T) {
	// --- part 1: enum tag matches AllResourceTypes set ---
	bodyField, ok := reflect.TypeOf(AdminCreateOverrideInput{}).FieldByName("Body")
	require.True(t, ok, "AdminCreateOverrideInput should have a Body field")

	rtField, ok := bodyField.Type.FieldByName("ResourceType")
	require.True(t, ok, "Body struct should have a ResourceType field")

	rawEnum := rtField.Tag.Get("enum")
	require.NotEmpty(t, rawEnum, "ResourceType field should have a non-empty enum tag")

	enumValues := strings.Split(rawEnum, ",")

	want := make([]string, len(domain.AllResourceTypes))
	for i, rt := range domain.AllResourceTypes {
		want[i] = string(rt)
	}

	assert.ElementsMatch(t, want, enumValues,
		"enum tag values must exactly match domain.AllResourceTypes (no extras, no missing)")

	// --- part 2: SetLimit/GetLimit round-trip for every constant ---
	for _, rt := range domain.AllResourceTypes {
		limits := &domain.SubscriptionLimits{}
		limits.SetLimit(rt, 42)
		assert.Equal(t, 42, limits.GetLimit(rt),
			"resource type %q not wired in SetLimit/GetLimit switches", rt)
	}
}
