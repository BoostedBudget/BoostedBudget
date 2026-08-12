package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// This file lives in package jobs, not internal/services, because it is the only side that can see
// BOTH the real *UniqueOpts() functions and services.NewDeadLetterService: jobs imports services, so
// a services-side test can never reach the producers it must be compared against. (A test in package
// jobs importing services is not a cycle — the cycle would be the reverse.)

// stubDLQRepo satisfies the unexported repository interface services.NewDeadLetterService takes.
// Hand-written rather than testify-mocked: this test asserts on what reached the QUEUE, and the repo
// only has to not get in the way.
type stubDLQRepo struct {
	job         *domain.DeadLetterJob
	commitCount int
}

// stubRetryTx is the transaction handle the replay claims, enqueues and commits in (#1508). Only
// Commit and Rollback are implemented; anything else would panic on the nil embedded interface,
// which is what should happen if the replay ever tried to run SQL of its own through it.
type stubRetryTx struct {
	pgx.Tx
	repo *stubDLQRepo
}

func (s *stubRetryTx) Commit(context.Context) error {
	s.repo.commitCount++
	return nil
}

func (s *stubRetryTx) Rollback(context.Context) error { return nil }

func (s *stubDLQRepo) BeginRetryTx(_ context.Context) (pgx.Tx, error) {
	return &stubRetryTx{repo: s}, nil
}

func (s *stubDLQRepo) GetByID(_ context.Context, _ int64) (*domain.DeadLetterJob, error) {
	return s.job, nil
}

func (s *stubDLQRepo) ListAll(_ context.Context, _ domain.DeadLetterJobFilters, _, _ int) ([]*domain.DeadLetterJob, int, error) {
	return nil, 0, nil
}

func (s *stubDLQRepo) AtomicMarkRetriedInTx(_ context.Context, _ pgx.Tx, _, _ int64) (bool, error) {
	return true, nil
}

func (s *stubDLQRepo) MarkRetriedInTx(_ context.Context, _ pgx.Tx, _ int64, _ int64) error {
	return nil
}

func (s *stubDLQRepo) AddNotes(_ context.Context, _ int64, _ string) error { return nil }

func (s *stubDLQRepo) DeleteByID(_ context.Context, _ int64) error { return nil }

func (s *stubDLQRepo) CountByFilters(_ context.Context, _ domain.DeadLetterJobFilters) (int64, error) {
	return 0, nil
}

func (s *stubDLQRepo) DeleteByFilters(_ context.Context, _ domain.DeadLetterJobFilters, _ int64) (int64, error) {
	return 0, nil
}

func (s *stubDLQRepo) GetStats(_ context.Context) (*repository.DeadLetterStats, error) {
	return &repository.DeadLetterStats{}, nil
}

func (s *stubDLQRepo) GetStatsByKind(_ context.Context) (map[string]int64, error) { return nil, nil }

func (s *stubDLQRepo) DeleteOlderThan(_ context.Context, _ time.Time, _ bool) (int64, error) {
	return 0, nil
}

// riverUniqueKeyInputs captures the PER-INSERT inputs river derives a job's unique_key from.
//
// dbunique.buildUniqueKeyString (river v0.43.0, internal/dbunique/db_unique.go:58-126) concatenates,
// in order:
//
//	"&kind="   + params.Kind
//	"&args="   + EncodedArgs re-serialised with its TOP-LEVEL keys sorted — only when ByArgs is set,
//	             and only in this form because no args type in this repo carries `unique:""` struct
//	             tags (verified: `grep -rn 'unique:"' --include='*.go' internal cmd` finds none)
//	"&period=" + ScheduledAt-or-now truncated to ByPeriod — only when ByPeriod != 0
//	"&queue="  + params.Queue — only when ByQueue is set
//
// and sha256s the result. ByState does not enter the key; it selects which existing rows the key is
// compared against.
//
// Two of those five are NOT captured here, deliberately:
//
//   - The period bucket depends on insert TIME, so a replay is in the original's bucket only if it
//     happens inside the same window. That is not a defect to be fixed — a replay minutes or days
//     later SHOULD be allowed through rather than being deduplicated against a long-finished run —
//     but it does mean this test proves key equality for the kind/args/queue/options inputs only, and
//     says nothing about a ByPeriod kind replayed across a boundary.
//   - No registered kind currently sets ByQueue (nothing in this repo's *UniqueOpts() does), so
//     `queue` never reaches the key today. It is compared anyway, because the replay takes its queue
//     from the stored row and that is the value that would be hashed the moment a kind sets ByQueue.
//
// Comparing the finished hash is not possible from here: dbunique is an internal package of river.
type riverUniqueKeyInputs struct {
	kind       string
	sortedArgs string
	queueName  string
	unique     *queue.UniqueOpts
}

// sortedArgsJSON re-serialises args' JSON with top-level keys sorted, mirroring what
// buildUniqueKeyString does to EncodedArgs (encoding/json emits map keys in sorted order).
func sortedArgsJSON(t *testing.T, args queue.Job) string {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	sorted, err := json.Marshal(fields)
	require.NoError(t, err)
	return string(sorted)
}

// originalInsert describes what a kind's real producer hands River. The uniqueness options and queue
// are supplied by the caller rather than discovered: this helper cannot invoke the producers (they
// live in packages this one cannot import), which is the limitation recorded on
// TestUniqueOptsForKind_MapsEachKindToItsOwnProducerOptions. What this side of the comparison is
// therefore worth: it fixes the args ENCODING as the producer's own `json.Marshal(args)`, which is the
// half a replay could plausibly get wrong.
func originalInsert(t *testing.T, args queue.Job, queueName string, unique *queue.UniqueOpts) riverUniqueKeyInputs {
	t.Helper()
	return riverUniqueKeyInputs{
		kind:       args.Kind(),
		sortedArgs: sortedArgsJSON(t, args),
		queueName:  queueName,
		unique:     unique,
	}
}

// replayInsert drives the REAL dead-letter replay path — services.deadLetterService.Retry wired with
// jobs.UniqueOptsForKind exactly as cmd/server/main.go wires it — over a dead_letter_jobs row
// carrying the args River stored for the original insert, and reports what reached the queue.
func replayInsert(t *testing.T, args queue.Job, storedQueue string) riverUniqueKeyInputs {
	t.Helper()
	storedArgs, err := json.Marshal(args)
	require.NoError(t, err)
	return replayInsertWithStoredArgs(t, args.Kind(), storedArgs, storedQueue)
}

// replayInsertWithStoredArgs is replayInsert with the dead_letter_jobs.args bytes supplied directly,
// for the case where those bytes are NOT simply json.Marshal of the args type (a JSONB round trip).
func replayInsertWithStoredArgs(t *testing.T, kind string, storedArgs []byte, storedQueue string) riverUniqueKeyInputs {
	t.Helper()

	repo := &stubDLQRepo{job: &domain.DeadLetterJob{
		ID:          1,
		Kind:        kind,
		Queue:       storedQueue,
		Args:        storedArgs,
		MaxAttempts: 5,
	}}
	mockQueue := &MockJobQueue{}
	mockQueue.On("EnqueueTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&queue.JobResult{ID: 4242}, nil)

	svc := services.NewDeadLetterService(repo, mockQueue, UniqueOptsForKind)
	resp, err := svc.Retry(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, resp.Success)
	require.Equal(t, 1, repo.commitCount, "a successful replay must commit its claim and its insert together")

	require.Len(t, mockQueue.Calls, 1)
	enqueuedJob, ok := mockQueue.Calls[0].Arguments.Get(2).(queue.Job)
	require.True(t, ok)
	opts, ok := mockQueue.Calls[0].Arguments.Get(3).([]queue.EnqueueOption)
	require.True(t, ok, "MockJobQueue.EnqueueTx's fourth mock argument must be the option slice")

	resolved := queue.ApplyOptions(opts)
	return riverUniqueKeyInputs{
		kind:       enqueuedJob.Kind(),
		sortedArgs: sortedArgsJSON(t, enqueuedJob),
		queueName:  resolved.Queue,
		unique:     resolved.Unique,
	}
}

// TestDeadLetterReplay_ProducesSameUniqueKeyInputsAsOriginalProducer is #1492's requirement stated as
// the thing that actually matters: a replayed job must deduplicate against a live job of its kind,
// which happens only if River derives the same unique_key for both. Before the fix the replay passed
// no uniqueness options at all, so its unique field was nil for every kind here — no key, no dedup.
func TestDeadLetterReplay_ProducesSameUniqueKeyInputsAsOriginalProducer(t *testing.T) {
	tests := []struct {
		name   string
		args   queue.Job
		queue  string
		unique *queue.UniqueOpts
	}{
		{
			// ByArgs with no ByPeriod: the args JSON is the whole key, so this is the case where a
			// replay MUST be byte-comparable to the original insert.
			name:   "account_deletion",
			args:   AccountDeletionArgs{UserID: "11111111-1111-1111-1111-111111111111"},
			queue:  queue.QueueDefault,
			unique: AccountDeletionUniqueOpts(),
		},
		{
			name:   "plaid_sync",
			args:   PlaidSyncArgs{UserID: "u1", ItemID: "i1", WebhookID: "w1"},
			queue:  queue.QueueDefault,
			unique: PlaidSyncUniqueOpts(),
		},
		{
			name:   "item_cleanup",
			args:   ItemCleanupArgs{LinkedItemID: "li1", UserID: "u1", Reason: "revoked"},
			queue:  queue.QueueLow,
			unique: ItemCleanupUniqueOpts(),
		},
		{
			// ByPeriod + a ByState that includes Completed: replaying inside the window is meant to
			// be skipped rather than duplicate a daily scan. That only holds if the options carry.
			name:   "retention_cleanup_check",
			args:   RetentionCleanupCheckArgs{TriggerSource: "periodic"},
			queue:  queue.QueueLow,
			unique: RetentionCleanupCheckUniqueOpts(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := originalInsert(t, tc.args, tc.queue, tc.unique)
			got := replayInsert(t, tc.args, tc.queue)

			require.NotNil(t, got.unique,
				"the replay carried no uniqueness options, so River would assign it no unique_key and it could not deduplicate against a live %s job (#1492)",
				tc.name)
			assert.Equal(t, want, got,
				"the replay and the original producer must agree on kind, sorted args JSON, and UniqueOpts — the three inputs river hashes into unique_key")
		})
	}
}

// TestDeadLetterReplay_NestedObjectsSurviveOnlyAtTheTopLevel demonstrates the mechanism that
// TestArgsTypes_NoByArgsKindHasANestedArgsField exists to keep out of reach, so that test's rationale
// is measured rather than asserted.
//
// A replay's args are read back from dead_letter_jobs.args, a JSONB column, so they are Postgres'
// re-serialisation of what the producer wrote — keys ordered by length-then-bytes, a space after each
// colon — not the producer's own json.Marshal output. river's ByArgs key derivation re-sorts the
// TOP-LEVEL keys, which absorbs that difference; it copies each value's raw JSON text through
// verbatim, which does not.
//
// The two halves below are what make the flat-args kinds safe and a nested-args kind unsafe. Both use
// the same helper the parity test above uses, so if that helper ever gained deep canonicalisation this
// would go red and the co-occurrence ban could be lifted.
func TestDeadLetterReplay_NestedObjectsSurviveOnlyAtTheTopLevel(t *testing.T) {
	// Same logical object, in the two encodings. json.Marshal of a Go map sorts keys lexicographically
	// with no spaces; '{"a":1,"zz":2,"percent_used":3}'::jsonb::text on PostgreSQL 17 returns
	// `{"a": 1, "zz": 2, "percent_used": 3}` (measured).
	goEncoding := func(inner string) queue.Job {
		return queue.RawJobArgs{JobKind: "probe", RawArgs: []byte(`{"b":"x","event_data":` + inner + `}`)}
	}

	t.Run("top-level key order and spacing do not matter", func(t *testing.T) {
		marshalled := queue.RawJobArgs{JobKind: "probe", RawArgs: []byte(`{"a":1,"zz":2}`)}
		jsonbText := queue.RawJobArgs{JobKind: "probe", RawArgs: []byte(`{"zz": 2, "a": 1}`)}
		assert.Equal(t, sortedArgsJSON(t, marshalled), sortedArgsJSON(t, jsonbText),
			"river re-sorts top-level keys, which is why every flat-args kind replays to the same unique_key")
	})

	t.Run("nested key order and spacing do matter", func(t *testing.T) {
		marshalled := goEncoding(`{"a":1,"percent_used":3,"zz":2}`)
		jsonbText := goEncoding(`{"a": 1, "zz": 2, "percent_used": 3}`)
		assert.NotEqual(t, sortedArgsJSON(t, marshalled), sortedArgsJSON(t, jsonbText),
			"if these are now equal, nested objects have become canonical somewhere and TestArgsTypes_NoByArgsKindHasANestedArgsField's ByArgs ban can be relaxed")
	})
}
