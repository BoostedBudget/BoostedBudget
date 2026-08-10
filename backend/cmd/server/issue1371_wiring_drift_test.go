package main

// TestIssue1371HouseholdCheckUniqueOptsWiringNoDrift guards against the exact bug class
// Round 8 (gap-analysis on issue #1371) found: HouseholdOverCapacityCheckUniqueOpts() and
// HouseholdMemberCleanupCheckUniqueOpts() (household_over_capacity_check.go /
// household_member_cleanup_check.go) are exported, documented "source of truth" helper
// functions describing the UniqueOpts these two periodic jobs must carry — but at the time
// this test was first written, main.go hand-duplicated the literal instead of calling them,
// so nothing enforced the two stay in sync. Round 8 found they had silently drifted to dead
// code (never wired at all); after wiring them in, a further Round 8 finding (Fix D) required
// removing ByPeriod entirely from both, and nothing would catch a future accidental
// reintroduction of a ByPeriod window, or a dropped ByState value.
//
// Issue #1448 rewrite: the ORIGINAL version of this test scanned main.go as text between each
// job's `Args{` literal and a hardcoded `&river.PeriodicJobOpts{RunOnStart: false},` end
// marker. That marker broke the moment Issue #1448 flipped these two jobs' RunOnStart to
// true (the fix this phase makes) — and could never have survived Issue #1448's
// buildPeriodicJobSpecs extraction (periodic_jobs.go) moving the registration out of main.go
// into its own file regardless. Text-scanning main.go was never actually necessary to test
// the real invariant — "the periodic registration's InsertOpts carries exactly the UniqueOpts
// its own *UniqueOpts() helper documents" — so this rewrite calls buildPeriodicJobSpecs
// directly and asserts the ACTUAL VALUES the periodic registration ships, field-for-field,
// against the real *UniqueOpts() helpers. A future change to either helper's ByArgs/ByState is
// what this test checks the real wiring against, not a second hardcoded copy (or a fragile
// text anchor) that could itself silently drift.
import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssue1371HouseholdCheckUniqueOptsWiringNoDrift(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	cases := []struct {
		jobName  string
		specName string
		wantOpts *queue.UniqueOpts
	}{
		{
			jobName:  "household member cleanup check",
			specName: "household member cleanup check",
			wantOpts: jobs.HouseholdMemberCleanupCheckUniqueOpts(),
		},
		{
			jobName:  "household over-capacity check",
			specName: "household over-capacity check",
			wantOpts: jobs.HouseholdOverCapacityCheckUniqueOpts(),
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.jobName, func(t *testing.T) {
			var found *periodicJobSpec
			for i := range specs {
				if specs[i].name == c.specName {
					found = &specs[i]
					break
				}
			}
			require.NotNilf(t, found, "no periodic job spec named %q returned by buildPeriodicJobSpecs — "+
				"did the job get renamed or removed?", c.specName)

			_, insertOpts := found.build()
			require.NotNil(t, insertOpts, "%s: build() returned nil InsertOpts", c.jobName)

			assert.Equal(t, c.wantOpts.ByArgs, insertOpts.UniqueOpts.ByArgs,
				"%s: ByArgs must match its *UniqueOpts() helper", c.jobName)

			// Issue #1371 Round 8 Fix D: a time-bucketed ByPeriod window lets a still-running
			// instance bridge into the next bucket and duplicate-run — global ByState-only
			// uniqueness is required instead. Assert both the helper itself and the wired
			// InsertOpts stay this way.
			assert.Zero(t, c.wantOpts.ByPeriod,
				"%s: its own *UniqueOpts() helper must not set ByPeriod (Issue #1371 Round 8 Fix D) — "+
					"test setup bug, not a wiring issue", c.jobName)
			assert.Zero(t, insertOpts.UniqueOpts.ByPeriod,
				"%s: periodic job registration must NOT carry ByPeriod (Issue #1371 Round 8 Fix D)", c.jobName)

			// ByQueue's zero value (false) means "global uniqueness across queues" — the
			// desired behavior. Only an explicit true is ever wrong.
			assert.False(t, insertOpts.UniqueOpts.ByQueue,
				"%s: periodic job registration must NOT set ByQueue: true — uniqueness must stay "+
					"global across queues, matching its *UniqueOpts() helper", c.jobName)

			require.NotEmpty(t, c.wantOpts.ByState,
				"%s: *UniqueOpts() helper itself returned an empty ByState — test setup bug, not a "+
					"main.go wiring issue", c.jobName)

			// Exact count (not just per-state presence) catches an EXTRA state slipping into the
			// wired InsertOpts that the helper doesn't have, not just a missing one.
			require.Lenf(t, insertOpts.UniqueOpts.ByState, len(c.wantOpts.ByState),
				"%s: ByState lists %d entries but its *UniqueOpts() helper returns %d (%v) — an extra "+
					"or missing entry changes which job states are deduplicated against",
				c.jobName, len(insertOpts.UniqueOpts.ByState), len(c.wantOpts.ByState), c.wantOpts.ByState)
			for i, wantState := range c.wantOpts.ByState {
				assert.Equal(t, string(wantState), string(insertOpts.UniqueOpts.ByState[i]),
					"%s: ByState[%d] must match its *UniqueOpts() helper", c.jobName, i)
			}
		})
	}
}
