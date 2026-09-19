package services

import (
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

// A missing injection would silently restore the legacy eligibility decision.
func TestRevenueCatRetentionGuardProductionWiring(t *testing.T) {
	main, err := os.ReadFile("../../cmd/server/main.go")
	require.NoError(t, err)
	for _, call := range []string{"deletionService.SetSubscriptionCleanupGuard(revenueCatCleanupGuard)", "retentionCleanupCheckWorker.SetSubscriptionCleanupGuard(revenueCatCleanupGuard)"} {
		require.True(t, strings.Contains(string(main), call), call)
	}
	worker, err := os.ReadFile("../queue/jobs/retention_cleanup_check.go")
	require.NoError(t, err)
	require.Contains(t, string(worker), "w.subscriptionCleanupGuard.CanCleanup(ctx, user.ID)")
	require.Contains(t, string(worker), "w.subscriptionCleanupGuard.CanCleanup(rctx, userID)")
	deletion, err := os.ReadFile("deletion_service.go")
	require.NoError(t, err)
	require.Contains(t, string(deletion), "kind == repository.DeletionKindRetention && s.subscriptionCleanupGuard != nil")
}
