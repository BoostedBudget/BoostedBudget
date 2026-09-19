package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainWiresPostHogErasureOutboxWorkerIndependentlyOfProviderConfig(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	require.NoError(t, err)
	source := string(raw)
	assert.Contains(t, source, "profileRepo.SetPostHogErasureTarget")
	assert.Contains(t, source, "repository.NewPostHogErasureOutboxRepository")
	assert.Contains(t, source, "jobs.NewPostHogErasureWorker")
	assert.Contains(t, source, "river.AddWorker(riverWorkers, postHogErasureWorker)")
	assert.Contains(t, source, "postHogErasureWorker:              postHogErasureWorker")
}
