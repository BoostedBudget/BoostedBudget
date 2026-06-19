# Design: Investment Transaction UpsertBatch Atomicity (#1306)

## Problem

`InvestmentTransactionRepository.UpsertBatch` performs two separate, non-transactional
database operations:

1. A per-row CTE: `INSERT ... ON CONFLICT DO NOTHING RETURNING id`
2. A post-loop bulk `UPDATE ... SET user_account_id = ... WHERE user_account_id IS NULL`

A crash or connection failure between step 1 and step 2 leaves pre-existing rows with
`user_account_id IS NULL` until the next sync cycle. Those rows are excluded from
chain-expansion queries (`GetByUserIDKeyset`) until they are repaired.

## Fix: Merge Backfill into INSERT via ON CONFLICT DO UPDATE

Replace `ON CONFLICT DO NOTHING` with `ON CONFLICT DO UPDATE ... WHERE user_account_id IS NULL`.
This eliminates the separate bulk UPDATE entirely; each row's insert and backfill become a single
atomic SQL statement.

### SQL Change

**Before:**
```sql
WITH ins AS (
    INSERT INTO investment_transactions (... user_account_id ...)
    VALUES ($1, ..., $4, ...)
    ON CONFLICT (plaid_investment_transaction_id) DO NOTHING
    RETURNING id
)
SELECT COUNT(*) AS inserted FROM ins
```

**After:**
```sql
WITH ins AS (
    INSERT INTO investment_transactions (... user_account_id ...)
    VALUES ($1, ..., $4, ...)
    ON CONFLICT (plaid_investment_transaction_id) DO UPDATE
        SET user_account_id = EXCLUDED.user_account_id
        WHERE investment_transactions.user_account_id IS NULL
    RETURNING id, (xmax = 0) AS is_new_insert
)
SELECT COUNT(*) FILTER (WHERE is_new_insert) AS inserted FROM ins
```

### Conflict Semantics

| Scenario | Outcome | Appears in RETURNING | Counted as inserted? |
|---|---|---|---|
| New row | Inserted with `user_account_id` from `$4` | Yes (`xmax = 0`) | Yes |
| Conflict, `user_account_id IS NULL` | `user_account_id` backfilled | Yes (`xmax ≠ 0`) | No |
| Conflict, `user_account_id` already set | No-op (WHERE false) | No | No |

The `(xmax = 0)` predicate is the standard PostgreSQL idiom to distinguish a fresh INSERT from
an upsert UPDATE within a RETURNING clause. It is safe and documented behaviour.

### Go Code Changes

`backend/internal/repository/investment_transaction_repository.go` — `UpsertBatch`:

1. Replace the CTE SQL as described above.
2. Change `Scan(&inserted)` destination: the new query still returns a single `int` column
   (`COUNT(*) FILTER (WHERE is_new_insert)`), so the scan signature is unchanged.
3. Delete `backfillPIDs`, `backfillUAIDs` variable declarations.
4. Delete the `if txn.UserAccountID != nil { backfillPIDs = append(...) }` block inside the loop.
5. Delete the post-loop `if len(backfillPIDs) > 0 { r.db.Exec(...) }` block.

Net result: ~15 lines deleted, no new lines added outside the SQL string.

## Testing Strategy

### Unit Tests (pgxmock) — `investment_transaction_repository_test.go`

Update existing `UpsertBatch` tests to match the new CTE SQL:

- `ExpectQuery` anchor changes from `INSERT INTO investment_transactions` (unchanged) but the
  query body now contains `DO UPDATE SET user_account_id`. Update `regexp.QuoteMeta` anchors as
  needed (any existing anchor that matches a substring still in the new query continues to work).
- Remove `ExpectExec` for the bulk UPDATE from any test that had it (none currently do — the
  existing tests don't cover backfill; they only cover insert count and duplicates).

Add new unit test:
- **`TestInvestmentTransactionRepository_UpsertBatch_BackfillsNullUserAccountID`** — mock returns
  `xmax = 0, is_new_insert = false` for a conflict row; asserts inserted count = 0 but no error.

### Integration Tests — `investment_integration_test.go`

The existing `TestInvestment_UserAccountID_SurvivesRelink_Integration` already covers the backfill
path end-to-end. It will continue to pass unchanged — it seeds a NULL row and asserts it is
repaired by a subsequent `UpsertBatch`. No new integration test needed; verify the existing test
still passes.

### What Is NOT Changed

- `InvestmentTransactionRepository` interface — signature of `UpsertBatch` is unchanged.
- All caller code in `investment_sync_service.go` — no changes.
- All other repository methods — no changes.
- The `inserted` count semantics — new rows only, same as before.

## Risks / Considerations

- **`xmax` is an internal PostgreSQL column** — it is exposed as a stable, documented pseudo-column
  in RETURNING clauses and is widely used for this pattern. It is not going away.
- **DO UPDATE acquires a row-level lock on conflict** — `DO NOTHING` does not. For this workload
  (investment transaction sync), the concurrency concern is negligible: the only writer is the
  per-user sync job, which runs serially per linked item.
- **pgxmock tests** — pgxmock cannot distinguish `DO NOTHING` from `DO UPDATE` at the mock layer,
  so mock tests exercise the Go control-flow (correct arg binding, correct count scan) but not the
  SQL semantics. The integration test covers SQL correctness.
