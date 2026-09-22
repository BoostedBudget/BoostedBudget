# Dead-letter triage for investment sync jobs

This is an **operator procedure**. It is written for a human with admin access to a
deployed environment. It is deliberately **not agent-executable**: no step here is safe
to run unattended, and nothing in this repository automates it. There is no bulk-replay
tool, no automated deletion, and no script behind this document. The only automatic
action that ever touches these rows is the 30-day retention drain described in Step 3.

The rows in scope are dead-letter entries for the three Plaid job kinds:

- `plaid_sync`
- `investment_holdings_sync`
- `investment_transactions_sync`

A row lands here only after a job exhausts all of its attempts. The default budget is
**5 attempts** (`backend/internal/queue/options.go`); periodic jobs use River's own
default of 25. The row is written by the River error handler once
`job.Attempt >= job.MaxAttempts` (`backend/internal/queue/river/dead_letter.go`).

## The one rule that matters

The rule is simple: **never blind-retry a `plaid_sync` dead-letter row.**

Replaying a `plaid_sync` row re-runs `SyncTransactions`. That function is idempotent for
transaction rows and for **nothing else on its tail**:

- **Transaction rows are safe.** `CreateBatch` upserts on
  `(source, external_transaction_id_blind_idx, user_id)`, and the `removed` tombstones
  are guarded by `deleted_at IS NULL`, so a re-delivery converges instead of duplicating.
- **`balance_snapshots` are not.** `balanceSnapshotRepository.Create` is a plain INSERT
  with no `ON CONFLICT`, and `snapshot_at` is a fresh `time.Now()` per run. A replay more
  than the 5-minute dedup window later adds a duplicate row per account.
- **The `PlaidSyncTxns` metrics are not.** The worker's counters add the page's
  added/modified/removed a second time.
- **Boost rules are not.** `ProcessEventTrigger` has no cooldown or dedup, so every
  matching rule re-executes: a new `boost_executions` row, incremented counters, and for
  transfer-suggestion rules a NEW pending suggestion with a fresh uuid.

The source states the conclusion directly:

> transactions are safe to re-deliver; nothing else on the tail is.

(`backend/internal/services/transaction_sync_service.go`, the T93(a) note in the
pagination loop.)

Because of this, a `plaid_sync` row must always be triaged individually. Do not retry it
to "see if it clears". Do not bulk-replay it. If the failure is transient and you believe
a re-run is warranted, treat that as a deliberate decision with the side effects above
accepted, not a routine action.

## Step 1 — Count, read-only

Establish what is actually there before touching anything. Both routes below are
read-only.

Admin API (requires an admin token):

```text
GET /api/v1/admin/dead-letter-jobs/stats
GET /api/v1/admin/dead-letter-jobs?kind=plaid_sync
GET /api/v1/admin/dead-letter-jobs?kind=investment_holdings_sync
GET /api/v1/admin/dead-letter-jobs?kind=investment_transactions_sync
```

Direct SQL, if you have database access instead:

```sql
SELECT kind, COUNT(*) FROM dead_letter_jobs GROUP BY kind ORDER BY kind;
SELECT id, kind, failed_at, retried_at, (notes IS NOT NULL) AS has_notes
  FROM dead_letter_jobs
 WHERE kind IN ('plaid_sync','investment_holdings_sync','investment_transactions_sync')
 ORDER BY failed_at;
```

Record the counts. As of the last read-only observation (2026-09-22, local dev only),
the backlog for all three kinds was **empty** in every reachable database; production was
not reachable from that environment, so treat the production count as unknown until you
run the query above against it.

## Step 2 — Inspect a row

Never act on a row you have not read. The detail route redacts args and errors; the
`/args` route returns the raw, unredacted job args for inspection.

```text
GET /api/v1/admin/dead-letter-jobs/{id}
GET /api/v1/admin/dead-letter-jobs/{id}/args
```

Read the error history and the args. Decide what the failure actually was before choosing
retry or delete.

## Step 3 — Pin a row under investigation

If you are investigating a row and do not want it swept away, attach notes to it:

```text
POST /api/v1/admin/dead-letter-jobs/{id}/notes
```

with a JSON body such as:

```json
{"notes": "under investigation - do not drain"}
```

The daily `DLQCleanupCheckWorker` deletes rows older than 30 days **unless they carry
notes** (`backend/internal/queue/jobs/dlq_cleanup_check.go`; the repository's
`DeleteOlderThan(..., skipWithNotes=true)` adds `AND notes IS NULL`). A noted row is
therefore preserved indefinitely until you clear the notes or delete the row yourself.
Use this to hold a row while you decide.

## Step 4 — Retry or delete

### Retry

```text
POST /api/v1/admin/dead-letter-jobs/{id}/retry
```

Retry is **single-shot and irreversible**. Once a row has been retried, `retried_at` is
set and a second attempt is refused with HTTP 409 (`ErrDeadLetterJobAlreadyRetried`). The
service claims the retry slot inside the same transaction that enqueues the job, so a
failed enqueue does not consume the slot, but a successful one does. A retry can also be
refused with 409 if River deduplicates it against an existing job, or with 422 if the job
kind has no registered uniqueness policy. There is no undo.

### Delete

```text
DELETE /api/v1/admin/dead-letter-jobs/{id}
POST /api/v1/admin/dead-letter-jobs/bulk-delete
```

Deletion is **permanent**. The row is gone from the product; the only recovery is a
Postgres point-in-time restore. Bulk delete requires at least one real filter and a
count-confirmation handshake: the server returns the exact match count and you must echo
it back, so a table-scale delete is necessarily a second, deliberate act. Do not use bulk
delete to "clean up" a kind you have not inspected row by row.

## Step 5 — Investment-job rows specifically

For kind `investment_holdings_sync` or `investment_transactions_sync`, deletion is safe
**only when inspection identifies the row as a pre-#1613 false-positive** — that is, the
row exhausted its attempts **before the #1613 fix shipped**, when both investment jobs
were enqueued unconditionally and a link with no investment grant failed the job for a
reason that was not actually an error.

These kinds are **not blanket-safe to delete**. A row of either kind minted **after** the
fix could be a genuine sync failure: a real transient error, or a real
`PRODUCTS_NOT_SUPPORTED` / `NO_INVESTMENT_ACCOUNTS` case that should have completed green.
Deleting it destroys the evidence permanently.

To classify a row:

1. Read `failed_at` from the detail route and compare it against the date #1613 shipped
   in the environment you are looking at. A row that exhausted its attempts before that
   date is a candidate.
2. Read the error history and confirm the failure is the pre-fix "investments not granted"
   false-positive, not a transient or genuine error.
3. Only if both hold, delete the row. If either is uncertain, leave it and pin it with
   notes instead.

A `plaid_sync` row is never covered by this shortcut. Triage it individually, per the rule
at the top of this document.

## What this procedure is not

- It is not a script. Do not wrap these steps in automation, and do not add a bulk-replay
  or auto-delete job.
- It is not agent-executable. Every mutation here is a deliberate human decision on a row
  a human has read.
- It is not a licence to retry `plaid_sync`. The prohibition at the top is the point of
  this document.