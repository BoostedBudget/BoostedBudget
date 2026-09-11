# ADR: Money Stays `float64` in Go; Rounding Belongs at the API Edge

**Date:** 2026-09-11
**Status:** Accepted and **implemented** — all four decisions now describe the tree
**Issue:** #1631
**Implementation:** #1636, delivered in the #1631 branch (the user directed that nothing be
deferred). `backend/internal/money` holds the fold; `backend/internal/huma/operations/money_rounding.go`
applies it at the ten response surfaces; `TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge`
guards it. The `roundToCents` reconciliation this record named is resolved — upward, to the
handler — and the one figure that still folds in a service is reclassified rather than exempted;
see Decision 3.
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
3. **There was no rounding boundary in the investment domain.** At the time this was written,
   `math.Round` appeared at exactly three non-test sites in `backend/internal/`, none of them in
   the investment path. (#1636 added a fourth home for it — `internal/money.ToCents`, which is now
   the only definition of the 2dp fold and the one the sites below either use or are distinguished
   from.) The original three:
   - `huma/operations/savings_impact.go:84` — rounds a public aggregate to the nearest $1,000 to
     prevent fingerprinting. An anonymisation step, not a precision boundary.
   - `huma/operations/transfer_suggestion.go:318` — `ConfirmMatch` rounds an **inbound**
     `actual_amount` to 2dp so the idempotency-key fold and the service call agree on one canonical
     value. An input canonicalisation, not an output boundary.
   - `services/savings_progress_service.go:419-422` — `roundToCents(v float64) float64 {
     return math.Round(v*100) / 100 }`. **This one was the 2dp fold this ADR prescribes, at the
     SERVICE layer, which is not where Decision 3 below puts it.** ⚠ RESOLVED by #1636, and not by
     deletion alone: seven of its eight call expressions were presentation rounding and moved up to
     the handler; the eighth turned out to be something else entirely and stayed. See Decision 3.

   No investment response field was rounded before it was serialised. That is what #1636 changed.

The consequence reached responses. Before #1636, a client asking for
`/api/v1/investments/portfolio/gain-loss` **could** receive `total_unrealized_gain_loss` or
`total_unrealized_gain_loss_pct` rendered with a long binary tail — `1234.5600000000002` is the illustrative shape, constructed here rather than
captured from a production response; no repro is cited because none was recorded. What *is* verified
is the mechanism that produces it, enumerated below. Such a value is not a wrong number. It is a
correct number presented in a way that makes the product look broken and gives a downstream consumer
— including the AI tool-call surface this analytics layer is explicitly designed to feed — a string
it has to decide what to do with.

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
of it. The first lives in the analytics repository; the second is spread across the repository AND
the domain layer, so the boundary this ADR decides on is **surface-scoped, not file-scoped**:

- **Derived subtraction.** `SUM(institution_value) - SUM(cost_basis)` produces a value whose exact
  decimal result is representable in `NUMERIC` and is not representable in binary — the classic
  `0.1 + 0.2` situation, arrived at by subtraction rather than addition.
- **`× 100` percentages.** Dividing and then multiplying by 100 is a reliable way to manufacture a
  long tail of digits out of two clean inputs, and **ten** Go-side sites do it on values that
  reach clients.

  ⚠ **This table said EIGHT until #1636, and the two it missed are a whole layer.** The original
  inventory swept the repository and the domain and never looked at `internal/services`, where
  `investmentService.GetPortfolioSummary` manufactures two more — which is how an ADR about
  unbounded digits came to carry an unbounded-digit count that was wrong. Re-derive with
  `grep -rn "\* 100" backend/internal/{repository,domain,services}` rather than trusting the table;
  the guard, not this list, is what actually holds the boundary shut.

  | Where | Sites | Reaches the client as |
  |---|---|---|
  | `repository/investment_analytics_repository.go` | 4 × `buckets[i].TotalValue / grandTotal * 100` (`AllocationBreakdown`, `allocationBreakdownByAccount`) | `percentage` on each allocation bucket |
  | `repository/investment_analytics_repository.go` | `totalGL / totalKnownPairCost * 100` | `total_unrealized_gain_loss_pct` |
  | `services/investment_service.go` | `totalGainLoss / knownBasisCost * 100` ← **missed by the original inventory** | `total_gain_loss_pct` on `/portfolio/summary` |
  | `services/investment_service.go` | `dc / priorGated * 100` ← **missed by the original inventory** | `day_change_pct` on `/portfolio/summary` |
  | `domain/investment.go` | `Holding.GainLossPct()` | `gain_loss_pct`, via `NewHoldingView` in `investment_portfolio.go` |
  | `domain/investment.go` | `Holding.DayChangePct()` | `day_change_pct`, via the same conversion |
  | `domain/holding_snapshot_view.go` | `ComputeGainLoss` | `gain_loss_pct` on the historical view |

  (Line numbers have been dropped from this table deliberately. The three it carried were already
  drifting — `holding_snapshot_view.go:99` named the function signature rather than the arithmetic,
  which is at `:103` — and `TestMarkdownCitedTestNamesResolve` checks cited test *names* only, so
  nothing was ever going to catch that. Names resolve; line numbers rot.)

  The five non-handler sites outside the repository matter disproportionately to Decision 3: none
  is in a handler, so the rounding boundary rounds the *field* on the way out rather than assuming
  the producing function rounded. That is exactly how `money_rounding.go` is written. (Two further
  `* 100` occurrences in the analytics repository sit inside SQL string literals and are therefore
  exact `NUMERIC` arithmetic, converted once at the scan — the same distinction as above.)

Both emit into `json` tags that the client renders directly. The severity is therefore
**presentational and contractual, not arithmetic**: the number is right, the *representation* was
ugly and unbounded in length, and no part of the API contract said how many digits a money field
carried. Since #1636 the boundary bounds the length; the contract still does not state it, which is
a documentation gap rather than a defect.

### The one place the column genuinely exceeds `float64`

`DECIMAL(18, 8)` carries 18 significant digits. `float64` carries ~15–17. This is a
**significant-digit** concern, not a range concern — `float64`'s exponent range is about `1e±308`,
so magnitude is never the issue; the issue is how many digits survive between the largest and
smallest that a single value uses at once. It is real for `holdings.quantity`,
`investment_transactions.quantity`, `institution_price` and `close_price` (migrations `000051`,
`000160`). A crypto position large enough to use the integer part while *also* using all eight
fractional digits — the exact shape `DECIMAL(18,8)` exists for — needs more digits than `float64`
has and **can round on the round-trip through Go**. For equity share counts and prices at ordinary
magnitudes it cannot: the digits are simply not there to lose.

⚠ **Do not use the reconciliation tolerance as evidence for this.** The header comment on
`PositionReconciliationQuantityTolerance`
(`backend/internal/domain/investment_reconciliation.go`) records a genuine, provable divergence: the
reconciliation's SQL compares exact `NUMERIC` arithmetic against the tolerance, while
`ClassifyPositionReconciliationMember` compares whatever `float64` the same decimal rounds to after
scanning, so within roughly `1e-15` of the tolerance the two comparisons can land on opposite sides
of a strict `>`. That is real, and it is labelled there as production behaviour no integration-test
row can construct. **But it is a different phenomenon** — ordinary representation error on `0.001`
at magnitude ~1, which is well inside `float64`'s 15 digits and would occur with a `DECIMAL(18,3)`
column just the same. It is not the 18-digit ceiling. The source comment runs the two together, and
an earlier revision of this ADR reproduced that conflation rather than inventing it; a document
about precision has to keep them apart. The honest summary is: **the ceiling is real and
unobserved; the tolerance band is real and observed, and they are unrelated.**

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

⚠ **This boundary EXISTS.** The first revision of this record said "this boundary does not exist
yet" and asked to be read in the future tense; that sentence was true for one afternoon and is
kept here only so the change is visible. #1636 implemented it inside the #1631 branch, because the
alternative — a decision record describing code nobody had written — is the defect this paragraph
used to be.

The boundary lives in `backend/internal/huma/operations/money_rounding.go`, on the way out, after
every computation is finished. The fold itself is `backend/internal/money` (`ToCents`,
`ToCentsPtr`), which is a package rather than an unexported helper because two handler surfaces
need the identical one:

- **Currency-denominated fields** round to 2 decimal places.
- **Percentage fields** round to 2 decimal places.
- **Quantity and per-unit price fields do NOT round to 2dp.** `DECIMAL(18,8)` exists because
  fractional shares and crypto need those digits; rounding a quantity to cents destroys real data
  rather than cosmetic noise. If a quantity ever needs presentational rounding, it is 8dp, and it is
  a separate decision.

The ten surfaces covered are the nine investment/holdings-history operations (`getHoldings`,
`getPortfolioSummary`, `getPortfolioHistory`, `getInvestmentTransactions`,
`getPortfolioAllocationBreakdown`, `getTopMovers`, `getInvestmentGainLossSummary`,
`getInvestmentIncomeSummary`, `getAccountHoldingsHistory`) plus `getSavingsProgressSummary`, which
is here because of the reconciliation below rather than because it is an investment endpoint.

⚠ **Four response structs put a quantity or per-unit price directly beside a currency field**, and
they are the reason the third bullet is not a footnote: `domain.HoldingView`
(`quantity`, `institution_price`), `domain.InvestmentTransaction` (`Quantity`, `Price`),
`domain.HoldingSnapshotView` (`Quantity`, `InstitutionPrice`), and `domain.ActivityItem`
(`investment_quantity`, `investment_price` — the BANKING feed's type, which is not on any of the
ten surfaces and is therefore untouched). A blanket "round every `float64` on the response" would
pass a rounding test and silently turn a 0.00123456 BTC position into 0.00.

**Why the handler and not the repository:** the repository's value is the one that feeds further
computation — snapshots, reconciliation, any future composition at the API layer. Rounding there
would make a stored or re-aggregated figure depend on how many times it had passed through a
rounding step. The handler is the last place the value is used before it becomes JSON, which is the
definition of a boundary.

**Why not the database:** a `ROUND()` in SQL rounds before Go's own division and `× 100`, which
re-introduces the tail it was meant to remove.

**Why not the domain layer.** Three of the ten percentage sites inventoried above
(`Holding.GainLossPct`, `Holding.DayChangePct`, `HoldingSnapshotView.ComputeGainLoss`) are domain
methods, and it is tempting to round there instead — one edit, three sites, done. It is the wrong
place for the same reason the repository is: those values feed `investment_portfolio.go`'s
conversion and could feed further composition. The handler rounds the *field*, and does not assume
any producing function rounded for it. None of them was changed by #1636.

### The `roundToCents` reconciliation — RESOLVED UPWARD, with one reclassification

This decision was in direct tension with existing code. `savings_progress_service.go` defined
`roundToCents(v float64) float64 { return math.Round(v*100) / 100 }` — precisely the 2dp fold
specified here — and applied it in the **service** layer, one layer below where this ADR puts the
boundary, across eight call expressions. The first revision of this record left that open and
assigned it to #1636 "to resolve in one direction".

**Resolved upward.** The local `roundToCents` is deleted. Seven of the eight call expressions were
presentation rounding of figures on their way to `GET /savings-goals/progress-summary`; the service
now returns those unrounded and `roundSavingsProgressSummary` folds them at the handler. The
argument for the handler is the one already made above: a service value can feed further
computation, and a convention with a named exception drifts into two conventions.

⚠ **The eighth call expression was NOT presentation rounding, and it stayed in the service.** This
is a reclassification, not an exemption, and the distinction is load-bearing. In `RecomputeGoal`
the folded value is (1) **persisted** to `savings_goals.current_amount` by
`goalRepo.UpdateCurrentAmount` and read back on every later request, and (2) compared against the
target to decide **completion**, which `MarkCompleted` makes sticky and never revisits. A tail
either side of the target changes which goals complete. That makes it an **inbound canonicalisation
of a stored figure** — the same class as `ConfirmMatch`'s round in `transfer_suggestion.go`, which
Context item 3 above already classifies as "an input canonicalisation, not an output boundary"
rather than as a violation of this decision. It calls `money.ToCents`, so there is still exactly
one definition of "2dp" in the tree.

**The general rule this establishes:** a fold whose output becomes a database row or a control-flow
decision is a canonicalisation and belongs where that write or decision is. A fold whose output
becomes JSON is the boundary and belongs at the handler. "Which layer" was the wrong question; "is
this figure about to be stored, or about to be shown" is the right one.

⚠ One field gained rounding it never had: `linked_balance` on each progress item is money and the
old service-layer helper did not touch it. `account_percentage` deliberately did not and still does
not — despite the name it is a 0–1 multiplier applied to a balance, not a display percentage, and
folding it to 2dp would move a 33.5%-of-balance goal's target to 34%.

**Honest limitation, stated rather than implied:** `math.Round(x*100)/100` is itself binary
arithmetic and is not exact at every input. This repo already had the worked example, in
`TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG`
(`backend/internal/huma/operations/transfer_suggestion_test.go`): a `float64` holding `1.005` is
slightly *less* than `1.005`, so the fold yields `1.00`, not `1.01`.
`TestToCents_IsNotDecimalExact_TheADRsStatedLimitation` (`backend/internal/money`) now pins the
same fact for the shared helper, and adds the part that makes it unfixable by choosing a different
tie-break rule: `1.015` also folds *down*, to `1.01`, while `1.235` folds *up*, to `1.24`. The
direction at a `.5` boundary is a property of each input's individual binary representation, not a
rule.

⚠ **A correction to this paragraph's own arithmetic.** It asserted that "`math.Round(1.005*100)` is
`100`". That is true of the runtime `float64` computation it means and **false of the Go expression
as written**: `1.005*100` there is an untyped constant expression, which Go evaluates at arbitrary
precision at compile time, making it exactly `100.5` and the result `101`. Writing the claim as a
test is what surfaced it — the assertion had to be routed through a `float64` variable before it
described the code. A document about precision is a poor place to be imprecise about precision.

The rounding boundary makes output **bounded and predictable in length**; it does not make it
decimal-exact, and claiming otherwise would be the same class of overstatement this ADR opens by
rejecting.

### 4. Nothing here changes which figures may be emitted

Rounding is applied to a figure that has already been cleared for emission. A nil day change stays
nil; a mixed-currency aggregate stays flagged; a NULL-basis holding stays excluded by its `FILTER`
clause. **Rounding never turns a withheld figure into a shown one, and never converts a `nil` to a
`0.00`** — the difference between "we do not know" and "it is zero" is exactly what the
`FILTER (WHERE h.cost_basis IS NOT NULL ...)` clauses and the currency refusals exist to preserve.

**Mechanically enforced by the helper's signature.** `money.ToCentsPtr` returns `nil` for `nil`, and
every optional field on every one of the ten surfaces goes through it rather than through a
dereference — so converting a `nil` to `0.00` requires writing different code, not merely
forgetting to be careful. `TestToCentsPtr_NilStaysNil` pins it, and the guard reports any field the
boundary drops. The mixed-currency flags, the `ranking_currency_code` scoping and the
`day_change_unavailable_reason` enum are untouched by #1636: rounding decides how many digits an
emittable figure carries, never whether a figure may be emitted.

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
- API responses no longer emit long binary tails, and field length is bounded and predictable,
  which matters most for the machine consumers this analytics layer is designed for. **Realised by
  #1636**; the first revision of this record noted the benefit was not yet realised, which was
  accurate then.
- The reconciliation produced a rule sharper than the one this ADR started with: the question is
  not which layer rounds but whether the figure is about to be **stored** or about to be **shown**.
  See Decision 3's `roundToCents` section.
- The scope boundary is explicit, so nobody converts one domain to decimals in isolation and creates
  the two-money-types problem.
- The precision question and the currency question are separated in writing, so a complaint about
  one is not "fixed" by changing the other.

### Negative / Risks

- **`float64` remains inexact, and this ADR does not change that.** It bounds the *presentation*, not
  the representation. The `PositionReconciliationQuantityTolerance` band stays exactly as documented.
- **The boundary is presentational, so a consumer that needs full precision has lost it.**
  Rounding is irreversible at the edge (see the last bullet), and there is no unrounded twin field.
  Nothing asks for one today.
- **A rounding boundary is a convention, and conventions decay** — but this one is no longer
  convention-only. `TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge` fails when a
  money-shaped field on one of the ten surfaces is emitted unrounded, when a `DECIMAL(18,8)`
  quantity is folded, when a NEW float field appears with no recorded verdict, and when a recorded
  verdict names a field that no longer exists. ⚠ What it does NOT cover: an **eleventh** surface.
  Adding a money-bearing response and not adding it to the guard's table is still undetected, and
  the guard's own `require.Len(surfaces, 10)` is a speed bump rather than a fix — it fails only
  once somebody has already edited the table.
- **The two service-layer percentage sites were missed for a whole revision of this document.** The
  inventory said eight and the tree had ten. The guard now covers the fields those sites feed, which
  is the durable protection; the inventory remains prose and remains re-derivable only by grep.
- **The `DECIMAL(18,8)` ceiling is unaddressed.** Deliberately: no production position is known to
  hit it, and fixing it properly means an exact quantity type, which is the out-of-scope programme.
  This is an accepted, named exposure, not an oversight.
- **Rounding is irreversible at the edge.** If a client ever needs the unrounded figure, it must come
  from a different field, not from removing the rounding.

### A note on enforcement, stated plainly

`docs/decisions/2026-08-24-financial-history-retained-by-default.md` earns its authority from guards
that fail when its rules are broken, and it says outright that a principle written down is not a
principle kept. **This ADR now has one**, added by #1636:
`TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge`
(`backend/internal/huma/operations/money_rounding_guard_test.go`), with six meta-tests
(`TestMoneyGuard_*`) that construct each violation and prove it is reported, following the pattern
the retention guards set.

⚠ **It is not the AST scan this section originally asked for, and the substitution is deliberate.**
An AST scan asks "does the source mention the rounding helper near this field", which a handler can
satisfy while folding the wrong field, and which an innocent refactor breaks with no behaviour
change. The guard instead drives every one of the ten handlers with a value carrying a long binary
tail and reads the emitted response back by reflection, so what is asserted is the artefact this
ADR set out to remove rather than the shape of the code that removes it.

⚠ **Its limits, stated plainly, because a guard oversold is worse than none.** It classifies every
`float64`/`*float64` reachable on a response body as `roundedTo2dp` or `exactAsProduced` and fails
on any unclassified or stale entry — so it catches a new *field*. It does **not** catch a new
*surface*: an eleventh money-bearing endpoint that never enters its table is invisible to it. It
says nothing about whether a figure *should* have been emitted at all — that is the currency and
`FILTER` machinery, a different mechanism (see the section above on not conflating them). And it
asserts nothing about arithmetic correctness: a handler that folds the right field to 2dp after
computing it wrongly passes.

The same limit applies to this document's own citations. `TestMarkdownCitedTestNamesResolve`
(`backend/internal/testsupport/citations/markdown_citations_guard_test.go`, Rule 1) checks that the
test names cited here resolve to functions that exist — names only. It says nothing about line
numbers, and its own header states that no guard catches a false factual claim. Re-derive before
relying on any assertion here.

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
- `backend/internal/huma/operations/savings_impact.go:84` — the second existing `math.Round`, an
  anonymisation step rather than a precision boundary
- `backend/internal/services/savings_progress_service.go` — where `roundToCents` was, and where the
  ONE surviving fold (the persisted `current_amount` canonicalisation in `RecomputeGoal`) still is,
  with the note explaining why it is a different mechanism
- `backend/internal/money` — `ToCents` / `ToCentsPtr`, the single definition of the 2dp fold, and
  `TestToCents_IsNotDecimalExact_TheADRsStatedLimitation`
- `backend/internal/huma/operations/money_rounding.go` — the boundary itself, one function per
  response surface, with the field-by-field reasoning
- `backend/internal/huma/operations/money_rounding_guard_test.go` —
  `TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge` and its `TestMoneyGuard_*` meta-tests
- `backend/internal/domain/investment.go` (`Holding.GainLossPct`, `Holding.DayChangePct`),
  `backend/internal/domain/holding_snapshot_view.go` (`ComputeGainLoss`) and
  `backend/internal/services/investment_service.go` (`GetPortfolioSummary`) — the five non-repository
  `× 100` percentage sites that reach clients, none of which rounds
- `backend/migrations/000051_create_investment_tables.up.sql`,
  `backend/migrations/000160_create_holding_and_liability_history.up.sql` — the `DECIMAL(18,8)` and
  `DECIMAL(18,2)` columns
- `docs/decisions/2026-09-11-realized-gain-loss-not-supported.md` — the companion decision
