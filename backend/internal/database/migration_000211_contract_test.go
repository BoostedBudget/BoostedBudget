package database

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000211_PostHogErasureOutboxContract(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000211_add_posthog_erasure_outbox.up.sql")
	require.NoError(t, err)
	sql := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS posthog_erasure_outbox",
		"identities_encrypted TEXT",
		"identities_encrypted LIKE 'ENC$%'",
		"UNIQUE (idempotency_key)",
		"CHECK (state IN ('pending', 'retrying', 'manual_pending', 'completed', 'dead_letter'))",
		"lease_token UUID",
		"version BIGINT NOT NULL DEFAULT 1",
		"support_verified_at TIMESTAMPTZ",
		"provider_person_uuids_encrypted TEXT",
		"posthog_erasure_operator_audit",
		"identity_reveal",
		"'replay'",
		"events_first_verified_at",
		"identities_encrypted IS NULL",
	} {
		assert.Contains(t, sql, fragment)
	}
	assert.NotContains(t, strings.ToLower(sql), "references users", "outbox must survive the user cascade")
	down, err := os.ReadFile("../../migrations/000211_add_posthog_erasure_outbox.down.sql")
	require.NoError(t, err)
	assert.Contains(t, string(down), "REFUSING TO DROP posthog_erasure_outbox")
}
