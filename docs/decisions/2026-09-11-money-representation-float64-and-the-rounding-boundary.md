# ADR: Money Stays `float64` in Go, and Is Rounded at the API Edge

**Date:** 2026-09-11
**Status:** Accepted
**Issue:** #1631
**Related:** `docs/decisions/2026-09-11-realized-gain-loss-not-supported.md` (the companion decision
on what we compute); `backend/internal/domain/investment_currency.go` (the mixed-currency decision,
which is a *different* problem and is distinguished below)

---

## Context

Every monetary column in this schema is an exact decimal in Postgres and a binary float in Go.
`DECIMAL(18, 2)`, `NUMERIC(18, 2)` and `DECIMAL(18, 8)` columns land in `float64` or `*float64`
without exception — `holdings.cost_basis`, `holdings.institution_value`,
`investment_transactions.amount`, `securities.close_price`, and on the banking side
`Transaction.Amount float64` (`backend/internal/domain/transaction.go`). The migration set declares
8 `DECIMAL(18, 8)` columns (all in migrations `000051` and `000160`) and 39 `DECIMAL(18, 2)` /
`NUMERIC(18, 2)` columns; re-derive with
`grep -rniE '(DECIMAL|NUMERIC)\(18, ?[28]\)' backend/migrations` rather than trusting these counts,
which are a snapshot.

Three things were true when #1631 started, and all three were verified again while writing this:

1. **No decimal library is in the build.** `backend/go.mod` (101 lines, Go 1.26.6) contains no
   `shopspring/decimal`, no `cockroachdb/apd`, no `govalues/decimal`, nothing equivalent. Values
   cross the boundary through `pgx/v5` v5.10.0's conversion into `float64` at `rows.Scan`.
2. **There was no ADR about money representation.** `docs/decisions/` had six records, none about
   this. The most load-bearing type decision in a financial product was an unrecorded default.
3. **There is no rounding boundary in the investment domain.** `math.Round` appears in exactly two
   handlers, neither of them here and neither of them an output boundary: `savings_impact.go` rounds
   a public aggregate to the nearest $1,000 to prevent fingerprinting, and
   `transfer_suggestion.go`'s `ConfirmMatch` rounds an **inbound** `actual_amount` to 2dp so the
   idempotency-key fold and the service call agree on one canonical value. No investment response
   field is rounded before it is serialised.

The consequence is visible in responses. A client asking for
`/api/v1/investments/portfolio/gain-loss` can receive `total_unrealized_gain_loss` or
`total_unrealized_gain_loss_pct` rendered as `1234.5600000000002`. That is not a wrong number. It is
a correct number presented in a way that makes the product look broken and gives a downstream
consumer — including the AI tool-call surface this analytics layer is explicitly designed to feed —
a string it has to decide what to do with.

---

## What the Risk Actually Is, and What It Is Not

This section matters more than the decision, because the decision is only defensible if the severity
is stated accurately. Overstating it would argue for a decimal migration we are explicitly not
doing; understating it would argue for doing nothing.

### It is NOT accumulated drift

The plausible scare story — "float64 money drifts, so totals are wrong" — does not describe this
codebase, for two structural reasons.

**Aggregation happens in Postgres, in exact NUMERIC, and converts once.** In
`investmentAnalyticsRepository.UnrealizedGainLoss`
(`backend/internal/repository/investment_analytics_repository.go`), the per-account value, gain/loss
and cost basis are all `SUM(...)` over `NUMERIC` columns. Postgres computes those exactly, at
arbitrary precision, across however many holdings the account has. The conversion to binary floating
point happens once per row at `rows.Scan`, on an already-final figure. Whatever error exists is a
single representation error, not an accumulated one.

**The Go-side loops are short.** What Go accumulates is the per-**account** rows — `totalValue`,
`totalCostBasis`, `totalKnownPairCost`, `totalGL` — which is tens of iterations for a realistic
portfolio, not tens of thousands. `float64` carries roughly 15–17 significant decimal digits; a
few dozen additions of cent-scale values do not consume them. The same shape holds for the
allocation breakdowns, where bucket totals are summed in SQL and only the bucket list is walked in
Go.

So: **drift is a real property of binary floating point and is not the practical risk here.** An
ADR that claimed otherwise would be inventing a problem, and the next engineer who measured it would
be right to distrust everything else in this document.

### It IS representation at the boundary

What actually reaches clients is the artefact of the final arithmetic step, and there are two shapes
of it, both in the same file:

- **Derived subtraction.** `SUM(institution_value) - SUM(cost_basis)` produces a value whose exact
  decimal result is representable in `NUMERIC` and is not representable in binary — the classic
  `0.1 + 0.2` situation, arrived at by subtraction rather than addition.
- **`× 100` percentages.** Five Go-side sites in
  `investment_analytics_repository.go` multiply a ratio by 100: four of the
  `buckets[i].TotalValue / grandTotal * 100` shape in `AllocationBreakdown` and
  `allocationBreakdownByAccount`, plus `totalGL / totalKnownPairCost * 100` for the gain/loss
  percentage. (Two further `* 100` occurrences in that file sit inside SQL string literals and are
  therefore exact `NUMERIC` arithmetic, converted once at the scan — the same distinction as
  above.) Dividing and then multiplying by 100 is a reliable way to manufacture a long tail of
  digits out of two clean inputs.

Both emit into `json` tags that the client renders directly. The severity is therefore
**presentational and contractual, not arithmetic**: the number is right, the *representation* is
ugly and unbounded in length, and no part of the API contract says how many digits a money field
carries.

### The one place the column genuinely exceeds `float64`

`DECIMAL(18, 8)` is 18 significant digits. `float64` has ~15–17. This is not a rounding-style
concern; it is a range-of-representable-values concern, and it is real for
`holdings.quantity`, `investment_transactions.quantity`, `institution_price` and `close_price`
(migrations `000051`, `000160`). A crypto position large enough to use the integer part while also
using the eight fractional digits — the shape `DECIMAL(18,8)` exists for — **can round on the
round-trip through Go.** For equity share counts and prices at ordinary magnitudes it cannot; the
digits are simply not there to lose.

This is not hypothetical, and #1631 documented it independently before this ADR existed. The header
comment on `PositionReconciliationQuantityTolerance`
(`backend/internal/domain/investment_reconciliation.go`) records a *provable* divergence: the
reconciliation's SQL compares exact `NUMERIC` arithmetic against the tolerance, while
`ClassifyPositionReconciliationMember` compares whatever `float64` the same decimal rounds to after
scanning, so within roughly `1e-15` of the tolerance the two comparisons can land on opposite sides
of a strict `>`. It is labelled there as real production behaviour that no integration-test row can
prove, because the band is too thin to construct one in. That is the honest scale of this risk: a
razor-thin band that is genuinely reachable and is not going to move anyone's balance.

---

## Precision Is Not the Currency Problem — Do Not Conflate Them

#1631's mixed-currency work (M8a) surfaced a separate correctness failure that looks superficially
similar, and the two get confused because both are "the total is not what you think":

- **Precision** is about whether a number's *representation* is exact. The values share a unit; the
  sum means something.
- **Unit mismatch** is about whether a sum means anything *at all*. `SUM`ming a USD holding and a
  EUR holding produces a number with no unit, and no amount of decimal precision improves it.

That second problem was solved by **refusing or flagging, never by converting**
(`backend/internal/domain/investment_currency.go`). There is no FX rate source anywhere in this
repository, and the file's own header states the decision: a per-row figure that is meaningless
across currencies is returned nil (the day change), and a whole-portfolio aggregate is still
returned but declared via `currency_code: null` and `is_mixed_currency: true`. The asymmetry is
deliberate and documented there.

**Rounding a mixed-currency total to two decimals does not make it correct — it makes it tidily
wrong.** These two mechanisms are orthogonal and compose in one direction only: the currency
resolution decides *whether* a figure may be emitted; the rounding boundary in this ADR decides *how
many digits* an emittable figure carries. A future reader must not "fix" a mixed-currency complaint
with a rounding change, nor vice versa.

---

## Scope: What This ADR Does Not Decide

**A wholesale `float64` → decimal migration is explicitly OUT of scope**, and this is a scope
decision rather than a technical verdict on decimals.

The reason is symmetry: the banking side is identically `float64`. `Transaction.Amount` is a
`float64`, and the entire transaction, budget, boost, savings-goal and analytics surface is built on
that type — repository scans, service arithmetic, domain helpers such as `Transaction.ExpenseAmount`
and `IncomeAmount`, every `json` tag, the generated OpenAPI schema, and every generated TypeScript
model in `packages/api-client`. Converting the investment domain alone would produce two money types
in one codebase, which is strictly worse than one imperfect one: every boundary between them becomes
a conversion site, and a conversion site is where the bug that motivated the migration comes back.

A decimal migration is therefore a **separate programme** with its own issue, its own scope, and its
own decision record, touching the schema, the scan layer, every arithmetic site, the JSON contract,
and the frontend's number handling. It is not something to begin inside a bugfix branch. Nothing in
this ADR argues against it; this ADR argues that it cannot be done here, half-done anywhere, or
started by accident.

What IS in scope: **naming the convention we are actually running, and putting a rounding boundary
at the API edge.**

---

## The Decision

### 1. `float64` is the money type in Go, and that is now a recorded choice

Not a default, not an oversight, not something the next reader should treat as a bug to be fixed
locally. It applies to both domains — investment and banking — and applies uniformly so that no
boundary between two money types can exist.

**Corollary — the database stays exact.** `DECIMAL`/`NUMERIC` columns are not to be relaxed to
`double precision` to "match" Go. The exactness of the stored value and of Postgres-side aggregation
is what keeps the error to one representation step rather than many, and is the reason the drift
argument above holds. Storage exact, transport binary.

### 2. Aggregate in SQL, not in Go, wherever the choice exists

This is already the prevailing pattern and is what makes `float64` acceptable rather than merely
tolerated. `UnrealizedGainLoss`, the allocation breakdowns and the income summary each do their
arithmetic in one `NUMERIC` round-trip, which the analytics layer's design notes already require for
latency reasons. It turns out the precision argument and the "one DB round-trip" rule point the same
way: a per-row Go loop that re-derives what `SUM` could have computed is both slower and less exact.

### 3. Round at the API edge — the handler — and nowhere else

The boundary belongs in `backend/internal/huma/operations/`, on the way out, after every computation
is finished:

- **Currency-denominated fields** round to 2 decimal places.
- **Percentage fields** round to 2 decimal places.
- **Quantity and per-unit price fields do NOT round to 2dp.** `DECIMAL(18,8)` exists because
  fractional shares and crypto need those digits; rounding a quantity to cents destroys real data
  rather than cosmetic noise. If a quantity ever needs presentational rounding, it is 8dp, and it is
  a separate decision.

**Why the handler and not the repository:** the repository's value is the one that feeds further
computation — snapshots, reconciliation, any future composition at the API layer. Rounding there
would make a stored or re-aggregated figure depend on how many times it had passed through a
rounding step. The handler is the last place the value is used before it becomes JSON, which is the
definition of a boundary.

**Why not the database:** a `ROUND()` in SQL rounds before Go's own division and `× 100`, which
re-introduces the tail it was meant to remove.

**Honest limitation, stated rather than implied:** `math.Round(x*100)/100` is itself binary
arithmetic and is not exact at every input. This repo already has the worked example, in
`TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG`
(`backend/internal/huma/operations/transfer_suggestion_test.go`): `1.005` is represented as slightly
*less* than `1.005`, so `math.Round(1.005*100)` is `100`, and the result is `1.00`, not `1.01`. The
rounding boundary makes output **bounded and predictable in length**; it does not make it
decimal-exact, and claiming otherwise would be the same class of overstatement this ADR opens by
rejecting.

### 4. Nothing here changes which figures may be emitted

Rounding is applied to a figure that has already been cleared for emission. A nil day change stays
nil; a mixed-currency aggregate stays flagged; a NULL-basis holding stays excluded by its `FILTER`
clause. **Rounding never turns a withheld figure into a shown one, and never converts a `nil` to a
`0.00`** — the difference between "we do not know" and "it is zero" is exactly what the
`FILTER (WHERE h.cost_basis IS NOT NULL ...)` clauses and the currency refusals exist to preserve.

---

## What Would Trigger Revisiting This

- **A crypto or fractional-share position in production that demonstrably rounds on the round-trip.**
  This is the one blocker `float64` cannot be argued out of. `DECIMAL(18,8)`'s 18 digits against
  `float64`'s ~15–17 is arithmetic, not preference. One real position of the right magnitude, and
  quantity needs an exact type regardless of what the rest of the codebase uses.
- **Money movement.** The product is suggestions-only today; nothing initiates a transfer. If that
  ever changes, the amount that leaves an account must be exact, and this decision expires for that
  path on the day the feature is designed, not after it ships.
- **Reconciliation against an external ledger to the cent**, where a sub-cent disagreement becomes a
  reportable break rather than a diagnostic. The `PositionReconciliationQuantityTolerance` divergence
  above is the early warning of this shape.
- **Tax or regulatory reporting**, where the figure is filed rather than displayed.
- **A cross-domain decimal migration being funded on its own terms.** If the banking side moves, the
  investment side moves with it in the same programme — never independently.

---

## Consequences

### Positive

- The convention is written down, so the next reader knows `float64` was chosen rather than
  defaulted into, and knows the argument for it.
- API responses stop emitting `1234.5600000000002`. Length becomes bounded and predictable, which
  matters most for the machine consumers this analytics layer is designed for.
- The scope boundary is explicit, so nobody converts one domain to decimals in isolation and creates
  the two-money-types problem.
- The precision question and the currency question are separated in writing, so a complaint about
  one is not "fixed" by changing the other.

### Negative / Risks

- **`float64` remains inexact, and this ADR does not change that.** It bounds the *presentation*, not
  the representation. The `PositionReconciliationQuantityTolerance` band stays exactly as documented.
- **A rounding boundary is a convention, and conventions decay.** A new handler that forgets to round
  emits the old artefact, and nothing in the tree currently detects that. This ADR does not claim a
  guard exists — see the note below.
- **The `DECIMAL(18,8)` ceiling is unaddressed.** Deliberately: no production position is known to
  hit it, and fixing it properly means an exact quantity type, which is the out-of-scope programme.
  This is an accepted, named exposure, not an oversight.
- **Rounding is irreversible at the edge.** If a client ever needs the unrounded figure, it must come
  from a different field, not from removing the rounding.

### A note on enforcement, stated plainly

`docs/decisions/2026-08-24-financial-history-retained-by-default.md` earns its authority from guards
that fail when its rules are broken, and it says outright that a principle written down is not a
principle kept. **This ADR has no equivalent guard**, and pretending otherwise would be the failure
mode that ADR warns about. A convention-only decision is the weaker artefact; the honest statement is
that this one is currently enforced by review and by this document. The natural guard — an AST scan
asserting that every currency- or percentage-typed field in an investment response passes through a
rounding helper before serialisation — is a follow-up, and it should be built the way the existing
guards were: with a meta-test that constructs the violation and proves the guard reports it.

---

## References

- `backend/internal/repository/investment_analytics_repository.go` — `UnrealizedGainLoss` (SQL-side
  `SUM`/`FILTER`, Go-side per-account accumulation, `/ ... * 100`), allocation and top-mover
  percentage sites
- `backend/internal/domain/investment_portfolio.go` — the `float64`/`*float64` money fields and their
  `json` tags
- `backend/internal/domain/investment_currency.go` — the mixed-currency decision (refuse or declare,
  never convert)
- `backend/internal/domain/investment_reconciliation.go` — `PositionReconciliationQuantityTolerance`
  and the documented `NUMERIC`-vs-`float64` divergence band
- `backend/internal/domain/transaction.go` — `Transaction.Amount float64`, the banking side this ADR
  scopes itself alongside
- `backend/internal/huma/operations/transfer_suggestion.go` and `..._test.go` — the existing
  2dp-rounding precedent and its documented `.005` limitation
  (`TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG`)
- `backend/internal/huma/operations/savings_impact.go` — the other existing `math.Round`, an
  anonymisation step rather than a precision boundary
- `backend/migrations/000051_create_investment_tables.up.sql`,
  `backend/migrations/000160_create_holding_and_liability_history.up.sql` — the `DECIMAL(18,8)` and
  `DECIMAL(18,2)` columns
- `docs/decisions/2026-09-11-realized-gain-loss-not-supported.md` — the companion decision
