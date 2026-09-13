//go:build integration

package operations

// Issue #1630, Task 7: proves the happy path nothing else in this ticket covers — a VALID
// income_sources entry can actually be set via updateBudget, persists, and after sync
// income_amount becomes non-zero. Task 0 read the code; task 7 fixes the status code for the
// INVALID case (budget_1630_error_mapping_test.go); this is the GREEN control for the valid
// case, against real Postgres, exercising the same real income-calculation mechanism task 0
// already pinned in services/budget_income_1630_integration_test.go
// (TestClaim1630_P2_IncomeAmountZero_EmptySourcesVsPFCPrimary: $3500 seeded income, 0 with no
// income_sources configured, 3500 once income_sources names the pfc_primary category) — but
// driven through the HTTP handler chain (UpdateBudget -> SyncBudget), not the service directly.
// Given P2's own severity criterion ("income unsettable via the API -> P1"), this is the test
// that closes P2.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=redis://... REDIS_TEST_URL=redis://... \
//	  go test ./internal/huma/operations/... -tags integration -p 1 \
//	  -run TestUpdateBudget_PFCPrimaryIncomeSource_PersistsAndSyncsIncomeAmount -v

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopHappyPathEncryptionSvc satisfies crypto.EncryptionService without a real KMS call.
type noopHappyPathEncryptionSvc struct{}

func (n *noopHappyPathEncryptionSvc) Encrypt(plaintext string) (string, error) {
	return "NOOP_" + plaintext, nil
}

func (n *noopHappyPathEncryptionSvc) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "NOOP_"), nil
}

func newHappyPathFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	key := []byte("12345678901234567890123456789012") // 32 bytes
	enc, err := appcrypto.NewFieldEncryptor(&noopHappyPathEncryptionSvc{}, key)
	require.NoError(t, err)
	return enc
}

// insertHappyPathIncomeTxn seeds one income transaction tagged pfc_primary=INCOME inside tx.
func insertHappyPathIncomeTxn(t *testing.T, tx pgx.Tx, userID, accountID, linkedItemID string, date time.Time, amount float64) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending, pfc_primary)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Paycheck', false, 'INCOME')
	`, uuid.New().String(), userID, accountID, linkedItemID, amount, date)
	require.NoError(t, err, "seed income transaction")
}

// TestUpdateBudget_PFCPrimaryIncomeSource_PersistsAndSyncsIncomeAmount is Task 7's happy-path
// proof: updateBudget with a valid income_sources entry ({type:"pfc_primary", value:"INCOME"})
// persists, and after POST /budgets/{id}/sync income_amount is non-zero (the seeded $3500).
func TestUpdateBudget_PFCPrimaryIncomeSource_PersistsAndSyncsIncomeAmount(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck
	ctx := context.Background()

	userID := seedClaim1630User(t, tx)
	budgetID := seedClaim1630Budget(t, tx, userID)

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-happypath-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-happypath', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// Both posted deposits belong to the budget's eligible UTC day, including on
	// the first day of a month. Relative hour offsets could precede its start_date.
	// Keep task 0's pinned $3500 total. Inflow = negative amount under PFCv2.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	_, err = tx.Exec(ctx, `UPDATE budgets SET start_date=$2 WHERE id=$1`, budgetID, today)
	require.NoError(t, err)
	insertHappyPathIncomeTxn(t, tx, userID, accountID, linkedItemID, today, -1500.00)
	insertHappyPathIncomeTxn(t, tx, userID, accountID, linkedItemID, today, -2000.00)

	// Wire the REAL budget service + REAL sync service (income calculator + category
	// matching service), not mocks, so domain.Validate(), persistence and recalculation all
	// actually run.
	enc := newHappyPathFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	budgetRepo := repository.NewBudgetRepository(tx)
	customCategoryRepo := repository.NewCustomCategoryRepository(tx)
	budgetSvc := services.NewBudgetService(budgetRepo, customCategoryRepo, nil, logger)

	incomeSvc := services.NewBudgetIncomeService(txnRepo.(services.IncomeTransactionGetter), logger)
	matchingSvc := services.NewBudgetCategoryMatchingService(txnRepo)
	syncSvc := services.NewBudgetSyncService(budgetRepo, incomeSvc, matchingSvc, logger)

	handler := NewBudgetHandler(budgetSvc, syncSvc, services.NewTransactionService(txnRepo, customCategoryRepo))

	// Step 1: updateBudget sets a VALID income_sources entry.
	updateInput := &UpdateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	}
	updateInput.Body = domain.UpdateBudgetRequest{
		IncomeSources: &[]domain.IncomeSource{
			{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"},
		},
	}

	updateOut, err := handler.UpdateBudget(ctx, updateInput)
	require.NoError(t, err, "a VALID income_sources entry must be settable via updateBudget")
	require.NotNil(t, updateOut)
	require.Len(t, updateOut.Body.IncomeSources, 1, "income_sources must persist")
	assert.Equal(t, domain.IncomeSourceTypePFCPrimary, updateOut.Body.IncomeSources[0].Type)
	assert.Equal(t, "INCOME", updateOut.Body.IncomeSources[0].Value)
	assert.Equal(t, 0.0, updateOut.Body.IncomeAmount,
		"income_amount is not recomputed by UpdateBudget itself — only by sync")

	// Step 2: sync recomputes income_amount from the seeded transactions via the configured
	// income source.
	syncOut, err := handler.SyncBudget(ctx, &SyncBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	})
	require.NoError(t, err)
	require.NotNil(t, syncOut)

	assert.Equal(t, 3500.00, syncOut.Body.IncomeAmount,
		"P2 is closed: a valid income_sources entry is settable via the API AND actually "+
			"drives income_amount away from 0 after sync — it is not permanently stuck at 0 "+
			"the way an unconfigured budget is (services.TestClaim1630_P2_IncomeAmountZero_"+
			"EmptySourcesVsPFCPrimary pins the same $3500/0/3500 numbers one layer down)")
}
