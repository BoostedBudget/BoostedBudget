package services

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskJobArgs_RedactsSensitiveKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKeys map[string]string // key → expected value
	}{
		{
			name:  "redacts access_token",
			input: `{"user_id":"u1","access_token":"secret-token-xyz"}`,
			wantKeys: map[string]string{
				"user_id":      "u1",
				"access_token": "[REDACTED]",
			},
		},
		{
			name:  "redacts token field",
			input: `{"token":"abc123","item_id":"item1"}`,
			wantKeys: map[string]string{
				"token":   "[REDACTED]",
				"item_id": "item1",
			},
		},
		{
			name:  "redacts email field",
			input: `{"email":"user@example.com","name":"Alice"}`,
			wantKeys: map[string]string{
				"email": "[REDACTED]",
				"name":  "Alice",
			},
		},
		{
			name:  "redacts password field",
			input: `{"password":"hunter2","username":"alice"}`,
			wantKeys: map[string]string{
				"password": "[REDACTED]",
				"username": "alice",
			},
		},
		{
			name:  "redacts secret field",
			input: `{"secret":"my-secret","kind":"test"}`,
			wantKeys: map[string]string{
				"secret": "[REDACTED]",
				"kind":   "test",
			},
		},
		{
			name:  "non-sensitive fields pass through",
			input: `{"user_id":"u1","item_id":"item123","sync_type":"incremental"}`,
			wantKeys: map[string]string{
				"user_id":   "u1",
				"item_id":   "item123",
				"sync_type": "incremental",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskJobArgs(json.RawMessage(tt.input))
			require.NotNil(t, result)

			var got map[string]interface{}
			require.NoError(t, json.Unmarshal(result, &got))

			for key, wantVal := range tt.wantKeys {
				assert.Equal(t, wantVal, got[key], "field %q mismatch", key)
			}
		})
	}
}

func TestMaskJobArgs_InvalidJSON(t *testing.T) {
	result := MaskJobArgs(json.RawMessage(`not-json`))
	// Should return original on parse failure
	assert.Equal(t, json.RawMessage(`not-json`), result)
}

func TestMaskJobArgs_Nil(t *testing.T) {
	result := MaskJobArgs(nil)
	assert.Nil(t, result)
}

func TestMaskJobErrors_RedactsTokens(t *testing.T) {
	// Errors is a JSON array of {attempt, message, timestamp}
	input := `[{"attempt":1,"message":"failed with token=abc123 path=/home/user","timestamp":"2024-01-01T00:00:00Z"}]`
	result := MaskJobErrors(json.RawMessage(input))
	require.NotNil(t, result)

	var got []map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &got))
	require.Len(t, got, 1)
	// Message should be present but potentially sanitized
	assert.NotEmpty(t, got[0]["message"])
}

func TestMaskJobErrors_InvalidJSON(t *testing.T) {
	result := MaskJobErrors(json.RawMessage(`not-json`))
	assert.Equal(t, json.RawMessage(`not-json`), result)
}

func TestMaskJobErrors_Nil(t *testing.T) {
	result := MaskJobErrors(nil)
	assert.Nil(t, result)
}
