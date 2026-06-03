package queue

import "encoding/json"

// RawJobArgs is a generic job args type that wraps a raw JSON payload.
// It is used when re-enqueueing dead letter jobs for retry — the original
// job's kind and args are preserved without needing to know the concrete type.
//
// River routes jobs by Kind() string and deserializes args from JSON, so this
// works as long as the original worker is registered for the given kind and the
// stored JSON is compatible with that worker's args schema.
type RawJobArgs struct {
	// JobKind is the River job kind string (e.g. "plaid_sync").
	JobKind string `json:"-"`
	// RawArgs is the original serialized job arguments from the dead_letter_jobs table.
	RawArgs json.RawMessage `json:"-"`
}

// Kind implements river.JobArgs — returns the job type identifier used for worker routing.
func (r RawJobArgs) Kind() string { return r.JobKind }

// MarshalJSON returns the raw args directly so River stores them in the river_job.args
// column in the same format the original worker expects for deserialization.
func (r RawJobArgs) MarshalJSON() ([]byte, error) {
	if len(r.RawArgs) == 0 {
		return []byte("{}"), nil
	}
	return r.RawArgs, nil
}
