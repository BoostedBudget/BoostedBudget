# Household scope is a share-gated subset

- **Date**: 2026-09-22
- **Issue**: #1706
- **Status**: Accepted
- **Code**: `backend/internal/repository/analytics_repository.go` (`householdGrantsCTE`),
  `backend/internal/repository/holding_repository.go` (`GetByUserIDWithSecurities`),
  `backend/internal/services/investment_service.go` (`GetPortfolioSummary`),
  `apps/web/src/features/dashboard/components/ScopeSegmentedControl.tsx` (the share-coverage caption).

## Why this is an ADR and not a comment

Issue #1706 was filed as "my net worth shows times 2". Measurement refuted that premise: the
household figure is a correct sum of a strict subset, and the report was a visibility problem, not a
double-count. The ruling that produced the number, the consent posture behind it, the privacy rule
the disclosure must obey, and the day-change refusal are each load-bearing and each easy to
re-litigate from the symptom alone. They live here so the next reader does not re-derive them from a
screenshot.

## (a) Semantics: `scope=household` sums only explicitly shared accounts

`scope=household` sums ONLY accounts the caller has explicitly shared with the household:
`household_account_shares.visibility = 'shared'`, per `householdGrantsCTE`
(`analytics_repository.go:712-735`) and the #1369 decision #3 ruling recorded at
`analytics_repository.go:648` ("Household inclusion is governed SOLELY by the share"). `is_hidden`
on `user_accounts`/`accounts` is the owner's own personal-view control and is deliberately ignored
here; it must not travel into a household view (`account_repository.go:1039-1046`).

**Consequence, and the reason #1706 was filed: a single-member household can show a LARGER net
worth than personal scope.** The household leg excludes every unshared account, and when an unshared
account is a large liability, removing it raises the total.

Measured on the reporter's account, same day (2026-09-22):

| | personal scope | household scope |
|---|---|---|
| net worth | 88,501.10 | 196,156.00 |
| accounts in breakdown | 19 | 9 |

The household figure is arithmetically correct. It is the 9 shared accounts, each counted once:

```
88,501.10 + 368,283.83 (unshared mortgage removed) - 260,628.93 (unshared assets removed) = 196,156.00
```

The original report ("net worth times 2") was a **subset-visibility problem, not a double-count**.
Nothing is summed twice; the personal view simply includes the unshared $368,283.83 mortgage while
the household view does not. The DB census confirmed it: 9 open share rows, zero accounts appearing
more than once, and zero household-only accounts (the household leg is a strict subset of the
personal leg).

Pinned by `TestHoldingsHouseholdScope_SubsetOfHouseholdNetWorthInvestmentLeg`, which asserts the
holdings total stays within the net-worth investment leg and that every holding's account is in the
household net-worth set.

## (b) The consent posture: sharing covers balance AND holdings

**Sharing an account makes it visible to the household's ACTIVE MEMBERS for its balance AND its
holdings.** The share is the consent act; it is not scoped to one read. This is the basis for
extending `scope` to the investments holdings and portfolio-summary reads, and it is why the
household holdings read is gated on the same share predicate as the balance reads rather than on a
separate consent.

The household holdings read replaces the personal predicate (`WHERE h.user_id = $1`) with
`WHERE h.user_account_id IN (SELECT user_account_id FROM household_accounts)` over
`householdAccountsCTEForBalancesAsOf()`. The personal branch is unchanged; its output is
JSON-equal to the committed golden fixture
(`backend/internal/services/testdata/personal_baseline.json`).

Pinned by:
- `TestHoldingsHouseholdScope_UnsharedAccountMustNotAppear` (an unshared account's holding is
  ABSENT under household scope),
- `TestHoldingsHouseholdScope_RevokedShareExcluded_ActiveSiblingStillPresent` (a revoked share is
  excluded while an active sibling survives),
- `TestHoldingsHouseholdScope_HiddenSharedStillVisible_HiddenUnsharedAbsent` (inclusion is
  share-governed, not `is_hidden`-governed),
- `TestHoldingsHouseholdScope_JointPairSharedCopyCountedOnceUnsharedCopyAbsent` (a joint account's
  shared copy is counted once, its unshared copy is absent),
- `TestHoldingsHouseholdScope_RelinkChainCountedOnce` (a re-link chain's money appears once),
- `TestInvestmentPersonalBaseline_Unchanged` (the personal reads are unchanged against the golden
  baseline).

## (c) The disclosure's privacy rule: caller-scoped only

The household view's share-coverage caption is **caller-scoped only**. It states how many of the
CALLER'S OWN accounts are shared ("N of your M accounts are shared"). It must never enumerate,
count, name, or imply another member's unshared accounts, and it must never attribute any part of
the household total to a member.

The caption's numerator is the caller's own accounts whose `user_account_id` is in
`listMyAccountShares` with `visibility === 'shared'` and `revoked_at == null`. Its denominator is
the caller's own account list, the same list the dashboard renders.

**The honest limitation, recorded so the guarantee is not overstated.** The household total is
already on screen, so the caption does narrow the other member's **shared** contribution by
subtraction: a reader who knows the household total and the caller's own shared total can infer the
remainder. What the caption does **not** reveal is anything about the other member's **unshared**
accounts. Those never enter the household total, so no subtraction can reach them. The rule is a
bound on what the caption may state, not a claim that the household total is opaque.

## (d) The day-change decision: omit it under household scope

Under household scope the portfolio summary OMITS `day_change` and sets
`day_change_unavailable_reason: "household_scope"`.

Reason: the day change is derived from `portfolio_snapshots` via `GetLatestBefore`
(`portfolio_snapshot_repository.go:265`), whose query is user-scoped (`WHERE user_id = $1`). There
is no household portfolio snapshot, so there is no household day change to derive. Silently
returning the requesting member's personal day change under a household label was rejected: it
would present one member's movement as the household's.

The service skips the snapshot fetch under household scope, leaves `DayChange`/`DayChangePct` nil,
and sets the reason (`investment_service.go:216`; `domain.DayChangeUnavailableHouseholdScope`). The
widget omits the Day Change card entirely rather than rendering the `--` that means "missing data"
on the personal path.

Pinned by `TestInvestmentService_GetPortfolioSummary_HouseholdOmitsDayChange` (nil day change plus
the reason, and `GetLatestBefore` never called).

## Known risk: the share endpoint takes a row lock on a read path

`GET /api/v1/households/me/account-shares` reaches `household_account_share_service.go`, which takes
a `FOR UPDATE OF hm` lock on the caller's own active `household_members` row
(`household_repository.go:265`, `LockActiveMembershipForUser`). That lock exists to serialize
against `RemoveMember` (the #1368 share-visibility TOCTOU guard), but it means a READ path holds a
row lock.

A transient 500 was observed once during investigation; 9 of 9 subsequent calls returned 200 with an
identical payload. It was characterised as a cold-start/infra flake and kept under watch rather than
filed. The latent risk is real and unproven: a concurrent household write could in principle abort
the read with an unmapped `40P01` (deadlock) and surface as a 500. Any such 500 is logged to Sentry
via `LoggingMapError`. Revisit on a second warm occurrence or a Sentry event.

## Known risk: the caption counts accounts, not user-accounts

The caption counts ACCOUNTS, not user-accounts. `listMyAccountShares` returned 19 rows while
`getUserAccounts` (hidden excluded by default) returned 17. The denominator uses the account list
the dashboard renders (hidden excluded), so the caption matches what the user sees elsewhere. The
numerator is joined to share state by `user_account_id`, excluding revoked and hidden. The two
numbers are deliberately drawn from the same rendered universe; do not "fix" the 19-vs-17 gap by
switching the denominator to the share endpoint's row count.