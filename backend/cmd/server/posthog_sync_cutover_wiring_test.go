package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostHogSyncCompletionCutoverIsWiredToLinkedItemRepository(t *testing.T) {
	source, err := os.ReadFile("main.go")
	require.NoError(t, err)
	require.True(t, strings.Contains(string(source),
		"linkedItemRepo.SetTransactionSyncCompletionCutover(cfg.PostHogSyncCompletionCutoverAt)"),
		"POSTHOG_SYNC_COMPLETION_CUTOVER_AT must reach the repository's atomic classification query")
}
