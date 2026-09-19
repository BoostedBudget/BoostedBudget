package services

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRevenueCatErasureWiredEvenWithoutPaddleConfiguration(t *testing.T) {
	source, err := os.ReadFile("../../cmd/server/main.go")
	require.NoError(t, err)
	require.Contains(t, string(source), "deletionService.SetRevenueCatCleanupService(services.NewRevenueCatErasureService(")
}
