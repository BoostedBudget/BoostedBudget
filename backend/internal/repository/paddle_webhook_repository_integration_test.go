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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestListWebhookEventsSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPaddleWebhookRepository(pool, enc)
	ctx := context.Background()

	t.Run("sort by processing_attempts asc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{SortBy: "attempts", SortDir: "asc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		for i := 1; i < len(events); i++ {
			assert.LessOrEqual(t, events[i-1].ProcessingAttempts, events[i].ProcessingAttempts)
		}
	})

	t.Run("sort by processed desc", func(t *testing.T) {
		filters := domain.WebhookEventFilters{SortBy: "status", SortDir: "desc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		// processed is bool; DESC means true first. Once we see a false, all remaining must be false.
		seenFalse := false
		for _, e := range events {
			if !e.Processed {
				seenFalse = true
			}
			if seenFalse {
				assert.False(t, e.Processed, "expected all false rows after the first false row (DESC sort)")
			}
		}
	})

	t.Run("sort by created_at desc (default)", func(t *testing.T) {
		filters := domain.WebhookEventFilters{SortBy: "created_at", SortDir: "desc"}
		events, _, err := repo.ListWebhookEvents(ctx, filters, 100, 0)
		require.NoError(t, err)
		for i := 1; i < len(events); i++ {
			assert.False(t, events[i-1].CreatedAt.Before(events[i].CreatedAt),
				"expected descending created_at order")
		}
	})
}
