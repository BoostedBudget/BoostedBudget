# #1652 RevenueCat centralization implementation

Status: implementation in progress. No production cutover has occurred.

## Agreed scope

Existing Go backend and React SPA only. RevenueCat is the authority for paid,
60-day card-free trial, and additive complimentary access. Paddle remains the
billing provider. Keep our subscription page and plan-change controls; use a
RevenueCat paywall for purchases and Paddle portal for billing self-service.
Native purchases and issue #1664 are excluded. Financial history is retained.

## Tasks

- [x] RevenueCat HTTP client, verified webhooks, configuration and catalog validation.
- [x] Durable projection, webhook inbox, grant operations, serialized refresh and repair.
- [x] Registration trial and audited admin trial/complimentary grant management.
- [x] Shared effective access and conversion of request/background consumers.
- [x] SPA paywall, subscription summary, retained paid-plan changes and billing portal.
- [x] Admin health, webhook explorer, operation recovery and DLQ integration.
- [x] Idempotent prelaunch bootstrap and staging/production connectivity runbook.
- [ ] Simplifier, full verification, adversarial review and issue-linked PR.

### Implementation checkpoints

- [x] HTTP client and signature verification: TDD, race tests, spec and quality reviews.
- [x] Backend credential validation and environment templates: TDD, race tests, both reviews.
- [x] Durable domain/repository/service: unit/race and initial PostgreSQL integration tests; spec and quality reviews.
- [x] Household access adapter and fail-closed subscription resolver: tests and both reviews pass; production injection complete.
- [x] Transactional registration trial-intent adapter: tests and both reviews pass; production injection complete.
- [x] Queue synchronization/repair and replay uniqueness: focused tests and both reviews pass.
- [x] Production authority wiring and legacy worker draining: server/job tests and both reviews pass.
- [x] Account summary and profile authority: focused tests and both reviews pass.
- [x] Admin backend grants, metrics, receipt recovery and diagnostics: focused tests and both reviews pass.
- [x] Household/background consumers and compatibility projection: focused/race and PostgreSQL tests pass; spec and quality reviews complete. Added final cascade and automatic-offboard grant fencing regressions.
- [x] Checkout coordination and retained Paddle billing: unit and PostgreSQL tests pass; spec and quality reviews complete.
- [x] SPA paywall and recovery: focused tests pass; spec and quality reviews complete, including stale-cache and identity-switch regression fixes.
- [x] GDPR erasure fencing and complete provider cleanup: unit/race and PostgreSQL tests; spec and quality reviews complete, including asynchronous provider deletion confirmation and customer-first final cascade locking.
- [x] Bootstrap catalog/environment checks and fixed-expiry inventory: unit and PostgreSQL tests; spec and quality reviews complete. Added encrypted-identity, last-customer failure, modern-operation preservation, and environment/product duplication coverage.
- [x] Admin dashboard UI: 487 focused tests, lint and typecheck pass; spec and quality reviews complete. Desktop diagnostic width regression fixed with a failing-then-passing test.
- [x] RevenueCat queue in system health: backend tests and both reviews pass. Unavailable metrics are explicitly marked and degrade health.
- [x] Deployment/public SDK key wiring and runbook: configuration regression tests and generated API verification pass; live sandbox acceptance is an operator cutover prerequisite.
- [x] Migration rollback safety: real PostgreSQL regressions and both reviews pass; all five migration suites pass in a task-owned scratch database.
- [x] Integration guard follow-up: migrated freshcheck through 210, updated webhook auth inventory and retention table coverage, and verified four-table GDPR cascade plus cross-user actor anonymization. Focused guards pass.
- [x] Final code-simplifier review: complete, including the follow-up on nullable diagnostics, checkout cancellation and E2E fixture helpers.
- [x] Whole-diff independent adversarial review: four findings fixed and re-reviewed (target-user analytics consent, promotional billing household admission, account-switch checkout cancellation, nullable health metrics).
- [x] Nullable diagnostics: real-null frontend regressions and Huma schema validation now cover summary classes, effective/own access, state and health metrics.
- [x] E2E authority fixtures: test-only, exact authenticated identities, disposable-database guards, unchanged-state freshness updates and real PostgreSQL refusal regressions; no production fallback.
- [x] Checkout bundle isolation: SDK moved out of eager vendor, matcher and production request regressions added, independently reviewed. Bundle gate passes: vendor 449.01/460 kB, lazy SDK 219.72/264 kB, landing 535/542 KiB with unchanged landing limit. Admin/shared budgets follow measured baseline +20%.
- [x] E2E harness final review: negative database regressions finish before heartbeat startup; teardown checks process liveness and final database invariants. Both timing findings fixed and independently re-reviewed.
- [x] Final gate reruns after adversarial fixes: backend full suite and vet pass; final serial integration: 7,927 passes, zero failures, 28 explicit skips. All 26 shell suites pass. Final E2E: 348 passed/135 skipped, including lazy SDK production smoke and final fixture invariants. Frontend: 7,723 passed/4 skipped; production typecheck, unchanged 1,908 test-error ratchet and lint pass. Both backend lint modes pass `--new-from-rev` against staging; four untagged/eight integration-tag findings remain in unchanged repository files. Mechanical lint cleanup independently reviewed; no further simplification warranted.
- [ ] Issue-linked PR and remote CI follow-through.

## Invariants

- Clerk user IDs identify RevenueCat customers; never match by email.
- Distinct production/nonproduction projects. Server credentials never reach the SPA.
- Five `paid_<tier>`, five `comp_<tier>` and `trial_premium` entitlements isolate
  grant revocation. Only paid entitlements attach to Paddle products.
- Active household coverage keeps existing precedence; otherwise highest active
  paid/comp tier wins, paid wins presentation ties, then trial. Billing remains
  visible independently of complimentary benefits.
- Registration atomically records trial eligibility and a fixed 60-day expiry.
  Retrying or replaying a grant never starts a new trial or revives superseded work.
- Local operation records authorize remote requests, never access. Access is based
  on RevenueCat-confirmed projections, not Paddle events or Clerk metadata.
- Webhook ingestion and queued work are atomic. Events request a fresh customer
  snapshot; event order never directly changes access.
- One customer refresh path serves webhooks, purchase confirmation, expiry, admin
  refresh and repair. Freshness is 15 minutes; unknown access preserves reads and
  billing recovery, denies protected writes with 503, and defers external cleanup.
- Cleanup requires fresh aggregate coverage confirmation plus existing occurrence
  guards and per-item rechecks. No financial-history deletion is introduced.
- Grant replacements have durable stages and readback; paid access survives any
  intermediate promotional operation failure.
- Plan changes compare paid products; seat preflight evaluates the proposed paid
  tier together with existing grants. Preserve external over-capacity grace.
- Admin replay retries existing failed processing, not delivery ingestion. DLQ
  deletion cannot erase grant/audit records. Every job registers producer/replay
  uniqueness consistently. Rejected authentication is distinct from processing failure.
- Admin metrics distinguish unavailable, known zero, and zero-denominator rates;
  delivery attempts are distinct from events, enqueue from applied access.

## Verification

Follow TDD for each task and review spec then code quality. Cover HTTP pagination,
errors/signatures, concurrent grants/refresh, fixed trial expiry, additive grants,
expired snapshots, cancellation/refund overlap, household races, safe DLQ replay,
admin authorization, PII masking and SPA account changes/payment failure recovery.
Run backend and frontend unit suites, frontend typecheck/lint, E2E, shell guards,
serial integration suites against freshcheck with both Redis URLs, migrations and
bundle checks. Validate real Paddle sandbox ingestion through RevenueCat before
cutover. Do not substitute Paddle webhook simulation for end-to-end validation.

## Release documentation

The PR must contain explicit staging and production setup instructions: RevenueCat
projects, Paddle integration, product/entitlement/offerings/paywall setup, API key
permissions, server secrets, webhook URL/authentication/signing, SPA build-time
public key, deployment configuration, bootstrap and acceptance checks. No actual
credentials may be committed or included in the PR. Preserve existing trial ends,
eligibility, comps and audit/history during prelaunch bootstrap. Unexpected live
paid subscribers require migration review before direct cutover.
