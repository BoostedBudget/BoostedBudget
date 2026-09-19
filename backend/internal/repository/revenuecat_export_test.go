package repository

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRevenueCatExportIncludesOwnOperationalAndBillingHistory(t *testing.T) {
	tables := map[string]DataExportTable{}
	for _, table := range DataExportTables {
		tables[table.Table] = table
	}
	for _, name := range []string{"revenuecat_customers", "revenuecat_operations", "revenuecat_webhooks", "revenuecat_checkout_intents"} {
		table, ok := tables[name]
		require.True(t, ok, name)
		require.Equal(t, "t.user_id = $1", table.OwnershipPredicate)
	}
	require.Contains(t, tables["revenuecat_operations"].ExcludedColumns, "actor_id", "another data subject's administrator identity is withheld")
}
