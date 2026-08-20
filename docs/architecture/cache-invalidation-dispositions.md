# Cache-invalidation dispositions

How BoostedBudget proves that a write to Postgres cannot leave a stale value in the Redis cache —
and, just as importantly, what that proof does *not* cover.

This document is the durable home for the reasoning that used to live in two files that never
existed. Guard comments cited `task-10b-3-report.md` and `task-10b-2a-iii-report.md` as "the actual
deliverable"; neither is in the repository, in any commit. If you were sent here by a code comment
looking for those, this is the replacement.

## Why this exists

The cache holds personal financial data. The dangerous failure is not a cache miss — it is
**silently serving stale money**. `internal/cache`'s package doc states that as the rule every
design decision there traces back to.

A cache is only as correct as its invalidation, and invalidation is easy to get *almost* right: you
bump the user who made the request and move on. That is wrong whenever a write touches somebody
else. Two live bugs of exactly that shape have been found in this codebase:

| Bug | Shape |
|---|---|
| `AccountRepository.ExpireAccountsByFingerprint` | Expires a **different** user's accounts (deliberately — no `user_id` predicate), while only the acting user's generation was bumped. |
| `MerchantRepo.GetOrCreateMerchantsWithNormalize` | Wrote the **global** `merchants` row during one user's Plaid sync, changing the merchant name rendered in every sharing user's cached read. |

Neither was found by review. The first was found by an independent gap-analysis pass; the second
while triaging the backlog the first one exposed. Both sat among entries nobody was looking at.

## The cache surface

Registry: `internal/cache/namespace.go`.

| Namespace | TTL | Scope kind | Notes |
|---|---|---|---|
| `analytics` | 120s | user *or* household | scope chosen by `cache.ScopeFor(userID, filters.HouseholdScope)` |
| `analytics_rows` | 300s | user / household | carries merchant names and descriptions, so GDPR caps it shorter |
| `dashprefs` | 300s | user | |
| `identity` | 600s | **clerk_id** | a scalar clerkID→UUID mapping, nothing else |
| `hhscope` | 30s | user | default **off**, zero consumers |
| `entitlement` | 60s | user | **hard-disabled**; no env var, control key or setter can enable it |

There are exactly 19 `cache.GetOrLoad` call sites: 17 in `services/analytics_service_cached.go`,
one in `dashboard_preferences_service_cached.go`, one in `identity_resolver_cached.go`.

**Those reads touch exactly twelve tables:** `accounts`, `balance_snapshots`, `budget_categories`,
`budgets`, `custom_categories`, `household_account_shares`, `merchant_name_overrides`, `merchants`,
`transactions`, `user_accounts`, `user_dashboard_preferences`, `user_identities`.

Two of those — `merchants` and `merchant_name_overrides` — enter only through the
`merchantSortJoins` const in `internal/repository/merchant_sql_helpers.go`. They never appear as
literal text in `analytics_repository.go`, so a naive grep of that file will tell you they are not
read. They are.

Generations are **random tokens, not counters** (`internal/cache/generation.go`). Upstash's
non-configurable `optimistic-volatile` eviction can drop a no-TTL generation key, and a counter
reset to 1 would resurrect every old data key.

## How invalidation actually happens

`internal/huma/middleware/cache_invalidation.go` bumps only when **all** of these hold: the
operation declares a non-empty `Security`; the method is not GET/HEAD/OPTIONS; the response status is
outside `[400,500)`; and `UserIDKey` is present. It then bumps `ScopeUser(actor)` and, if
resolvable, `ScopeHousehold(actor's household)`.

Everything else is an explicit bump in a service or a River job — 17 call sites, listed in the
guard's own comments.

**Nothing in production ever bumps `ScopeClerkID`.** Only `internal/cache/purge.go` references that
scope tag, on GDPR deletion. The `identity` namespace is therefore invalidated only by a purge or by
its 600s TTL — which is fine today, but it means "the HTTP middleware covers it" is *structurally
false* for anything touching that namespace.

## The six conditions

`CoveredByHTTPMiddleware` is sound only if all six hold. Four of them exist because a plausible
three-condition version was wrong.

| | Condition | Why it is not obvious |
|---|---|---|
| **C1** | Every production call site is a secured, non-GET Huma operation | A single River-job or webhook caller voids it. Jobs frequently reach services through **job-local narrow interfaces**, so searching by type name misses them — search by method name. |
| **C2** | The set of `user_id`s the statement can affect is confined to the actor and their household | This is a **blast-radius** claim, not "does the SQL mention user_id". An admin operation carries `user_id = $n` where `$n` is the *target* while the middleware bumps the *admin*; batch writes take per-row `UserID` with no single-tenant assertion. |
| **C3** | The affected namespace's scope kind is one the middleware bumps | It bumps `ScopeUser` and `ScopeHousehold` only, so anything reaching `identity` (`ScopeClerkID`) is not covered. |
| **C4** | The route's `huma.Register` runs *after* the cache-invalidation `UseMiddleware` line | huma **snapshots the middleware chain at registration time** (`chain.go`), so a route registered earlier silently gets no middleware at all. This was violated by the deletion routes — see issue #1560, where it left `POST /api/v1/user/delete` returning 401 unconditionally. |
| **C5** | The write happens inside the invalidation window | The **Clerk middleware itself writes**: auto-registration creates profile and identity rows, and `UpdateLastUsed` runs in a detached goroutine — on any request, including GETs, which the bump skips. |
| **C6** | Every affected scope ID is captured *before* the mutation | A membership row is gone after commit, so a post-commit lookup cannot recover who was affected. `services/household_service.go` captures member IDs first for exactly this reason. |

Two limits are accepted rather than solved, and should be stated whenever someone claims middleware
coverage:

- **The 4xx skip is an assumption.** A handler that commits and then returns 409/422 gets no bump.
  The middleware's own test pins this as an assumption; the same file argues, for 5xx, that a status
  code cannot distinguish "failed before commit" from "wrote then failed". That argument applies to
  4xx verbatim.
- **`Bump` is best-effort.** It returns nothing and swallows Redis failures (logged and counted); the
  household lookup error is logged and dropped. So `CoveredByHTTPMiddleware` means "probably", never
  "guaranteed".

## The four dispositions

Every mutating repository method carries one, in `repoMethodDispositions`
(`internal/repository/cache_completeness_repo_test.go`).

- **`dispositionExplicitBump`** — a named production file bumps for this write. Detail mandatory.
- **`dispositionCoveredByHTTPMiddleware`** — all six conditions above. Detail mandatory.
- **`dispositionNoCacheImpact`** — a human established that no cached read can observe the write.
  Detail mandatory. **This is the dangerous direction**: a wrong one is worse than a missing bump,
  because the guard then records it as reviewed and nobody looks again.
- **`dispositionDerivedNoCacheImpact`** — the same conclusion **proven mechanically**, and it must
  carry *no* detail.

There is deliberately **no "unclassified" kind**. It existed, accumulated 127 entries, and was
skipped rather than failed — so it blocked nothing and grew for months. Issue #1537 classified all
127 and then deleted the enum value in both this guard and the job guard, so the category cannot be
expressed at all.

## Derived, not narrated

`cache_completeness_write_set_test.go` derives two sets and intersects them:

```
cached-read tables  ∩  method's write tables  ==  ∅   ⇒   NoCacheImpact
```

A derived entry has no prose to rot, and it **breaks** the day someone adds a cached read over a
table it writes. Prose cannot do that. 80 of the 127 were discharged this way.

Both sides expand package-level SQL constants and follow intra-package delegation, because neither
side is visible from literal text alone. Getting the write side to resolve every method required
four hops, each found by an actual false negative:

1. **Package-level SQL consts** — `InvalidWebhookAttemptRepository.Record` executes one.
2. **Unexported helpers** — `AuditLogRepository.Log` has no SQL of its own; the `INSERT` is in
   `writeAuditLogEntry`. (This is the method the guard's original verb-prefix matcher missed
   entirely, and the reason a name-based enumeration can never be complete.)
3. **Same-receiver exported delegation inside a transaction** —
   `budgetRepository.CreateWithCategories` rebinds `&budgetRepository{db: tx}` and calls
   `txRepo.Create`.
4. **Paren-aware body finding** — taking the first `{` after the function name breaks when the
   signature contains one, so `fn func() (interface{}, error)` truncated a body to the empty braces
   of `interface{}`.

### It must fail closed

An unresolved write set is indistinguishable from an **empty** one, and an empty set is trivially
disjoint from anything. A permissive extractor would therefore manufacture `NoCacheImpact` for
methods that really do write cached tables — the exact wrong answer, arrived at confidently. So an
unresolved write set is a hard failure with **no exemption list**. Teach the extractor the missing
hop; do not exempt the method.

## What none of this proves

Read this before trusting a green run.

- **Table granularity, not column.** A write to a cached-read table lands in human review even when
  the specific column is never projected. That is the safe direction, but it means "in the overlap
  set" does not imply "actually matters".
- **Per method name, not per call site.** Where one method is called from places with different
  coverage, the entry picks one disposition; the detail documents the split, but the guard cannot
  enforce per-call-site correctness.
- **Not new mutations inside existing methods.** The guard notices a new exported *method name*. It
  does not notice a second `UPDATE` added inside an already-classified method. Closing that needs
  per-entry declared write-sets asserted against observed ones — deliberately not built.
- **Interfaces only, non-recursively.** Exported mutating methods on concrete types with no
  interface are invisible: all of `DeadLetterJobRepository` (including the dynamically-composed
  `DeleteByFilters`), `linkedItemRepository.ClaimItemForCleanup`, and seven `profileRepository`
  methods including the cross-user `UPDATE users` sweeps `StampMissingPlaidCleanupMarkers`,
  `ClearCompletedPlaidCleanupMarkers` and `ClearMarkersOnEligibleStatus`. `users` is not a
  cached-read table, so cache impact is nil — but the enumeration gap is real.
- **Runtime SQL composition.** SQL assembled by string concatenation across function boundaries is
  invisible to a static scan.
- **`ExplicitBump` proves a call exists, not that it is well-founded.**
  `PortfolioSnapshotRepository.Upsert` is the standing counterexample: the bump runs, but it
  invalidates a namespace nothing about that write populates (issue #1562).
- **A green guard is not proof of invalidation correctness.** It proves enumeration completeness,
  mandatory reasoning where reasoning is the basis, and mechanical disjointness where that is the
  basis. Closing #1537 does not mean invalidation is proven complete.

## Behavioural evidence

Static classification is not the whole story, and one existing harness is weaker than it looks:
`analytics_cache_parity_integration_test.go`'s `Bump` field is documented as "the production
invalidation hook" but resolves to a **test-authored** `gens.Bump(...)`, and its `Mutate` writes raw
SQL straight to the pool. It proves "if something bumps, the cached read updates" — it can never
prove "the production path bumps".

`services/merchant_normalize_cross_user_integration_test.go` is the template for the opposite shape:
seed two users sharing a row, drive the **real** repository write, bump only the user production
bumps, and assert the other user's cached read matches ground truth. It failed before the merchant
fix and passes after, and both of its assertions were mutation-probed independently — including one
probe that caught the test comparing against the warm value, which is exactly what a stale cache
returns and would have passed under the bug.

## Adding a mutating repository method

1. Add it to its interface. The guard fails: a mutating method with no entry.
2. Work out the write set. If the extractor cannot resolve it, teach the extractor.
3. If the write set is disjoint from the twelve cached-read tables, use
   `dispositionDerivedNoCacheImpact` with **no** detail.
4. Otherwise decide between `ExplicitBump`, `CoveredByHTTPMiddleware` (all six conditions) and
   `dispositionNoCacheImpact`, and write a detail that cites `file:line` and says what it does *not*
   prove.
5. If the blast radius escapes every bump that covers it, that is a bug — fix it, RED-first, rather
   than describing it.
