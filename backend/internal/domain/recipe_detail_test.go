package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPRecipeCallDetailJSON(t *testing.T) {
	for _, detail := range []string{"", ",\"detail\":null", ",\"detail\":\"compact\"", ",\"detail\":\"full\""} {
		t.Run(detail, func(t *testing.T) {
			var call MCPRecipeCall
			require.NoError(t, json.Unmarshal([]byte(`{"operation_id":"getAccounts"`+detail+`}`), &call))
			encoded, err := json.Marshal(call)
			require.NoError(t, err)
			expected := detail
			if detail == ",\"detail\":null" {
				expected = ""
			}
			require.JSONEq(t, `{"operation_id":"getAccounts"`+expected+`}`, string(encoded))
		})
	}
}
