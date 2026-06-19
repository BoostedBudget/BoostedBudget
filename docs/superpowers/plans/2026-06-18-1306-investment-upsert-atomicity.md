# Investment Transaction UpsertBatch Atomicity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `InvestmentTransactionRepository.UpsertBatch` atomic by merging the separate post-loop `user_account_id` backfill UPDATE into the INSERT CTE via `ON CONFLICT DO UPDATE ... WHERE user_account_id IS NULL`, eliminating a non-transactional two-step operation.

**Architecture:** Replace `ON CONFLICT DO NOTHING` with `ON CONFLICT DO UPDATE SET user_account_id = EXCLUDED.user_account_id WHERE investment_transactions.user_account_id IS NULL`. Use `(xmax = 0)` in RETURNING to distinguish fresh inserts from backfill updates so the `inserted` count remains correct. Delete the `backfillPIDs`/`backfillUAIDs` collection and the bulk UPDATE entirely.

**Tech Stack:** Go, pgx/v5, pgxmock/v4, PostgreSQL 17

## Global Constraints

- Branch: `feature/1306-investment-upsert-atomicity`
- Commits: `#1306: message`
- TDD: red → green → refactor, no exceptions
- `inserted` count must continue to reflect only genuinely new rows (not backfill updates)
- No interface changes — `UpsertBatch(ctx, []domain.InvestmentTransaction) (int, error)` stays unchanged
- No caller changes — `investment_sync_service.go` is not touched

---

## Files

| Action | Path | What changes |
|--------|------|--------------|
| Modify | `backend/internal/repository/investment_transaction_repository.go:56-120` | Replace INSERT CTE SQL; delete backfill collection and bulk UPDATE |
| Modify | `backend/internal/repository/investment_transaction_repository_test.go` | Update existing mock expectations; add new backfill unit test |

---

### Task 1: Create feature branch

**Files:**
- None (git only)

- [ ] **Step 1: Create and switch to feature branch**

```bash
git checkout -b feature/1306-investment-upsert-atomicity
```

Expected: `Switched to a new branch 'feature/1306-investment-upsert-atomicity'`

---

### Task 2: Write failing tests, then implement the atomic CTE

This is the only code task. It covers the SQL change, Go cleanup, and all test updates in one commit because the repository file and its test file are tightly coupled — a reviewer cannot meaningfully approve one without the other.

**Files:**
- Modify: `backend/internal/repository/investment_transaction_repository.go:56-120`
- Modify: `backend/internal/repository/investment_transaction_repository_test.go`

**Interfaces:**
- Produces: `UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error)` — unchanged signature, unchanged semantics

#### Step 2.1 — Write the new failing unit test

Add this test to `backend/internal/repository/investment_transaction_repository_test.go`, after the `TestInvestmentTransactionRepository_UpsertBatch_MixedNewAndDuplicate` test (around line 123):

```go
// TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID verifies
// that when a conflicting row has user_account_id IS NULL, the DO UPDATE branch fires
// and the call succeeds without counting the row as inserted.
func TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	uaid := uuid.New().String()
	txn := makeTestInvTxn(userID, accountID)
	txn.UserAccountID = &uaid

	// Conflict row with NULL user_account_id: DO UPDATE fires.
	// xmax != 0 (update), so is_new_insert = false → COUNT returns 0.
	rows := pgxmock.NewRows([]string{"inserted"}).AddRow(0)
	mock.ExpectQuery(regexp.QuoteMeta(`ON CONFLICT (plaid_investment_transaction_id) DO UPDATE`)).
		WithArgs(anyArgs(15)...).
		WillReturnRows(rows)

	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{txn})

	require.NoError(t, err)
	assert.Equal(t, 0, inserted)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2.2: Run the new test to verify it fails**

```bash
cd backend && go test ./internal/repository/... -run TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID -v
```

Expected: FAIL — the test's `ExpectQuery` won't match the current `DO NOTHING` SQL.

#### Step 2.3 — Also update existing mock tests that must reflect the new SQL anchor

The existing `UpsertBatch` tests use `regexp.QuoteMeta(\`INSERT INTO investment_transactions\`)` as the `ExpectQuery` anchor. The new CTE body contains `DO UPDATE SET user_account_id`, so the `INSERT INTO investment_transactions` anchor continues to match (it's still present). **No change is needed to the existing `ExpectQuery` anchors.** Verify this by reading the tests at lines 58, 83, 114, 242 — each anchors on `INSERT INTO investment_transactions` which is still present in the new SQL.

However, the existing tests **do not** set up an `ExpectExec` for the bulk UPDATE (because the test transactions don't set `UserAccountID`). After the change, there is no bulk UPDATE, so expectations are already correct. No existing test needs modification.

- [ ] **Step 2.4: Implement the fix — replace CTE SQL and delete backfill code**

Replace the `UpsertBatch` method body in `backend/internal/repository/investment_transaction_repository.go`. The full new implementation:

```go
// UpsertBatch inserts investment transactions atomically.
// For each row: if it is new, it is inserted with user_account_id from the caller.
// If it already exists with user_account_id IS NULL, user_account_id is backfilled
// in the same statement (ON CONFLICT DO UPDATE ... WHERE user_account_id IS NULL).
// If it already exists with user_account_id already set, it is silently skipped.
// Returns the count of genuinely new rows (backfill updates are not counted).
func (r *investmentTransactionRepository) UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error) {
	if len(transactions) == 0 {
		return 0, nil
	}

	totalInserted := 0

	for i := range transactions {
		txn := &transactions[i]
		if txn.ID == "" {
			txn.ID = uuid.New().String()
		}
		txn.CreatedAt = time.Now()

		query := `
			WITH ins AS (
				INSERT INTO investment_transactions (
					id, user_id, account_id, user_account_id, security_id,
					plaid_investment_transaction_id,
					type, subtype, name,
					quantity, amount, price, fees, date,
					created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (plaid_investment_transaction_id) DO UPDATE
					SET user_account_id = EXCLUDED.user_account_id
					WHERE investment_transactions.user_account_id IS NULL
				RETURNING id, (xmax = 0) AS is_new_insert
			)
			SELECT COUNT(*) FILTER (WHERE is_new_insert) AS inserted FROM ins
		`

		var inserted int
		err := r.db.QueryRow(ctx, query,
			txn.ID,
			txn.UserID,
			txn.AccountID,
			txn.UserAccountID,
			txn.SecurityID,
			txn.PlaidInvestmentTransactionID,
			txn.Type,
			txn.Subtype,
			txn.Name,
			txn.Quantity,
			txn.Amount,
			txn.Price,
			txn.Fees,
			txn.Date,
			txn.CreatedAt,
		).Scan(&inserted)
		if err != nil {
			return totalInserted, err
		}
		totalInserted += inserted
	}

	return totalInserted, nil
}
```

Also update the doc comment at line 56 (above the function) to remove the outdated reference to `ON CONFLICT DO NOTHING` and the backfill step.

- [ ] **Step 2.5: Run all UpsertBatch unit tests**

```bash
cd backend && go test ./internal/repository/... -run TestInvestmentTransactionRepository_UpsertBatch -v
```

Expected: all 5 tests pass:
- `TestInvestmentTransactionRepository_UpsertBatch_InsertNew` — PASS
- `TestInvestmentTransactionRepository_UpsertBatch_SkipDuplicates` — PASS
- `TestInvestmentTransactionRepository_UpsertBatch_MixedNewAndDuplicate` — PASS
- `TestInvestmentTransactionRepository_UpsertBatch_Empty` — PASS
- `TestInvestmentTransactionRepository_UpsertBatch_NullSecurityID` — PASS
- `TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID` — PASS

- [ ] **Step 2.6: Run the full repository test suite**

```bash
cd backend && go test ./internal/repository/... -v 2>&1 | tail -20
```

Expected: all tests pass, no compilation errors.

- [ ] **Step 2.7: Run the integration test that covers the backfill path**

```bash
cd backend && go test ./internal/repository/... -run TestInvestment_UserAccountID_SurvivesRelink_Integration -v
```

Expected: PASS. This test seeds a NULL `user_account_id` row directly via SQL, then calls `UpsertBatch`, and asserts the NULL is repaired. It covers the actual PostgreSQL semantics end-to-end.

If you don't have a test database available, skip this step and note it in the PR body.

- [ ] **Step 2.8: Run full backend test suite**

```bash
cd backend && go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```

Expected: no FAIL lines.

- [ ] **Step 2.9: Commit**

```bash
git add backend/internal/repository/investment_transaction_repository.go \
        backend/internal/repository/investment_transaction_repository_test.go
git commit -m "#1306: make UpsertBatch atomic via ON CONFLICT DO UPDATE

Replace ON CONFLICT DO NOTHING + separate bulk UPDATE with a single
ON CONFLICT DO UPDATE SET user_account_id = EXCLUDED.user_account_id
WHERE investment_transactions.user_account_id IS NULL. Uses xmax = 0
in RETURNING to count only genuine inserts, not backfill updates.

Eliminates the non-transactional two-step that could leave NULL
user_account_id rows between a crash and the next sync cycle."
```

---

### Task 3: PR

- [ ] **Step 3.1: Run /commit-push-pr**

Use `/commit-push-pr` to push the branch and open a PR. PR body must include:

```
Closes #1306

## Summary
- Replaces `ON CONFLICT DO NOTHING` + post-loop bulk `UPDATE` in `UpsertBatch` with a
  single atomic `ON CONFLICT DO UPDATE SET user_account_id = EXCLUDED.user_account_id
  WHERE investment_transactions.user_account_id IS NULL`.
- Eliminates the non-transactional window where a crash between the INSERT loop and
  the bulk UPDATE left rows with `user_account_id IS NULL`.
- The `inserted` count is preserved via `(xmax = 0)` in RETURNING.
- ~15 lines deleted, no interface or caller changes.

## Test plan
- [ ] All existing `UpsertBatch` unit tests pass (pgxmock)
- [ ] New `TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID` unit test passes
- [ ] `TestInvestment_UserAccountID_SurvivesRelink_Integration` passes (covers SQL semantics end-to-end)
- [ ] Full backend test suite passes
```
