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
// path. RetentionCleanupCheckInsertOpts and ManualTierOverrideExpiryCheckInsertOpts
// (Issue #1413) each hand-copied this loop independently; this shared helper is what every
// other periodic job's own *InsertOpts() function (Issue #1448) calls instead of hand-copying
// it a further ~10 times.
func riverUniqueOpts(unique *queue.UniqueOpts) river.UniqueOpts {
	states := make([]rivertype.JobState, len(unique.ByState))
	for i, s := range unique.ByState {
		states[i] = rivertype.JobState(s)
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
