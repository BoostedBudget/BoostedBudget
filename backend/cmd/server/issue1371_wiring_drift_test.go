package main

// TestIssue1371HouseholdCheckUniqueOptsWiringNoDrift guards against the exact bug
// class Round 8 (gap-analysis on issue #1371) found: HouseholdOverCapacityCheckUniqueOpts()
// and HouseholdMemberCleanupCheckUniqueOpts() (household_over_capacity_check.go /
// household_member_cleanup_check.go) are exported, documented "source of truth" helper
// functions describing the UniqueOpts these two periodic jobs must carry — but main.go
// hand-duplicates the literal instead of calling them (matching this file's existing
// direct-construction style for other periodic jobs), so nothing enforces the two stay
// in sync. Round 8 found they had silently drifted to dead code (never wired at all);
// after wiring them in, a further Round 8 finding (Fix D) required removing ByPeriod
// entirely from both, and NOTHING would catch a future accidental reintroduction of a
// ByPeriod window, or a dropped ByState value, in main.go alone.
//
// This test scans main.go as text (same technique as issue972_wiring_drift_test.go), but
// — per a follow-up Codex finding that the first version of this test only checked a
// hardcoded literal and never asserted ByArgs — derives its expectations by actually
// CALLING the two exported helper functions, so a future change to either helper's
// ByArgs/ByState is what this test checks main.go against, not a second hardcoded copy
// that could itself silently drift from the helpers.
import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
)

// jobStateIdent maps this repo's internal/queue.JobState values to the corresponding
// github.com/riverqueue/river/rivertype.JobState* Go identifier used in main.go's
// literal construction (queue.JobState and rivertype.JobState are distinct string types
// with identical underlying string values, but main.go's source text contains the
// identifier name, not the string value — so this maps one to the other for the text scan).
var jobStateIdent = map[queue.JobState]string{
	queue.JobStateAvailable: "JobStateAvailable",
	queue.JobStateScheduled: "JobStateScheduled",
	queue.JobStateRunning:   "JobStateRunning",
	queue.JobStateRetryable: "JobStateRetryable",
	queue.JobStatePending:   "JobStatePending",
}

func TestIssue1371HouseholdCheckUniqueOptsWiringNoDrift(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	cases := []struct {
		jobName     string
		startMarker string
		wantOpts    *queue.UniqueOpts
	}{
		{
			"household member cleanup check",
			"HouseholdMemberCleanupCheckArgs{",
			jobs.HouseholdMemberCleanupCheckUniqueOpts(),
		},
		{
			"household over-capacity check",
			"HouseholdOverCapacityCheckArgs{",
			jobs.HouseholdOverCapacityCheckUniqueOpts(),
		},
	}
	const endMarker = "&river.PeriodicJobOpts{RunOnStart: false},"

	for _, c := range cases {
		startIdx := strings.Index(src, c.startMarker)
		if startIdx == -1 {
			t.Fatalf("%s: could not find start marker %q in main.go", c.jobName, c.startMarker)
		}
		rest := src[startIdx:]
		endIdx := strings.Index(rest, endMarker)
		if endIdx == -1 {
			t.Fatalf("%s: could not find end marker %q after start marker in main.go", c.jobName, endMarker)
		}
		block := rest[:endIdx]

		if !strings.Contains(block, "UniqueOpts: river.UniqueOpts{") {
			t.Errorf("%s: periodic job registration must wire UniqueOpts, or the corresponding "+
				"*UniqueOpts() helper function's documented uniqueness guarantee silently no-ops "+
				"in production (Issue #1371 Round 8 Finding 1)", c.jobName)
			continue
		}

		// Regexp (not a fixed-spacing string match) tolerates gofmt re-aligning the
		// column width of "ByArgs:" if a sibling scalar field (e.g. ByQueue) is ever
		// added explicitly nearby — a fixed-width match would false-fail on a
		// semantically-correct, merely differently-formatted literal.
		byArgsPattern := regexp.MustCompile(fmt.Sprintf(`ByArgs:\s*%v,`, c.wantOpts.ByArgs))
		if !byArgsPattern.MatchString(block) {
			t.Errorf("%s: periodic job registration's ByArgs must match its *UniqueOpts() helper "+
				"function (want to match %q in the block)", c.jobName, byArgsPattern.String())
		}

		// ByQueue's zero value (false) means "global uniqueness across queues" — the
		// desired behavior here, whether achieved by omitting the field (this
		// codebase's convention for a zero-value field, as ByPeriod already
		// demonstrates) or by an explicit "ByQueue: false,". Only an explicit `true`
		// is ever wrong, so assert its absence rather than requiring a specific
		// literal spelling.
		if !c.wantOpts.ByQueue && regexp.MustCompile(`ByQueue:\s*true`).MatchString(block) {
			t.Errorf("%s: periodic job registration must NOT set ByQueue: true — uniqueness must "+
				"stay global across queues, matching its *UniqueOpts() helper function", c.jobName)
		}

		if strings.Contains(block, "ByPeriod:") {
			t.Errorf("%s: periodic job registration must NOT set ByPeriod — a time-bucketed window "+
				"lets a still-running instance bridge into the next bucket and duplicate-run "+
				"(Issue #1371 Round 8 Fix D); global ByState-only uniqueness is required instead",
				c.jobName)
		}

		if len(c.wantOpts.ByState) == 0 {
			t.Fatalf("%s: *UniqueOpts() helper itself returned an empty ByState — test setup bug, "+
				"not a main.go wiring issue", c.jobName)
		}
		for _, state := range c.wantOpts.ByState {
			ident, ok := jobStateIdent[state]
			if !ok {
				t.Fatalf("%s: *UniqueOpts() helper returned queue.JobState %q with no known "+
					"rivertype identifier mapping in this test's jobStateIdent — update the map",
					c.jobName, state)
			}
			if !strings.Contains(block, ident) {
				t.Errorf("%s: periodic job registration's ByState must include rivertype.%s "+
					"to match its *UniqueOpts() helper function (which returns %v)",
					c.jobName, ident, c.wantOpts.ByState)
			}
		}

		// Exact count (not just per-state presence) catches an EXTRA state slipping into
		// main.go's ByState (e.g. rivertype.JobStateCompleted, a real River constant) that
		// the helper function doesn't have — a per-state Contains loop alone would miss
		// this, since it only checks the helper's states are present, never that nothing
		// else is (Round 8 Codex finding).
		const byStateOpen = "ByState: []rivertype.JobState{"
		openIdx := strings.Index(block, byStateOpen)
		if openIdx == -1 {
			t.Fatalf("%s: could not find %q in main.go's UniqueOpts block", c.jobName, byStateOpen)
		}
		afterOpen := block[openIdx+len(byStateOpen):]
		closeIdx := strings.Index(afterOpen, "},")
		if closeIdx == -1 {
			t.Fatalf("%s: could not find ByState's closing \"},\" in main.go's UniqueOpts block", c.jobName)
		}
		gotCount := strings.Count(afterOpen[:closeIdx], "rivertype.JobState")
		if gotCount != len(c.wantOpts.ByState) {
			t.Errorf("%s: main.go's ByState lists %d rivertype.JobState entries but its "+
				"*UniqueOpts() helper function returns %d (%v) — an extra or missing entry "+
				"changes which job states are deduplicated against", c.jobName, gotCount,
				len(c.wantOpts.ByState), c.wantOpts.ByState)
		}
	}
}
