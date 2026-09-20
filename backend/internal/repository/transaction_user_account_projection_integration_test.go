//go:build integration

package repository

// Issue #1684. The rule preview reported ZERO matches for an `account_id eq <user_account_id>`
// rule against transactions that plainly carried that id. The cause was structural, not logical:
// `transactionSelectColumns` did not project `user_account_id`, so every read path produced a
// `domain.Transaction` whose `UserAccountID` was nil, and `matchAccountID` had nothing to compare.
//
// The two test doubles that should have caught it could not. The preview service's in-memory
// stub transaction reader is handed the columns the test author chose, and the repository unit
// tests build their mock rows from `transactionSelectColumns` itself — so both agreed with the
// defect by construction. Only a real row written through real SQL and read back through the real
// projection can see the difference, which is why this test carries the integration tag.
//
// It asserts TWO things, in order:
//  1. every row returned by GetByUserIDWithFilters carries the exact seeded `user_account_id`
//     (not merely a non-nil pointer), and
//  2. the end-to-end effect: those same rows match an `account_id` rule holding that id, evaluated
//     with the same `rules.NewRuleEngine()` the preview uses. A zero match count here is the
//     reported defect.
//
// It deliberately does NOT import the services package: services imports repository, so a
// package repository test that imported services would be an import cycle. The `rules` package
// does not import repository, so it is safe to reuse the engine directly. The preview read this
// stands in for is services/rule_preview_service.go (candidate read at :103, engine at :111-119).

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/rules"
)

func TestTransactionRepository_ProjectsUserAccountID(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "uacctproj-1684-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'User Account Projection Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-acc-uacctproj-"+accountID)
	require.NoError(t, err, "seed account")

	// MANDATORY: transactions.user_account_id carries a LIVE FK to user_accounts(id), so the
	// target must exist in the SAME tx before the transactions INSERT. On user_accounts only
	// user_id and institution_id are NOT NULL without a default. This id is the value the
	// read-back and the rule evaluation both pin.
	userAccountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, $3)
	`, userAccountID, userID, "ins-uacctproj-"+userAccountID)
	require.NoError(t, err, "seed user_account")

	// Several candidates, each carrying the SAME known, non-null user_account_id. Only the
	// columns without a default are supplied: source, amount, date and name.
	const candidateCount = 3
	baseDate := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	seededTxnIDs := make([]string, 0, candidateCount)
	for i := 0; i < candidateCount; i++ {
		txnID := uuid.New().String()
		seededTxnIDs = append(seededTxnIDs, txnID)
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name, user_account_id)
			VALUES ($1, $2, $3, $4, 'plaid', $5, $6, $7, $8)
		`, txnID, userID, accountID, linkedItemID, float64(10+i), baseDate.AddDate(0, 0, i),
			"UACCTPROJ_SENTINEL_"+txnID, userAccountID)
		require.NoError(t, err, "seed transaction %d", i)
	}

	// The REAL repository, bound to the same transaction the rows were seeded in — not a stub.
	repo := NewTransactionRepository(tx, newTestFieldEncryptor(t))

	got, err := repo.GetByUserIDWithFilters(ctx, userID, domain.TransactionFilters{}, 100, 0)
	require.NoError(t, err)
	require.Len(t, got, candidateCount,
		"the read must return every seeded candidate; an empty result would make the assertions below vacuous")

	// ── (1) The projection itself. Pin the exact value on every row. ──
	for _, row := range got {
		require.NotNil(t, row.UserAccountID,
			"row %s: user_account_id was not projected by the read path", row.ID)
		assert.Equal(t, userAccountID, *row.UserAccountID,
			"row %s: user_account_id must be the seeded value, not merely non-nil", row.ID)
	}

	// ── (2) The end-to-end effect, without importing services. ──
	// The candidate shape mirrors what PreviewRule forces onto a candidate: enabled, AND logic,
	// and an account_id eq condition holding the seeded user_account_id.
	rule := &domain.CategorizationRule{
		ID:             uuid.New().String(),
		UserID:         userID,
		Name:           "1684 account_id rule",
		Enabled:        true,
		Priority:       100,
		ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{
			{Field: domain.FieldAccountID, Operator: string(domain.RuleOpEq), Value: userAccountID},
		},
	}

	engine := rules.NewRuleEngine()
	matchCount := 0
	matchedIDs := make([]string, 0, candidateCount)
	for i := range got {
		matched, err := engine.RuleMatches(&got[i], rule)
		require.NoError(t, err, "evaluate account_id rule against row %s", got[i].ID)
		if matched {
			matchCount++
			matchedIDs = append(matchedIDs, got[i].ID)
		}
	}

	assert.NotZero(t, matchCount,
		"the account_id rule must match at least one candidate — zero is the defect #1684 reports")
	assert.Equal(t, candidateCount, matchCount,
		"every seeded candidate carries the seeded user_account_id and must match")
	assert.ElementsMatch(t, seededTxnIDs, matchedIDs,
		"the match set must be exactly the seeded rows, not a subset")
}
