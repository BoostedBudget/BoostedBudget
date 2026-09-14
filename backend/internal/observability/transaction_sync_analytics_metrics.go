package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type FinancialDataSyncCompletionSuppressionReason string

const (
	FinancialDataSyncCompletionSuppressionCutoverUnset    FinancialDataSyncCompletionSuppressionReason = "cutover_unset"
	FinancialDataSyncCompletionSuppressionPreCutover      FinancialDataSyncCompletionSuppressionReason = "pre_cutover"
	FinancialDataSyncCompletionSuppressionLegacyAmbiguous FinancialDataSyncCompletionSuppressionReason = "legacy_ambiguous"
	FinancialDataSyncCompletionSuppressionUnknown         FinancialDataSyncCompletionSuppressionReason = "unknown"
)

var financialDataSyncCompletionSuppressionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "financial_data_sync_completion_suppressions_total",
	Help: "Fully completed financial-data sync analytics events suppressed by bounded rollout reason.",
}, []string{"reason"})

func RecordFinancialDataSyncCompletionSuppression(reason FinancialDataSyncCompletionSuppressionReason) {
	switch reason {
	case FinancialDataSyncCompletionSuppressionCutoverUnset,
		FinancialDataSyncCompletionSuppressionPreCutover,
		FinancialDataSyncCompletionSuppressionLegacyAmbiguous,
		FinancialDataSyncCompletionSuppressionUnknown:
	default:
		reason = FinancialDataSyncCompletionSuppressionUnknown
	}
	financialDataSyncCompletionSuppressionsTotal.WithLabelValues(string(reason)).Inc()
}
