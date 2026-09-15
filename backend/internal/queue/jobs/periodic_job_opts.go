// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// riverUniqueOpts converts this package's queue-runner-agnostic queue.UniqueOpts to the
// river.UniqueOpts type river.InsertOpts.UniqueOpts actually requires — the same conversion
// internal/queue/river/client.go's toRiverUniqueOpts performs for the general-purpose enqueue
// path. RetentionCleanupCheckInsertOpts (Issue #1413) hand-copied this loop; this shared helper is
// what every other periodic job's own *InsertOpts() function (Issue #1448) calls instead of
// hand-copying it a further ~10 times.
//
// ByState is only allocated when unique.ByState is non-empty — mirroring
// internal/queue/river/client.go's toRiverUniqueOpts guard exactly. Without this guard,
// make([]rivertype.JobState, 0) on a job whose queue.UniqueOpts leaves ByState unset (nil)
// would produce a non-nil, zero-length slice instead of preserving nil. River's own
// UniqueOpts.isEmpty() (insert_opts.go) treats `ByState == nil` and a non-nil empty slice as
// two DIFFERENT things when deciding whether this InsertOpts' UniqueOpts should fall back to
// the job's own default UniqueOpts (JobArgsWithInsertOpts) — so preserving nil here, not an
// empty slice, is required for that fallback path to trigger correctly on some future job
// whose UniqueOpts has ByState unset AND every other field at its zero value (none of the 14
// periodic jobs' current *UniqueOpts() helpers hit this case today — every one of them sets a
// non-zero ByPeriod or a non-empty ByState — but a future job easily could).
func riverUniqueOpts(unique *queue.UniqueOpts) river.UniqueOpts {
	var states []rivertype.JobState
	if len(unique.ByState) > 0 {
		states = make([]rivertype.JobState, len(unique.ByState))
		for i, s := range unique.ByState {
			states[i] = rivertype.JobState(s)
		}
	}
	return river.UniqueOpts{
		ByArgs:   unique.ByArgs,
		ByPeriod: unique.ByPeriod,
		ByQueue:  unique.ByQueue,
		ByState:  states,
	}
}

// riverInsertOptsFromUnique builds the *river.InsertOpts a periodic job's main.go
// registration must use: the given queue name plus the river-converted form of unique.
// Shared by every *InsertOpts() helper introduced for Issue #1448.
func riverInsertOptsFromUnique(queueName string, unique *queue.UniqueOpts) *river.InsertOpts {
	return &river.InsertOpts{
		Queue:      queueName,
		UniqueOpts: riverUniqueOpts(unique),
	}
}
