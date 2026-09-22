//go:build integration

package services

// investment_personal_baseline_integration_test.go — #1706 personal-scope regression gate.
//
// The household-scope work must not change the PERSONAL investments output. This test seeds the
// exact deterministic fixture behind .omo/evidence/task-5-personal-baseline.json, calls the two
// personal-scope reads (scope nil), and asserts their JSON equals the golden baseline's
// personal_holdings / personal_portfolio_summary.
//
// The baseline lives OUTSIDE the worktree (it is evidence, not a committed fixture), so the path
// is overridable via PERSONAL_BASELINE_PATH and the test SKIPS when the file is absent — CI has no
// evidence dir. Locally, run it against the disposable `freshcheck` DB:
//
//	DATABASE_URL="postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable" \
//	go test -tags integration -p 1 -count=1 -run TestInvestmentPersonalBaseline_Unchanged \
//	    ./internal/services/ -v

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

const defaultPersonalBaselinePath = "/Users/yingyueliu/coding/BoostedBudget3/.omo/evidence/task-5-personal-baseline.json"

type personalBaselineDoc struct {
	PersonalHoldings         []*domain.HoldingView    `json:"personal_holdings"`
	PersonalPortfolioSummary *domain.PortfolioSummary `json:"personal_portfolio_summary"`
}

func TestInvestmentPersonalBaseline_Unchanged(t *testing.T) {
	path := os.Getenv("PERSONAL_BASELINE_PATH")
	if path == "" {
		path = defaultPersonalBaselinePath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("personal baseline not found at %s — skipping (evidence file is outside the worktree)", path)
	}
	var golden personalBaselineDoc
	require.NoError(t, json.Unmarshal(raw, &golden), "parse baseline %s", path)
	require.NotEmpty(t, golden.PersonalHoldings, "baseline must carry personal_holdings")

	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Same deterministic fixture as the generator: fixed literal UUIDs, fixed money values.
	const (
		userID       = "11111111-1111-4111-8111-111111111111"
		linkedItemID = "22222222-2222-4222-8222-222222222222"
		investAcctID = "33333333-3333-4333-8333-333333333333"
		depAcctID    = "44444444-4444-4444-8444-444444444444"
		sec1ID       = "55555555-5555-4555-8555-555555555555"
		sec2ID       = "66666666-6666-4666-8666-666666666666"
		sec3ID       = "77777777-7777-4777-8777-777777777777"
		hold1ID      = "88888888-8888-4888-8888-888888888888"
		hold2ID      = "99999999-9999-4999-8999-999999999999"
		hold3ID      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	)

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "task5-baseline|"+userID, "task5-baseline@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-task5-baseline')
	`, linkedItemID, userID, "task5-baseline-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Task5 Brokerage', 'investment', 'active', 0)
	`, investAcctID, linkedItemID, userID, "task5-inv-"+investAcctID)
	require.NoError(t, err, "seed investment account")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Task5 Cash Management', 'depository', 'active', 987.65)
	`, depAcctID, linkedItemID, userID, "task5-dep-"+depAcctID)
	require.NoError(t, err, "seed depository account")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, currency_code)
		VALUES ($1, $2, 'Task5 Equity', 'TSEC1', 'equity', 'USD')
	`, sec1ID, "task5-sec1-"+sec1ID)
	require.NoError(t, err, "seed security 1")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, currency_code)
		VALUES ($1, $2, 'Task5 ETF', 'TSEC2', 'etf', 'USD')
	`, sec2ID, "task5-sec2-"+sec2ID)
	require.NoError(t, err, "seed security 2")

	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, currency_code)
		VALUES ($1, $2, 'Task5 Fund', 'TSEC3', 'mutual fund', 'USD')
	`, sec3ID, "task5-sec3-"+sec3ID)
	require.NoError(t, err, "seed security 3")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price, currency_code)
		VALUES ($1, $2, $3, $4, 10, 800, 1000, 100, 'USD')
	`, hold1ID, userID, investAcctID, sec1ID)
	require.NoError(t, err, "seed holding 1")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price, currency_code)
		VALUES ($1, $2, $3, $4, 5, NULL, 500, 100, 'USD')
	`, hold2ID, userID, investAcctID, sec2ID)
	require.NoError(t, err, "seed holding 2 (NULL cost_basis)")

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price, currency_code)
		VALUES ($1, $2, $3, $4, 20, 100, 300, 15, 'USD')
	`, hold3ID, userID, depAcctID, sec3ID)
	require.NoError(t, err, "seed holding 3 (depository — must be excluded)")

	svc := NewInvestmentService(
		repository.NewHoldingRepository(tx),
		repository.NewPortfolioSnapshotRepository(tx),
		repository.NewInvestmentTransactionRepository(tx),
	)

	holdings, err := svc.ListHoldings(ctx, userID, nil, nil)
	require.NoError(t, err, "ListHoldings (personal)")
	summary, err := svc.GetPortfolioSummary(ctx, userID, nil)
	require.NoError(t, err, "GetPortfolioSummary (personal)")

	actualHoldings, err := json.Marshal(holdings)
	require.NoError(t, err)
	goldenHoldings, err := json.Marshal(golden.PersonalHoldings)
	require.NoError(t, err)
	require.JSONEq(t, string(goldenHoldings), string(actualHoldings),
		"personal ListHoldings output must equal the golden baseline")

	actualSummary, err := json.Marshal(summary)
	require.NoError(t, err)
	goldenSummary, err := json.Marshal(golden.PersonalPortfolioSummary)
	require.NoError(t, err)
	require.JSONEq(t, string(goldenSummary), string(actualSummary),
		"personal GetPortfolioSummary output must equal the golden baseline")
}
