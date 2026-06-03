package queue

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawJobArgs_Kind(t *testing.T) {
	args := RawJobArgs{
		JobKind: "plaid_sync",
		RawArgs: json.RawMessage(`{"user_id":"u1"}`),
	}
	assert.Equal(t, "plaid_sync", args.Kind())
}

func TestRawJobArgs_MarshalJSON_ReturnsRawArgs(t *testing.T) {
	raw := json.RawMessage(`{"user_id":"u1","item_id":"item123"}`)
	args := RawJobArgs{
		JobKind: "plaid_sync",
		RawArgs: raw,
	}

	marshaled, err := json.Marshal(args)
	require.NoError(t, err)
	// Should marshal to the raw args directly, not wrap the struct
	assert.JSONEq(t, `{"user_id":"u1","item_id":"item123"}`, string(marshaled))
}

func TestRawJobArgs_MarshalJSON_EmptyArgs(t *testing.T) {
	args := RawJobArgs{
		JobKind: "some_job",
		RawArgs: json.RawMessage(`{}`),
	}

	marshaled, err := json.Marshal(args)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(marshaled))
}
