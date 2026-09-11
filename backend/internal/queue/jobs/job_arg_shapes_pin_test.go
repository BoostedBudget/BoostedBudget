package jobs

import (
	"encoding/json"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalMirroredArgs is the jobs-side half of the Issue #1490 mirror pin: for each of the six
// River kinds that `internal/services` mirrors, the REAL args type — the one the worker
// deserializes river_job.args into.
//
// Registered by Kind(), never by Go type name. That is the #1486 lesson made structural: the
// producers of a kind are found by its Kind() STRING, and a type-name grep misses both the
// services mirrors and queue.RawJobArgs (whose Kind() returns a stored string, so no static grep
// can see which kinds it produces — a dead-letter replay can resurrect any of them).
var canonicalMirroredArgs = []queue.Job{
	PlaidSyncArgs{},
	InvestmentHoldingsSyncArgs{},
	InvestmentTransactionsSyncArgs{},
	ItemCleanupArgs{},
	EffectiveCategoryRefreshArgs{},
	BudgetHierarchyResyncArgs{},
	EmailChangePropagationArgs{},
}

// TestMirroredJobArgsShapes_CoversExactlyTheMirroredKinds pins the table's key set against the
// canonical types themselves, so neither list can grow or shrink alone. Without this, adding a
// seventh mirror and forgetting the table would leave the new pair unpinned and both pin tests
// green.
func TestMirroredJobArgsShapes_CoversExactlyTheMirroredKinds(t *testing.T) {
	declared := map[string]bool{}
	for k := range queue.MirroredJobArgsShapes {
		declared[k] = true
	}
	for _, args := range canonicalMirroredArgs {
		kind := args.Kind()
		assert.True(t, declared[kind],
			"kind %q has a canonical args type registered here but no entry in "+
				"queue.MirroredJobArgsShapes, so its services-side mirror is unpinned", kind)
		delete(declared, kind)
	}
	assert.Empty(t, declared,
		"queue.MirroredJobArgsShapes declares kinds with no canonical args type in this list: %v", declared)
}

// TestCanonicalJobArgs_MatchShapeTable is the assertion that fails when a jobs.*Args type gains,
// loses or renames a JSON field without the services mirror following. It is one half of the pin;
// internal/services/job_arg_shapes_pin_test.go is the other, and both compare against the SAME
// queue.MirroredJobArgsShapes entry, which is why a change to either side alone reddens exactly one
// of them.
func TestCanonicalJobArgs_MatchShapeTable(t *testing.T) {
	for _, args := range canonicalMirroredArgs {
		kind := args.Kind()
		t.Run(kind, func(t *testing.T) {
			shape, ok := queue.MirroredJobArgsShapes[kind]
			require.True(t, ok, "no shape declared for kind %q", kind)

			got, omitEmpty := queue.JSONFieldNames(args)
			assert.ElementsMatch(t, shape.Fields, got,
				"the canonical args type for kind %q no longer has the JSON field set declared in "+
					"queue.MirroredJobArgsShapes. River deserializes river_job.args into THIS type, so "+
					"until the services-side mirror is updated to match, that producer's payload is "+
					"read with a zero value for any field it no longer names", kind)

			// The omitempty set, compared against the DECLARED list rather than against itself.
			// Deriving the expectation from the same tags being checked passes in both
			// directions — verified: with the expectation derived, adding omitempty to
			// item_cleanup.reason left this whole file green.
			var gotOmitEmpty []string
			for f := range omitEmpty {
				gotOmitEmpty = append(gotOmitEmpty, f)
			}
			assert.ElementsMatch(t, shape.OmitEmpty, gotOmitEmpty,
				"kind %q's canonical args type no longer has exactly the omitempty fields declared "+
					"in queue.MirroredJobArgsShapes. omitempty decides whether an unset field is "+
					"written to river_job.args at all, so moving it changes the bytes that "+
					"UniqueOpts{ByArgs: true} deduplicates on and that the services mirror must "+
					"reproduce", kind)

			// Every deliberately-omitted field must be omitempty, or the mirror's JSON is a
			// different document from the canonical type's zero-valued JSON.
			for _, f := range shape.MirrorMayOmit {
				assert.Contains(t, shape.Fields, f,
					"MirrorMayOmit names %q, which is not a field of kind %q at all", f, kind)
				assert.Contains(t, shape.OmitEmpty, f,
					"kind %q declares that its services mirror may omit %q, but %q is NOT declared "+
						"omitempty — so the canonical type emits it (as a zero value) and the mirror "+
						"does not, which is a genuine wire-shape difference, not a safe omission",
					kind, f, f)
			}
		})
	}
}

// TestCanonicalJobArgs_ZeroValueJSONIsStable pins the actual bytes, not just the tag set: for each
// kind, marshalling the zero value must produce exactly the non-omitempty fields. This is what
// catches an `omitempty` being added to or removed from a field that the shape table says both
// sides carry — a change the ElementsMatch assertion above cannot see, because the tag NAME does
// not move.
func TestCanonicalJobArgs_ZeroValueJSONIsStable(t *testing.T) {
	for _, args := range canonicalMirroredArgs {
		kind := args.Kind()
		t.Run(kind, func(t *testing.T) {
			b, err := json.Marshal(args)
			require.NoError(t, err)

			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(b, &decoded))

			shape := queue.MirroredJobArgsShapes[kind]
			declaredOmitEmpty := map[string]bool{}
			for _, f := range shape.OmitEmpty {
				declaredOmitEmpty[f] = true
			}
			var wantPresent []string
			for _, f := range shape.Fields {
				if !declaredOmitEmpty[f] {
					wantPresent = append(wantPresent, f)
				}
			}
			gotPresent := make([]string, 0, len(decoded))
			for f := range decoded {
				gotPresent = append(gotPresent, f)
			}
			assert.ElementsMatch(t, wantPresent, gotPresent,
				"the zero value of kind %q's canonical args marshals to %s; an omitempty tag was "+
					"added or removed, which changes what a producer that leaves the field unset "+
					"actually writes to river_job.args", kind, b)
		})
	}
}
