package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordFinancialDataSyncCompletionSuppressionUsesBoundedReasons(t *testing.T) {
	for _, reason := range []FinancialDataSyncCompletionSuppressionReason{
		FinancialDataSyncCompletionSuppressionCutoverUnset,
		FinancialDataSyncCompletionSuppressionPreCutover,
		FinancialDataSyncCompletionSuppressionLegacyAmbiguous,
		FinancialDataSyncCompletionSuppressionUnknown,
	} {
		before := testutil.ToFloat64(financialDataSyncCompletionSuppressionsTotal.WithLabelValues(string(reason)))
		RecordFinancialDataSyncCompletionSuppression(reason)
		after := testutil.ToFloat64(financialDataSyncCompletionSuppressionsTotal.WithLabelValues(string(reason)))
		assert.Equal(t, before+1, after)
	}

	beforeUnknown := testutil.ToFloat64(financialDataSyncCompletionSuppressionsTotal.WithLabelValues("unknown"))
	RecordFinancialDataSyncCompletionSuppression("invented-unbounded-reason")
	afterUnknown := testutil.ToFloat64(financialDataSyncCompletionSuppressionsTotal.WithLabelValues("unknown"))
	assert.Equal(t, beforeUnknown+1, afterUnknown, "unexpected inputs must collapse to the bounded unknown series")
}
