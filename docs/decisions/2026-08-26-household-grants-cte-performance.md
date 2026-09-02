# householdGrantsCTE: measured performance, rejected alternatives, and the refuted anti-join

- **Date**: 2026-08-26
- **Issue**: #1569 (T3), #1582
- **Status**: Accepted. #1582 remains OPEN — its "within ~2x" target is NOT met at HEAD.
- **Code**: `backend/internal/repository/analytics_repository.go`, `householdGrantsCTE()` and
  `householdSharedTransactionPredicate()`.

## Why this is an ADR and not a comment

This content lived as a ~180-line block comment above `householdGrantsCTE`. On this branch alone it
grew three times, and each growth shifted every `analytics_repository.go:NNN` citation below it —
which is how a whole class of citation drift got introduced. Measurement history is a record, not an
explanation of the code beneath it, so it lives here. The function keeps the invariants a reader
must not break; this file keeps the numbers.

**What the code comment keeps**: the CTE's contract (interval emission, semi-join inclusion, the
membership guard), the correctness pin on `fingerprint_contested`, and a pointer here.

## Summary a reader needs before touching the function

1. **#1582's "within ~2x" is NOT met at HEAD** — the retention rewrite costs 6.00x against
   pre-#1569, and nothing measured here closes that.
2. **Option (b), the `(fingerprint, day)` anti-join, is refuted on OUTPUT, not on speed.** It makes
   money disappear. Do not re-propose it, in any transcription.
3. **Materialization is what makes HEAD fast, and it is also what forbids parallelism.** That trade
   is settled by the numbers below.

## The two performance properties the CTE carries

### 1. `fingerprint_contested`

A window-computed flag saying whether MORE THAN ONE `user_account` in this household holds this
fingerprint. The predicate's inner joint-account tiebreak is a correlated `NOT EXISTS` whose only
matching condition is `g2.user_account_id <> g.user_account_id` on the SAME fingerprint, so when
exactly one `user_account` holds the fingerprint the subquery is PROVABLY VACUOUS and skipping it is
an identity, not an approximation. That is always the case for mask-less accounts, whose fingerprint
COALESCEs to their own unique `ua.id`.

Measured: subplan executions 141,137 -> 34,628; 407ms -> 150ms.

⚠ It MUST be computed over EVERY grant, closed ones included. Deciding contestedness from the open
grants alone makes a joint account whose winning copy has been revoked look uncontested, skips the
tiebreak, and counts that account TWICE. Pinned by
`TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountWithClosedGrantCountedOnce`,
which fails at $120-vs-$60 under exactly that mutation.

### 2. UTC conversion hoisted into the CTE

`share_started_at` / `share_ended_at` are emitted ALREADY converted to UTC wall time. The predicate
compares them against `t.date` once per (transaction, grant) pair, so the CTE does 68 conversions
where the join filter did hundreds of thousands. Derived from the plan rather than estimated: the
semi-join tested 126,472 + 116,249 = 242,721 pairs (its `rows` plus its `Rows Removed by Join
Filter`), each evaluating the opening bound and, unless `share_ended_at` is NULL, the closing one too
— so between ~243,000 and ~486,000 conversions. Measured: 150ms -> 132ms.

The comparison rule itself is unchanged — see `householdSharedTransactionPredicate`, whose two
bounds are mutation-pinned.

Together these took the retention rewrite from 407ms back to 132ms. The rewrite's own cost was
measured on a seeded 200k-transaction / 50-account / 80-grant household (PG17, the same shape the
ticket guessed at): 18.7ms -> 407ms on cash-flow aggregation, a **21.8x** regression — not the 3-12x
#1582 asserted without a source.

## What was TRIED and is measurably WRONG, so nobody re-proposes it

- **`AS NOT MATERIALIZED`** (the plan's first candidate, and #1582's own suggestion) is **5.4x
  SLOWER**: 407ms -> 2,214ms, shared buffers 10,466 -> 433,877. `household_grants` is referenced
  twice, and inlining makes the correlated inner reference re-run the `has` x `user_accounts` join
  once per transaction row instead of scanning a materialized 68-row result.
- **An inline derived table carrying per-grant rival ARRAYs** (to get the reference count down to one
  and restore parallelism) is worse still: **6,537ms**, because the ARRAY subqueries get pushed into
  the same per-row subplan.

Materialization is what makes this query fast, and it is also what forbids parallelism (a CTE Scan is
parallel-restricted, which is why the `Gather` the pre-retention plan had does not come back).

## The round-5 table, and the ceiling that was wrong

An earlier version of this text ended "the parallel no-tiebreak ceiling is 37.7ms; nothing measured
gets closer to it without losing the dedup" — which read as "2x may be unreachable without a
correctness regression". Re-derived from scratch, that is false in both halves.

**Conditions for EVERY number in this section**, because ratios only travel if their conditions do:
Apple M4, 10 cores, 16 GiB, macOS 15.3.1, load avg ~4.3; Postgres 17.11 in Docker on `localhost:5434`
(NOT the Homebrew PG15 on 5432); database `t3bench_r5` = `pg_dump -s` of the then-current migrated
schema plus `backend/internal/repository/testdata/t3bench_seed.sql` unmodified; measured 2026-08-26
at commit `3d21e077`.

⚠ **The protocol is recorded PER MEASUREMENT, not as one blanket claim.** An earlier version ended
this paragraph with "best-of-7 via psql `\timing`" and scoped that over every number below it — but
the merchant-leg paragraph is best of 5, and so is the Go harness that produced it, so the blanket
claim was false and it had already propagated into the project ledger and the plan before it was
caught (#1569 round 5, Codex F6). Instrument and run count now travel with the block they describe:

- **The table below** — psql `\timing`, **best of 7, HAND-RUN**; plans confirmed with
  `EXPLAIN (ANALYZE, BUFFERS)`. **Four of its six rows are hand-written query variants that exist
  nowhere in Go, so `TestT3Bench` cannot reproduce them**: the count is as recorded by the person who
  ran them and is not re-derivable from the harness.
- **The merchant-leg paragraph** (last section) — the Go harness `TestT3Bench`
  (`t3bench_capture_test.go`), whose `iters = 5`: **best of 5**, as it says there.

| variant | time | vs pre-#1569 | plan |
|---|---|---|---|
| pre-#1569 (DISTINCT ON + JOIN) | 22.7ms | 1.00x | Gather, 2 workers |
| **HEAD** | **136.2ms** | **6.00x** | no Gather (CTE Scan x2) |
| no tiebreak (dedup DROPPED) | 26.9ms | 1.19x | Gather, 2 workers |
| option (b), day-winner JOIN | 1,956ms | 86.2x | nested loop, estimate collapse |
| option (b), losers ANTI JOIN | 76.2ms | 3.36x | Hash Semi + Hash Anti, no Gather |
| ...the same, parallelism forced | 33.1ms | 1.46x | Gather, 3 workers |

⚠ **BOTH anti-join rows are WRONG OUTPUT** — see the refutation below. Their timings are retained
only so nobody re-measures a variant that cannot ship.

Three corrections fall out of that table:

1. **The no-tiebreak ceiling is 1.19x, not 37.7ms/2.02x.** So #1582's "within ~2x" was NEVER pinned
   against a correctness regression — the criterion does not conflict with the joint-account dedup,
   and nothing here licenses dropping it. (The no-tiebreak variant is not merely faster, it is
   WRONG: it returns 5,436,775 where every correct variant returns 4,898,422.)
2. **Materialization is not intrinsic to the retention rewrite.** It happens because the TIEBREAK
   makes `household_grants` a SECOND reference; delete the tiebreak and Postgres auto-inlines the
   single-reference CTE, the parallel-restricted CTE Scan disappears, and `Gather` returns. Any
   future attempt should aim at getting the reference count back to one, not at hinting the CTE.
3. **Option (b) — the ticket's `(fingerprint, day)` mitigation — was built and measured, twice.**
   Written literally (a per-`(fingerprint, day)` winner relation equi-joined on
   `(user_account_id, date)`) it is 1.96s, 14x WORSE than HEAD: `generate_series`'s fixed 1000-row
   estimate collapses through `DISTINCT ON` + HashAggregate to 191 estimated rows against 34,685
   actual, so the planner picks a nested loop and demotes `a.id = t.account_id` to a join filter,
   discarding 6.2M rows. Casting the day to `date` so the key is hashable does not rescue it (1.95s)
   — the estimate, not the type, chooses the plan.

   Rewritten as an ANTI join it is FASTER AND WRONG. An earlier version said "Output is identical to
   HEAD on all four columns" — an assertion with no diff method, no row count and no reproducer,
   sitting two lines under a row that DID carry its falsification. Round 6 was told to ship the
   anti-join and re-established that claim first. It does not hold.

## Round 6: the anti-join is REFUTED, not merely deferred

The rewrite splits HEAD's single `EXISTS(g : g covers this day AND g wins its tiebreak)` into
`EXISTS(g : g covers) AND NOT EXISTS(g : g covers AND g loses)`. Those are different questions: the
first asks whether SOME covering grant wins, the second whether EVERY covering grant wins.

They agree only while no `user_account` holds two grants covering the same day — and
`uq_household_account_shares_active` forbids two UN-REVOKED grants per `(household, user_account)`
**and nothing more**, so unsharing an account and re-sharing it later the SAME DAY produces exactly
that, with the day-overlap predicate (generous at both ends, by design) putting both grants on the
boundary day. Two reproducible consequences, **both money DISAPPEARING**:

- the member's own, uncontested, freshly re-fingerprinted account is dropped for that day because an
  unrelated revoked grant of the same `user_account` lost a tiebreak; and
- worse, on a **JOINT** account **both copies are dropped** — the re-shared copy loses to the other
  member, the other member loses to the original copy — so the account contributes nothing at all on
  that day. HEAD counts exactly one copy, which is the whole point of the dedup.

This is a property of the DECOMPOSITION, not of one transcription of it. Measured, not argued: the
recorded day-expansion shape (subtract the `(user_account_id, day)` pairs where a contested
fingerprint's rival outranks this one) was rebuilt with `generate_series` and produces the same two
wrong answers. **Any relation keyed on `(user_account_id, day)` has the same defect**, because "this
user_account loses" is existential over grants while the correct rule is existential over WINNERS.
Getting it right needs the winner decided per fingerprint before the subtraction, which an anti-join
cannot express without aggregating the day expansion — and it is that expansion, not the anti-join,
that would need an upper date bound.

⚠ **The counterfactual, so it is not rediscovered as a fix.** The day expansion is precisely what
made the recorded variant need an upper date bound that `getMonthOverMonthSpendingHousehold` does
not have. The tempting response is to give that call site a bound (clamp it). The right response was
the opposite: drop the expansion and write the anti-join as a top-level correlated `NOT EXISTS`, so
no call site needs excluding and no bound is needed at all. That is what
`candidateAntiJoinPredicate` (`t3bench_equality_test.go`) does — and it is still refuted, on
semantics. **Excluding or clamping the month-over-month call site would not have saved the anti-join;
the decomposition is what is wrong.**

Pinned by `TestT3BenchAntiJoinIsNotEquivalent` (`t3bench_equality_test.go`), which builds all three
shapes — including a control proving the probe still agrees on the plain joint-account case — and
evaluates the REAL predicate functions, not transcriptions of them.

**And pinned against real Postgres**, because until #1569 round 6 nothing was:
`TestAnalyticsRepository_GetSpendingByCategory_Household_SameDayUnshareReshare_PlainAccountCountedOnce`
and
`TestAnalyticsRepository_GetSpendingByCategory_Household_SameDayUnshareReshare_JointAccountCountedOnce`
(`analytics_repository_household_integration_test.go`) build the divergent shape through the
production share/revoke paths and assert HEAD's amounts. Both return $60; under the anti-join
predicate both return **$0**. Measured under exactly that mutation, the four existing household
tests that come closest —
`TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountCountedOnce`,
`TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountWithClosedGrantCountedOnce`,
`TestAnalyticsRepository_GetSpendingByCategory_Household_DepartedMemberSharedPeriodRetained` and
`TestAnalyticsRepository_GetSpendingByCategory_Household_ClipsToSharedAt` — **all still passed.**
The suite agreed with the wrong variant, which is the same blind spot as the seed's.

### Why the agreement harness is not evidence

`TestT3BenchAntiJoinEquality`, in the same file, runs both forms of all NINE reads against the seeded
household and compares full result sets, row count and every column. **It PASSES — and that pass is
precisely why it is not sufficient evidence.** The seed gives each `user_account` one open grant plus
non-overlapping closed ones, so it **structurally cannot contain** the divergent shape. An agreement
harness bounds the risk; it does not establish the identity.

### The number dies with the claim

The anti-join written WITHOUT a day expansion (top-level correlated `NOT EXISTS`, expected to pull up
into a Hash Anti Join on the hashable `user_account_id` leg) **does not pull up at all**. Postgres
keeps it as `SubPlan 2` and executes it 177,778 times against HEAD's 30,781, and it is **4.5x
SLOWER**, not 1.8x faster: SpendingByCategory 128.5ms -> 544.8ms.

Conditions: same box and database as the round-5 table (Apple M4 / 10 cores / 16 GiB / macOS 15.3.1,
load avg ~3.5; Postgres 17.11 in Docker on `localhost:5434`, database `t3bench_r5`), measured
2026-08-26 at commit `e042468d`, by `TestT3BenchAntiJoinEquality`'s own timer — **best of 5**, NOT
the table's best of 7. That same run re-measures HEAD at 128.5-164.4ms across the nine reads, which
reproduces the table's 136.2ms.

## Where this leaves #1582

Nothing is shipped from option (b), and #1582 stays open at HEAD's 6.00x. It is also **not proven
unreachable** — with `parallel_setup_cost` / `parallel_tuple_cost` zeroed the anti-join form plans a
`Gather` and hits 1.46x, though that measurement inherits the same wrong output and so bounds nothing
on its own. What blocks it is that the serial and parallel plans sit within ~2% of each other at
default GUCs and application SQL cannot break the tie. A target met only when the planner happens to
flip parallel is not an SLA, so this is recorded as unmet rather than claimed. **#1582 is NOT closed
by this branch.**

The next idea worth trying is the one that keeps the ∃-over-winners shape while removing the
correlated subplan: give each grant a precomputed `daterange` and a `range_agg` multirange of the
days it LOSES, so the tiebreak becomes a scalar `NOT (t.date <@ g.loss_days)` inside the single
semi-join. That is an identity by construction (a union of loss intervals) and needs no date bound.
It is not built or measured here; it is a direction, not a result.

## One thing that is NOT a factor

#1585's third `ORDER BY` leg on the merchant reads. Measured with the Go harness `TestT3Bench`
(`t3bench_capture_test.go`), which runs `iters = 5` — so **this pair is best of 5, NOT the table's
best of 7**: with the leg, 169.0ms; with only two legs, 170.3ms. It sorts at most a few hundred
post-aggregation groups and does not move any ceiling here. Re-derived 2026-08-26 on the same box and
the same `t3bench_r5`: SpendingByMerchant best 166.8ms of 5 (mean 170.8ms), which matches the
with-the-leg figure to within run-to-run noise and confirms that this pair came from the harness at
n=5 rather than from the table's psql protocol.
