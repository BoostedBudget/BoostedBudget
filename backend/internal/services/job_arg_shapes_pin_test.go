package services

import (
	"encoding/json"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mirroredJobArgs is the services-side half of the Issue #1490 mirror pin: every local struct in
// this package that exists ONLY to enqueue a River job whose worker lives in
// internal/queue/jobs. They exist because services cannot import jobs (jobs imports services), so
// each re-declares its counterpart's JSON tags and Kind() string by hand, with nothing but this
// test to keep the two in step.
//
// Keyed by Kind(), never by Go type name — #1486's Critical defect was a second producer of
// "item_cleanup" that a Go-type-name grep could not see. Note also that
// `grep 'Kind() string { return "'` is NECESSARY BUT NOT SUFFICIENT for enumerating producers:
// queue.RawJobArgs.Kind() returns a stored string, so services.deadLetterService.Retry can
// re-enqueue ANY kind — including any of these six — and no static grep will show it.
//
// This list must stay complete. Verify with:
//
//	grep -rn 'Kind() string { return "' internal/services
var mirroredJobArgs = []queue.Job{
	plaidSyncJobArgs{},
	investmentHoldingsSyncJobArgs{},
	investmentTransactionsSyncJobArgs{},
	itemCleanupJobArgs{},
	effectiveCategoryRefreshJobArgs{},
	budgetHierarchyResyncJobArgs{},
	emailChangePropagationArgs{},
}

// TestMirroredJobArgs_CoverEveryDeclaredShape pins that the mirror list and
// queue.MirroredJobArgsShapes agree on the KIND SET, so a mirror added here without a table entry
// (or a table entry with no mirror) fails rather than sitting unpinned.
func TestMirroredJobArgs_CoverEveryDeclaredShape(t *testing.T) {
	declared := map[string]bool{}
	for k := range queue.MirroredJobArgsShapes {
		declared[k] = true
	}
	for _, args := range mirroredJobArgs {
		kind := args.Kind()
		assert.True(t, declared[kind],
			"mirror %T returns Kind %q, which has no entry in queue.MirroredJobArgsShapes — add one, "+
				"or this mirror can drift from jobs.%sArgs undetected", args, kind, kind)
		delete(declared, kind)
	}
	assert.Empty(t, declared,
		"queue.MirroredJobArgsShapes declares kinds with no mirror in this package: %v", declared)
}

// TestMirroredJobArgs_MatchShapeTable is the assertion that fails when a mirror's JSON tags drift
// from its jobs.*Args counterpart's. Today nothing else catches that: the two types are in packages
// that cannot see each other, so the compiler is silent and River only notices in production, where
// the WORKER's type is the one river_job.args is deserialized into and a renamed or dropped tag
// reads back as a zero value.
func TestMirroredJobArgs_MatchShapeTable(t *testing.T) {
	for _, args := range mirroredJobArgs {
		kind := args.Kind()
		t.Run(kind, func(t *testing.T) {
			shape, ok := queue.MirroredJobArgsShapes[kind]
			require.True(t, ok, "no shape declared for kind %q", kind)

			omit := map[string]bool{}
			for _, f := range shape.MirrorMayOmit {
				omit[f] = true
			}
			want := make([]string, 0, len(shape.Fields))
			for _, f := range shape.Fields {
				if !omit[f] {
					want = append(want, f)
				}
			}

			got, gotOmitEmptySet := queue.JSONFieldNames(args)
			assert.ElementsMatch(t, want, got,
				"mirror %T (kind %q) no longer carries the JSON field set declared in "+
					"queue.MirroredJobArgsShapes. Every field it fails to name is a field the worker "+
					"will read as a zero value; every field it names that the canonical type does not "+
					"is silently discarded", args, kind)

			// The mirror's omitempty set must be the canonical one restricted to the fields the
			// mirror actually carries — compared against the DECLARED list, never against the
			// mirror's own tags, which would be tautological (verified: with the expectation
			// derived from the mirror itself, adding omitempty to
			// emailChangePropagationArgs.ClerkUserID left this file green).
			var wantOmitEmpty []string
			for _, f := range shape.OmitEmpty {
				if !omit[f] {
					wantOmitEmpty = append(wantOmitEmpty, f)
				}
			}
			var gotOmitEmpty []string
			for f := range gotOmitEmptySet {
				gotOmitEmpty = append(gotOmitEmpty, f)
			}
			assert.ElementsMatch(t, wantOmitEmpty, gotOmitEmpty,
				"mirror %T (kind %q) does not have the omitempty tags declared in "+
					"queue.MirroredJobArgsShapes. An omitempty the canonical type lacks makes this "+
					"producer write a SHORTER document than the worker's type round-trips; one it has "+
					"and this mirror lacks makes it write a longer one. Either way the two producers "+
					"of this kind stop agreeing on the bytes UniqueOpts{ByArgs: true} keys on",
				args, kind)

			for _, f := range shape.MirrorMayOmit {
				assert.Contains(t, shape.OmitEmpty, f,
					"kind %q lets its mirror omit %q, but %q is not declared omitempty on the "+
						"canonical type, so omitting it is a wire-shape change", kind, f, f)
			}
		})
	}
}

// TestMirroredJobArgs_ZeroValueJSONMatchesDeclaredShape pins the mirrors' actual bytes. A mirror
// that carries the right field NAMES but tags one `omitempty` (or drops an omitempty its
// counterpart has) writes a different document to river_job.args than the worker's type would
// round-trip, which the field-name comparison above cannot see.
func TestMirroredJobArgs_ZeroValueJSONMatchesDeclaredShape(t *testing.T) {
	for _, args := range mirroredJobArgs {
		kind := args.Kind()
		t.Run(kind, func(t *testing.T) {
			b, err := json.Marshal(args)
			require.NoError(t, err)

			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(b, &decoded))

			shape := queue.MirroredJobArgsShapes[kind]
			omit := map[string]bool{}
			for _, f := range shape.MirrorMayOmit {
				omit[f] = true
			}
			declaredOmitEmpty := map[string]bool{}
			for _, f := range shape.OmitEmpty {
				declaredOmitEmpty[f] = true
			}

			var want []string
			for _, f := range shape.Fields {
				if omit[f] || declaredOmitEmpty[f] {
					continue
				}
				want = append(want, f)
			}
			got := make([]string, 0, len(decoded))
			for f := range decoded {
				got = append(got, f)
			}
			assert.ElementsMatch(t, want, got,
				"the zero value of mirror %T (kind %q) marshals to %s, which is not the document "+
					"queue.MirroredJobArgsShapes describes", args, kind, b)
		})
	}
}
