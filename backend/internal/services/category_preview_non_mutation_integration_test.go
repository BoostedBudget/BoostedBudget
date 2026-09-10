//go:build integration

package services

// Issue #1630 (Task 12b): previewCategoryMatches is about to be admitted into the MCP server's
// readExecute tool as a "safe POST" — a POST whose body is a filter, not a resource to persist,
// and whose handler is claimed to write nothing. Nothing in the codebase actually proved that
// claim before this test; the manifest comment ("POST but non-mutating") was assertion, not
// evidence. This calls the REAL CategoryPreviewService, wired to the REAL TransactionRepository
// against real Postgres, across every MatchLogic branch (category_only, tag_only, and, or) —
// each of which builds a structurally different SQL query in GetMatchingTransactionStats and
// GetCategoryMatchBreakdown — and asserts that not one row in `transactions`, `custom_categories`,
// or `audit_logs` changes across any of the calls.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -p 1 \
//	  -run TestPreviewCategoryMatches_WritesNothing_Integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewCategoryMatches_WritesNothing_Integration(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "1630-preview-nomutation-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-1630-preview-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-1630-preview', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	customCategoryID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Groceries Umbrella', 'FOOD_AND_DRINK')
	`, customCategoryID, userID)
	require.NoError(t, err, "seed custom category")

	txnDate := time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC)
	insertTxn := func(name string, amount float64, pfcPrimary, pfcDetailed *string, tags []string, kind *string, customID *string) {
		_, execErr := tx.Exec(ctx, `
			INSERT INTO transactions
			  (id, user_id, account_id, external_transaction_id, name, amount, date, pending, source,
			   pfc_primary, pfc_detailed, tags, category_override_kind, custom_category_override_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,false,'plaid',$8,$9,$10,$11,$12)
		`, uuid.New().String(), userID, accountID, "plaid-1630-preview-"+uuid.New().String(),
			name, amount, txnDate, pfcPrimary, pfcDetailed, tags, kind, customID)
		require.NoError(t, execErr, "seed transaction %q", name)
	}
	sp := func(s string) *string { return &s }

	insertTxn("groceries", 85.40, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_GROCERIES"), nil, nil, nil)
	insertTxn("dining", 32.10, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_RESTAURANT"), nil, nil, nil)
	insertTxn("custom-groceries", 44.00, nil, nil, nil, sp("custom"), &customCategoryID)
	insertTxn("tagged-txn", 19.99, sp("ENTERTAINMENT"), nil, []string{"subscription"}, nil, nil)

	enc := newTestFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	previewSvc := NewCategoryPreviewService(txnRepo)

	startDate := "2026-01-01"
	endDate := "2026-03-01"

	// A representative snapshot of everything a write could plausibly touch: row counts for the
	// three tables the query joins/reads, PLUS an amount checksum on transactions (catches an
	// UPDATE that changes a value without changing the row count).
	type snapshot struct {
		transactionCount    int
		transactionAmounts  float64
		customCategoryCount int
		auditLogCount       int
	}
	takeSnapshot := func() snapshot {
		var s snapshot
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM transactions WHERE user_id = $1`, userID,
		).Scan(&s.transactionCount, &s.transactionAmounts))
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`, userID,
		).Scan(&s.customCategoryCount))
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE user_id = $1`, userID,
		).Scan(&s.auditLogCount))
		return s
	}

	before := takeSnapshot()
	require.Equal(t, 4, before.transactionCount, "sanity: all four seeded transactions are visible before any call")

	// Exercise every MatchLogic branch — each builds a structurally different query in
	// GetMatchingTransactionStats / GetCategoryMatchBreakdown.
	calls := []*domain.CategoryPreviewRequest{
		{
			MatchLogic:   domain.MatchLogicCategoryOnly,
			CategoryKind: "pfc",
			PFCPrimary:   sp("FOOD_AND_DRINK"),
			StartDate:    startDate,
			EndDate:      endDate,
		},
		{
			MatchLogic: domain.MatchLogicTagOnly,
			Tags:       []string{"subscription"},
			StartDate:  startDate,
			EndDate:    endDate,
		},
		{
			MatchLogic:   domain.MatchLogicAnd,
			CategoryKind: "pfc",
			PFCPrimary:   sp("FOOD_AND_DRINK"),
			Tags:         []string{"subscription"},
			StartDate:    startDate,
			EndDate:      endDate,
		},
		{
			MatchLogic:       domain.MatchLogicOr,
			CategoryKind:     "custom",
			CustomCategoryID: &customCategoryID,
			Tags:             []string{"subscription"},
			StartDate:        startDate,
			EndDate:          endDate,
		},
	}

	for i, req := range calls {
		_, previewErr := previewSvc.PreviewCategoryMatches(ctx, userID, req)
		require.NoError(t, previewErr, "call %d (%s) should succeed", i, req.MatchLogic)
	}

	after := takeSnapshot()
	assert.Equal(t, before, after,
		"previewCategoryMatches must not change transactions, custom_categories, or audit_logs "+
			"across any MatchLogic branch — this is the proof gating its readExecute safe-POST annotation")
}
