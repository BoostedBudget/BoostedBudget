# ADR: Realized Gain/Loss Is Not Supported, and Says So — Rather Than Being Approximated

**Date:** 2026-09-11
**Status:** Accepted
**Issue:** #1631
**Related:** `docs/decisions/2026-08-24-financial-history-retained-by-default.md` (in particular its
`AMENDMENT (#1631, 2026-09-10)`, which governs the non-ingestion rules cited below)

---

## Context

`GainLossSummary.RealizedGainLossSupported` is a hard `false`
(`backend/internal/repository/investment_analytics_repository.go`, in
`investmentAnalyticsRepository.UnrealizedGainLoss`). It is not a feature flag, not a
capability probe, and not a TODO: it is written as a literal in the returned struct, and the reason
is published to clients in the
operation description registered for `getInvestmentGainLossSummary`
(`backend/internal/huma/operations/investments.go`), which reads:

> `realized_gain_loss_supported` is always false — Plaid does not provide per-lot cost basis and
> the 2-year transaction lookback truncates buy history.

Three tests pin it, one per layer: `TestInvestmentAnalytics_UnrealizedGainLoss_AggregatesAccounts`
(repository), `TestInvestmentAnalyticsService_GetGainLossSummary_UnrealizedOnly` (service), and
`TestInvestmentHandler_GetInvestmentGainLossSummary_UnrealizedOnly` (handler). The last of those
asserts with the message *"realized_gain_loss_supported MUST be false in response — Plaid provides
no per-lot cost basis"*.

This ADR exists because that arrangement is currently defended only by a constant, a sentence in an
API description, and three assertions — none of which explains *why we chose it over the
alternative*, and none of which would stop the next engineer from replacing the constant with a
computation that looks plausible and is wrong. The first thing anyone asks on seeing a
`*_supported: false` field is "so when do we implement it?", and until now there was no durable
answer.

Every claim below was re-derived against the tree on this branch, and anything that could not be
verified is labelled **unverified** in place rather than asserted. That is a description of the
process, not a guarantee of the result, and two review rounds have now proved it:

- The first revision asserted that `TestEveryCitedTestNameResolves` guarded its cited names. False —
  that guard covers Go source only.
- The revision that fixed it then quoted the phrase *"which no guard catches"* as the Markdown
  guard's wording. Also false: that string is verbatim from `cited_test_names_guard_test.go:46`, the
  Go-source guard. The substance was right and the attribution was not.

Both slipped a writing pass and were caught by a reviewer re-deriving them; both landed in the
passage that argues citations must be re-derived. That is the failure mode this repository's guards
are explicitly unable to catch, demonstrated twice on this document. Re-derive before relying on
anything here; see Decision 4 for what is and is not mechanically checked.

---

## What Realized P&L Would Require, and What We Actually Have

Realized gain/loss for a disposal is `proceeds − basis of the specific shares disposed`. That needs
three things. We have one of them.

**1. Per-lot cost basis — absent.** Plaid's holdings product gives a single aggregate figure per
position: `holdings.cost_basis DECIMAL(18, 2)`, whose own schema comment (migration
`000051_create_investment_tables.up.sql`) reads *"Total cost basis in account currency"*. Total, for
the position as it stands today. It is not decomposed into lots, it carries no acquisition dates,
and it is restated by the provider rather than derived by us — `holdings` is a pure snapshot,
DELETE+INSERT-replaced per account on every sync (`holding_repository.go`, `ReplaceForAccount`; see
the header of `backend/internal/domain/investment_reconciliation.go`). A total basis cannot answer
"which shares were sold", so it cannot answer FIFO, LIFO, average-cost, or specific-identification —
and which of those applies is a tax-jurisdiction question we would also then be answering on the
user's behalf.

**2. A complete buy ledger reaching back to each position's opening — absent for older positions.**
`investmentTxnLookbackYears = 2` (`backend/internal/services/investment_sync_service.go`), because
that is how far Plaid's `/investments/transactions/get` will serve. A position opened before that
window has real shares whose acquisition rows we never received and now never can.

This is not speculative; #1631 measured it. `PositionReconciliation`
(`backend/internal/repository/investment_analytics_repository.go`) compares the ledger-implied
position against Plaid's snapshot, and had to introduce `SuppressedPreWindowCount` specifically to
keep window-edge shortfalls out of its drift list. The measurement that matters, though, is in
`backend/internal/domain/investment_reconciliation.go:56-75` — not in the repository file — where
the header records something stronger and worse for us, in the section titled *"PRE-WINDOW BASE PLUS IN-WINDOW CONTRIBUTIONS — NOT ADDRESSED, AND
LIKELY THE DOMINANT POPULATION OF Drifts, NOT A RESIDUAL"*: the suppression proxy is "does this
(account, security) pair have at least one buy in the window", so a position opened years ago and
topped up ever since — dollar-cost averaging, auto-invest, payroll deferral — **passes** the proxy
while still missing its entire pre-window base. That is the modal long-held retail position. The
truncation is therefore not an edge case at the boundary; on real portfolios it is expected to be
most of the affected population.

**3. A complete disposal ledger — present in principle, and made *deliberately less complete* during
#1631.** See the next section, because this is the part that changed.

---

## The Blockers Got Worse During #1631, on Purpose

Two changes in this very issue traded ledger completeness for correctness elsewhere. Both were
right. Both make realized P&L harder, and an ADR that only cited the two historical blockers would
be describing a tree that no longer exists.

**A `securityMap` miss now refuses the row instead of storing it.** In
`mapPlaidInvestmentTransactions` (`investment_sync_service.go`), a transaction that carries a
`security_id` Plaid sent but this page could not resolve is dropped and counted as
`drops.unresolvedSecurity`, rather than stored with a NULL `security_id`. The reason is in the code:
per the schema comment on `investment_transactions.security_id`, NULL *means* "cash-only", so the
old behaviour stored the row **as a lie**, indistinguishable from a real dividend or fee. Refusing is
the correct call — `SyncHoldings` already refuses an account's whole holdings set for the same class
of failure. But the effect on this ADR is direct: **the ledger may now be missing a buy or a sell it
would previously have stored wrongly.** A basis calculation is exactly the consumer that cannot tell
the difference between "no such trade" and "a trade we declined to write".

**The watermark now pins when anything is dropped.** `InvestmentTransactionSyncResult.AnyTransactionDropped`
(`backend/internal/domain/investment.go`) gates the advance of `investments_last_synced_date`: a run
that failed to store rows Plaid delivered leaves the cursor where it is, so the next run re-fetches
the same window. This is what keeps a drop *temporary* rather than permanent, and it is the only
reason the previous paragraph is survivable. It does not make the ledger complete at any given
instant — it makes it eventually complete, for drops whose cause eventually resolves. A row that can never be
**stored** — Plaid sending a permanently malformed date, or a security that never resolves — pins
the cursor indefinitely, and every subsequent run re-fetches the whole window. The code documents
that as a repeated cost rather than a loss, since the upserts are idempotent
(`investment_sync_service.go:713-717`; note the source comment calls this an "undroppable" row,
which reads backwards — the row is one we always drop and never store).

**`exclude_investment_sync` now suppresses ingestion, not just reads.** The #1631 amendment to the
retention ADR authorises never writing an `investment_transactions` row for an account whose owner
flagged its chain as a duplicate of the banking feed. That amendment is careful and correct — it is
scoped to a duplicate the user named, with a durable backfill obligation
(`user_accounts.investment_backfill_requested_at`, migration `000201`) — and it still means the
ledger for an excluded account is, by design, not the complete record of that account's activity.
Worse for us specifically: clearing the flag backfills only `investmentTxnLookbackYears`, so an
account excluded for longer than two years **permanently loses its earliest excluded period**. The
amendment names that cost honestly as availability rather than retention. For a basis calculation
the distinction is academic: the rows are not there, and cannot be obtained.

**The through-line.** Each of these three is a deliberate, defensible choice to make the ledger a
*smaller and more honest* record rather than a larger and partly fictitious one. Realized P&L is the
one calculation that needs the opposite: it needs the ledger to be **complete**, because a missing
buy does not make the answer noisy, it makes the answer confidently wrong in a specific direction.

---

## The Failure This Decision Exists to Prevent

**Shipping a realized-P&L number computed against the visible ledger alone would be wrong, and wrong
in a systematically flattering direction.**

The mechanism is simple enough to state in one line: with no basis for shares acquired before the
window, any implementation that sums in-window proceeds against in-window basis is implicitly
treating the pre-window basis as **zero**, and `proceeds − 0` is the entire sale price booked as
gain. Sell 100 shares bought in 2019 for $50 and worth $90 today, and a window-blind calculation
reports a $9,000 realized gain instead of $4,000. The error is unbounded, it scales with how long
the user has held the position, and it is always an **overstatement** — never an offsetting
understatement, because you cannot have negative missing basis.

Three properties make this worse than an ordinary inaccuracy:

- **It is invisible to the user.** There is no signal in the output distinguishing a correctly
  computed gain from an inflated one. The two are the same JSON field with the same shape.
- **It is worst for our best users.** The longer and more loyally someone has held a position, the
  more of its basis predates the window, and the more the number lies. A brand-new account is
  reported correctly; a ten-year portfolio is reported nonsensically.
- **It is a number people act on.** Users read realized gain as a tax-relevant figure. We would be
  handing them an overstatement to file against. This system is explicitly "suggestions only, no
  money movement" — but a wrong realized-gain figure causes real-world harm without moving a cent.

An unrealized figure does not have this problem and is why it ships: unrealized gain/loss is
computed entirely from `holdings` (today's `institution_value` against today's provider-restated
`cost_basis`), needs no history at all, and its own NULL-basis holdings are excluded by explicit
`FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL)` clauses rather than
being silently treated as zero-basis. That FILTER is the same instinct as this ADR, applied at the
row level: **a missing basis suppresses the figure; it never defaults to zero.** This decision is
that rule, applied to a whole feature.

---

## The Options, With Their Real Costs

### Option A — Keep the honest `false` (**CHOSEN**)

Emit `realized_gain_loss_supported: false`, publish the reason in the API description, and compute
nothing.

- **Cost:** a real capability gap. Realized P&L is table stakes at brokerages and at competitors
  with direct custodian relationships, and every user who wants it is told no. The field is
  currently read by nothing on the frontend (`grep -rni realized apps/web/src` returns no hits) —
  the flag is emitted to API consumers, including the planned AI tool-call surface, and no UI
  presents the gap to a human today. That is a gap in *communication*, not in correctness, and it is
  noted in Consequences below rather than hidden here.
- **What it buys:** every number we emit in this domain is one we can defend. A machine consumer —
  the AI assistant this analytics layer is explicitly designed to feed — can branch on the flag
  instead of being handed a plausible-looking figure it will happily reason about.

### Option B — Per-lot tracking from the first sync forward

Add a lot table, open a lot on every buy/reinvest the ledger delivers from now on, and relieve it on
every sell.

- **What it gets right:** positions *opened after* the feature ships are computed correctly and stay
  correct forever, with no dependence on the provider's restated aggregate basis.
- **Cost 1 — silent about the past, which is the whole problem restated.** Every position already
  open on the day it ships has no opening lot. The implementation must then choose between refusing
  a realized figure for those positions (correct, and means the feature does not work for existing
  users for years) or synthesising an opening lot (which is Option C wearing a schema).
- **Cost 2 — it inherits all three of the #1631 completeness trades above.** A refused
  `securityMap` row is a lot that never opens; an excluded account's period is a lot history that
  does not exist; a pinned watermark means lots arrive late. A lot ledger built on a ledger that
  *correctly* refuses rows is a lot ledger with holes, and a hole in a lot ledger relieves the wrong
  shares.
- **Cost 3 — it is the start of a tax-lot system, not the end.** Disposal method (FIFO/LIFO/average/
  specific-ID), wash sales, corporate actions, splits, in-kind transfers. The reconciliation header
  already documents that `type='transfer'` covers both splits and ACATS transfers, that options
  assignment/exercise is a known uncovered gap, and — flagged there as **unverified** — that Plaid's
  docs do not state a sign convention for transfers at all. Every one of those is a lot-adjustment
  event. Getting them wrong is not a display bug; it is a wrong basis carried forward indefinitely.
- **Verdict:** viable, and the right shape *if and when* we decide to pay for it. Out of scope for
  #1631, which is a bugfix issue for activity types and a portfolio 500.

### Option C — Approximate: compute realized P&L from what the window shows

Rejected outright. This is the zero-pre-window-basis failure described above. Not "shipped with a
caveat", not "shipped behind a disclaimer" — a disclaimer does not make the number less wrong, and
the AI tool-call consumer does not read disclaimers.

### Option D — Ask the user for their basis

A manual per-position basis entry would unblock the calculation without the provider.

- **Cost:** it relocates the accuracy problem into the user's memory and makes us the system of
  record for a tax input we cannot verify, while doing nothing about disposals we never saw. Not
  chosen, and not currently designed. Recorded so the next person does not have to rediscover that
  it was considered.

---

## The Decision

1. **`RealizedGainLossSupported` stays a hard-coded `false`** — a literal field in the
   `domain.GainLossSummary` value that `investmentAnalyticsRepository.UnrealizedGainLoss` returns
   (`investment_analytics_repository.go:690`). It is not made configurable, not made
   conditional on data availability, and not removed from the response — a consumer must be able to
   distinguish "we do not support this" from "the field is absent because something broke".
2. **The reason stays published** in the `getInvestmentGainLossSummary` operation description, which
   flows through `api/openapi.yaml` into the generated client
   (`packages/api-client/src/endpoints/analytics/analytics.ts`). The reason travelling with the API
   is the point: a consumer reading the generated client sees why without reading this repository.
3. **No realized-P&L figure may be emitted from an incomplete ledger.** If a future change computes
   realized gain, it must be able to state, per position, that it has basis for every share it is
   relieving — and refuse (nil, not zero) for positions where it cannot. This is the same rule as
   the `FILTER (WHERE h.cost_basis IS NOT NULL ...)` clauses in `UnrealizedGainLoss` and the same
   rule as the mixed-currency refusal in `backend/internal/domain/investment_currency.go`: **a
   figure that cannot be computed is withheld, never defaulted.**
4. **The three pinning tests stay.** They are the enforcement.

⚠ **What checks this document, and what it does not.** The guard that scans *this file* is
`TestMarkdownCitedTestNamesResolve` (`backend/internal/testsupport/citations/markdown_citations_guard_test.go`,
Rule 1), scoped to tracked `*.md` at the repository root or under `docs/`. Its Go-source sibling,
`TestEveryCitedTestNameResolves`, does **not** cover Markdown — its own header says so explicitly
("SCOPE: tracked Go source only"), and an earlier revision of this ADR named it here and was wrong.
The Markdown guard has three rules: cited test names must resolve (Rule 1), a `Decision N`
cross-reference must name a decision that exists (Rule 2), and a backticked repository path must
resolve on disk (Rule 3). It is a ratchet over a baseline rather than a ban.

**None of the three checks whether a claim is true**, and that guard's header says so in its own
words, under a heading reading "WHAT IS NOT MECHANICALLY CHECKABLE, PLAINLY". Of the three
documentation defects that motivated it, two were false factual claims about behaviour —
*"No regexp reaches it. The only control that has actually worked on this branch is a reviewer
reproducing the claim."* It closes with: *"It does not close 'the ADR was wrong', and no guard in
this repository does."*

So every non-name, non-path assertion in this document — every line number, every quoted comment,
every statement about what the code does — is defended by the reader re-deriving it, and by nothing
else.

---

## What Would Have to Change to Revisit This

Any *one* of these on its own is insufficient. Realized P&L becomes computable when the first is
true, and defensible only when the rest are:

- **A basis source that reaches each position's opening.** Either a provider that supplies per-lot
  basis with acquisition dates, or a lot ledger (Option B) that has been running for longer than the
  oldest position it reports on — plus an explicit, per-position rule for what happens before that.
- **A completeness assertion the calculation can rely on.** Today `PositionReconciliation` is
  explicitly a *diagnostic*: its own header says it never writes and that its `Drifts` list is not
  yet fully actionable. A realized-P&L feature needs the inverse — a positive statement that a
  position's ledger is complete over the interval being reported — and the reconciliation header
  already names the shape that would do it (anchor the position to a window using
  `holding_snapshots`, migration `000160`, rather than to its whole lifetime).
- **The #1631 completeness trades accounted for, not ignored.** An excluded account's unbackfillable
  period and a permanently unresolvable security must make the calculation refuse for the affected
  positions, not quietly compute around them.
- **A stated disposal method**, and a decision about whether we are willing to be read as giving tax
  guidance.

Until then, `false` is not a placeholder. It is the answer.

---

## Consequences

### Positive

- No number in the investment gain/loss surface is defended by "it's approximately right".
- Machine consumers get a branchable capability flag rather than a figure with hidden semantics.
- The reason lives in the API contract, so it survives a reader who never opens this repository.

### Negative / Risks

- **A real feature gap, accepted.** Users who want realized P&L do not get it, and will not get it
  from a small change.
- **The gap is not surfaced to humans.** Nothing in `apps/web/src` reads
  `realized_gain_loss_supported` today, so the UI neither shows the capability nor explains its
  absence. That is a follow-up, not a defect in this decision: surfacing a `false` is cheap and
  safe, whereas surfacing a wrong number is neither.
- **The constant is easy to flip.** `false` is one token. The three pinning tests and this document
  are what stand between it and an Option C implementation; both must be read together, because the
  tests say *what* and only this says *why*.
- **Cost recorded, since #1631 chose to pay it:** folding this ADR into the bugfix branch roughly
  doubled the PR. The user was shown that trade and took it deliberately, so the scope is a decision
  rather than drift.

---

## References

- `backend/internal/repository/investment_analytics_repository.go` — `UnrealizedGainLoss`
  (the hard `false`), `PositionReconciliation`, `PositionReconciliationSummary`
- `backend/internal/huma/operations/investments.go` — `getInvestmentGainLossSummary` registration
  and description
- `backend/internal/domain/investment_portfolio.go` — `GainLossSummary.RealizedGainLossSupported`
- `backend/internal/domain/investment_reconciliation.go` — the two-year window's measured effect,
  `SuppressedPreWindowCount`, and the enumerated gaps
- `backend/internal/domain/investment.go` — `InvestmentTransactionSyncResult.AnyTransactionDropped`
- `backend/internal/services/investment_sync_service.go` — `investmentTxnLookbackYears`,
  `mapPlaidInvestmentTransactions`, watermark pinning
- `backend/migrations/000051_create_investment_tables.up.sql` — `holdings.cost_basis`
- `docs/decisions/2026-08-24-financial-history-retained-by-default.md` § `AMENDMENT (#1631,
  2026-09-10)` — non-ingestion, and the two-year backfill bound
- `backend/internal/testsupport/citations/markdown_citations_guard_test.go` —
  `TestMarkdownCitedTestNamesResolve`, the guard that checks the test names cited in this file
  (names only, not truth)
- `docs/decisions/2026-09-11-money-representation-float64-and-the-rounding-boundary.md` — the
  companion decision on how these figures are represented once computed
