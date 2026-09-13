package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBudgetPlanUnknownActualsRemainJSONNull(t *testing.T) {
	data, err := json.Marshal(BudgetPlanLine{ID: "retained", Kind: "savings", Planned: 100})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Contains(t, fields, "actual")
	require.Nil(t, fields["actual"])
	require.Contains(t, fields, "variance")
	require.Nil(t, fields["variance"])
	require.Equal(t, 100.0, fields["planned"])
}
