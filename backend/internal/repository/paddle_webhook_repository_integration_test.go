//go:build integration

package repository

// Integration tests for paddle webhook repository sort.
// These tests execute SQL against a real Postgres planner and verify that the
// dynamic ORDER BY clause (sort_by/sort_dir) produces correct ordering.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListWebhookEventsSort_Integration -tags integration -v

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestListWebhookEventsSort_Integration exercises the dynamic `ORDER BY %s %s` in
// ListWebhookEvents against a real planner — the stated reason this file exists.
//
// ⚠ #1569 r17: this test SEEDS. Until this round it asserted ordering over whatever
// `paddle_webhook_events` happened to hold, and nothing in the tree ever writes that table against
// a real database (the only writes are pgxmock expectations). On a freshly-migrated CI database the
// result set was EMPTY, so every `for i := 1; i < len(events); i++` loop iterated zero times and
// the file could not fail — not for an inverted direction, not for a dropped ORDER BY.
//
// Two properties make the assertions real rather than cosmetic:
//   - every fixture carries a DISTINCT sort key, so an inverted or dropped ORDER BY produces a
//     different sequence (equal keys satisfy an ordering assertion in any order);
//   - the seeds share a unique `event_type`, and every call filters on it, so the assertions are
//     about EXACTLY these rows and cannot be perturbed by another test's fixtures on the shared
//     integration database.
func TestListWebhookEventsSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPaddleWebhookRepository(pool, enc)
	// ⚠ NOT t.Context(): the cleanup below must outlive this test's own context, or the fixture rows
	// leak into the shared integration database and corrupt OTHER packages' whole-table reads.
	ctx := context.Background()

	eventType := fmt.Sprintf("sort_fixture_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM paddle_webhook_events WHERE event_type = $1`, eventType)
	})

	// created_at is stamped by a column default, so it is set explicitly afterwards: without that,
	// the four rows differ only by however long four INSERTs take, and the created_at ordering
	// assertion would be pinning statement latency rather than the ORDER BY.
	base := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Millisecond)
	seeds := []struct {
		suffix    string
		attempts  int
		processed bool
		createdAt time.Time
	}{
		{"a", 7, false, base.Add(3 * time.Hour)},
		{"b", 0, true, base.Add(1 * time.Hour)},
		{"c", 3, true, base.Add(2 * time.Hour)},
		{"d", 11, false, base.Add(4 * time.Hour)},
	}
	// Rows go in through repo.Create so the payload takes the real encryption path — a fixture that
	// wrote plaintext into the encrypted column would produce a row production cannot produce, and
	// the scan would fail to decrypt it.
	for _, sd := range seeds {
		ev := &domain.PaddleWebhookEvent{
			EventID:            eventType + "_" + sd.suffix,
			EventType:          eventType,
			Payload:            json.RawMessage(`{"seed":"` + sd.suffix + `"}`),
			SignatureValid:     true,
			Processed:          sd.processed,
			ProcessingAttempts: sd.attempts,
		}
		require.NoError(t, repo.Create(ctx, ev))
		_, err := pool.Exec(ctx,
			`UPDATE paddle_webhook_events SET created_at = $1 WHERE id = $2`, sd.createdAt, ev.ID)
		require.NoError(t, err)
	}

	eventIDs := func(events []domain.PaddleWebhookEvent) []string {
		out := make([]string, 0, len(events))
		for _, e := range events {
			out = append(out, e.EventID)
		}
		return out
	}

	t.Run("sort by processing_attempts asc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{EventType: &eventType, SortBy: "attempts", SortDir: "asc"}
		events, total, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		// The floor: without it every assertion below is vacuous again the moment seeding breaks.
		require.Len(t, events, len(seeds), "the fixtures must be visible or the ordering assertion proves nothing")
		require.Equal(t, len(seeds), total)

		attempts := make([]int, 0, len(events))
		for _, e := range events {
			attempts = append(attempts, e.ProcessingAttempts)
		}
		assert.Equal(t, []int{0, 3, 7, 11}, attempts, "expected ascending processing_attempts")
	})

	t.Run("sort by processing_attempts desc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{EventType: &eventType, SortBy: "attempts", SortDir: "desc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		require.Len(t, events, len(seeds))

		attempts := make([]int, 0, len(events))
		for _, e := range events {
			attempts = append(attempts, e.ProcessingAttempts)
		}
		assert.Equal(t, []int{11, 7, 3, 0}, attempts, "expected descending processing_attempts")
	})

	t.Run("sort by processed desc puts the processed rows first", func(t *testing.T) {
		filters := domain.WebhookEventFilters{EventType: &eventType, SortBy: "status", SortDir: "desc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		require.Len(t, events, len(seeds))

		// processed is a bool: DESC means true first. Only the boolean sequence is asserted — ties
		// inside each group have no defined order and asserting one would be a flake.
		processed := make([]bool, 0, len(events))
		for _, e := range events {
			processed = append(processed, e.Processed)
		}
		assert.Equal(t, []bool{true, true, false, false}, processed, "expected processed=true rows first (DESC)")
	})

	t.Run("sort by created_at desc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{EventType: &eventType, SortBy: "created_at", SortDir: "desc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		require.Len(t, events, len(seeds))

		assert.Equal(t,
			[]string{eventType + "_d", eventType + "_a", eventType + "_c", eventType + "_b"},
			eventIDs(events),
			"expected newest-first created_at order")
	})

	t.Run("sort by created_at asc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{EventType: &eventType, SortBy: "created_at", SortDir: "asc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		require.Len(t, events, len(seeds))

		assert.Equal(t,
			[]string{eventType + "_b", eventType + "_c", eventType + "_a", eventType + "_d"},
			eventIDs(events),
			"expected oldest-first created_at order")
	})
}
