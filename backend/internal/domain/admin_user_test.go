package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdminUserListItem_EffectiveStatus_AlwaysSerializes proves EffectiveStatus has no
// `omitempty` — the JSON key must always be present, even when the value is the empty string,
// so the frontend never has to distinguish "empty" from "absent" (Issue #1413).
func TestAdminUserListItem_EffectiveStatus_AlwaysSerializes(t *testing.T) {
	item := AdminUserListItem{
		ID:              "user-1",
		EffectiveStatus: "", // deliberately empty/unset
	}

	data, err := json.Marshal(item)
	require.NoError(t, err)

	var asMap map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &asMap))

	value, present := asMap["effective_status"]
	assert.True(t, present, "effective_status key must always be present in the JSON output")
	assert.Equal(t, "", value)
}

// TestAdminUserListItem_DeletionFields_OmittedWhenNil proves DeletionRequestedAt/
// DeletionScheduledAt keep `omitempty` — absent means no pending deletion, unlike
// EffectiveStatus which always serializes.
func TestAdminUserListItem_DeletionFields_OmittedWhenNil(t *testing.T) {
	item := AdminUserListItem{ID: "user-1"}

	data, err := json.Marshal(item)
	require.NoError(t, err)

	var asMap map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &asMap))

	_, hasRequested := asMap["deletion_requested_at"]
	_, hasScheduled := asMap["deletion_scheduled_at"]
	assert.False(t, hasRequested, "deletion_requested_at must be omitted when nil")
	assert.False(t, hasScheduled, "deletion_scheduled_at must be omitted when nil")
}
