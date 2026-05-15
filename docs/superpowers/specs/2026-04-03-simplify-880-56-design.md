# Design: Simplify & Close Issues #880 and #56

**Date:** 2026-04-03
**Issues:** #880 (Replace in-memory subscription limit tracking), #56 (Rate Limiting Middleware)
**Decision:** Remove dead tier-based execution limit code; close rate limiting as already implemented.

## Context

All premium tiers are now identical with `executions_per_day: -1` (unlimited). There is no free tier. Redis is not used. The subscription limit tracking system (#880) is dead code that contradicts the actual configuration. Rate limiting (#56) is already fully implemented as per-IP middleware.

## Issue #56: Rate Limiting Middleware -- CLOSE, NO CODE CHANGES

### Current State (Already Done)
- `backend/internal/middleware/rate_limit.go` -- per-IP token bucket via `golang.org/x/time/rate`
- 100 req/min regular, 10 req/min auth/webhook endpoints
- `X-Forwarded-For` support for Fly.io reverse proxy
- `Retry-After` header on 429 responses
- Background cleanup goroutine for stale entries
- 7 unit tests passing
- Wired as middleware #3 in `cmd/server/main.go`

### Action
Close issue #56 with a comment documenting the existing implementation. No code changes required.

## Issue #880: Remove Dead Subscription Limit System

### Problem
`SubscriptionLimitAdapter` (in-memory map) tracks daily rule execution counts using stale tier names (`free`, `premium_lite`) while the real config has all tiers set to unlimited. The adapter:
- Hardcodes everyone to 10/day (`premium_lite` default)
- Loses state on restart
- References tier names that don't exist in `subscription_tiers.yaml`
- Is never needed because `executions_per_day: -1` for all tiers

Meanwhile, the DB already persistently tracks executions via the `rule_executions` table and `GetTodayExecutionCount()`.

### Solution: Full Removal

Remove the entire `SubscriptionLimitChecker` system and all dead tier-limit UI code.

### Backend Changes (10 files)

#### DELETE (2 files)
| File | Lines | What |
|------|-------|------|
| `backend/internal/workers/subscription_limit_adapter.go` | 120 | In-memory adapter with stale tier limits |
| `backend/internal/workers/subscription_limit_adapter_test.go` | 108 | Tests for the adapter |

#### MODIFY (8 files)

**`backend/internal/workers/cron_trigger_provider.go`**
- Remove `SubscriptionLimitChecker` interface definition (lines 27-31)
- Remove `subscriptionSvc` field from struct (line 55)
- Remove constructor parameter from `NewCronTriggerProvider` and `NewCronTriggerProviderWithSeconds`
- Remove limit check block in `executeRule()` (lines 335-353)

**`backend/internal/workers/manual_trigger_provider.go`**
- Remove `ErrExecutionLimitExceeded` error var (line 17)
- Remove `subscriptionSvc` field (line 26)
- Remove constructor parameter from `NewManualTriggerProvider`
- Remove limit check block in `TriggerRule()` (lines 90-108)

**`backend/cmd/server/main.go`**
- Remove `subscriptionLimitAdapter` creation (line 317)
- Remove passing it to `NewCronTriggerProvider` (line 323)
- Remove passing it to `NewManualTriggerProvider` (line 333)

**`backend/internal/domain/automation_rule.go`**
- Remove 4 dead error constants (lines 682-685): `ErrAutomationRuleLimitExceeded`, `ErrExecutionLimitExceeded`, `ErrExecutionIntervalNotMet`, `ErrEventTriggersNotAllowed`
- Clean up stale tier comment above them referencing "free: 1 rule, premium_lite: 5"

**`backend/internal/domain/automation_rule_test.go`**
- Remove 4 entries from `TestErrorConstants` error list (lines 654-657)

**`backend/internal/huma/errors/mapper.go`**
- Remove 4 error mapping entries (lines 139-142: `ErrAutomationRuleLimitExceeded`/`ErrEventTriggersNotAllowed` -> 403, lines 148-151: `ErrExecutionLimitExceeded`/`ErrExecutionIntervalNotMet` -> 429)

**`backend/internal/huma/operations/automation_rules.go`**
- Remove switch case for `workers.ErrExecutionLimitExceeded` -> 429 (lines 266-267)

**`backend/internal/services/rule_execution_service.go`**
- Remove dead fields: `userGetter` (line 65), `subscriptionChecker` (line 66)
- Remove dead setter methods: `SetUserGetter` (lines 91-93), `SetSubscriptionChecker` (lines 96-98)
- Remove interface method declarations for the setters (lines 52, 55)

#### Test Updates (3 files)

**`backend/internal/workers/cron_trigger_provider_test.go`**
- Delete `MockSubscriptionService` struct (lines 134-147)
- Delete `TestCronProvider_EnforcesExecutionLimit` test (lines 528-561)
- Update ~16 test functions to remove mock creation and constructor parameter

**`backend/internal/workers/manual_trigger_provider_test.go`**
- Delete `TestManualProvider_TriggerRule_ExecutionLimitExceeded` test (lines 167-184)
- Update ~13 test functions to remove mock creation and constructor parameter
- Remove `mockSub.On("CheckDailyExecutionLimit"...)` and `IncrementDailyExecutions` expectations

**`backend/internal/services/rule_execution_service_test.go`**
- Delete `mockUserGetterSimple` and `mockSubscriptionCheckerSimple` structs (lines 897-916)
- Remove `service.SetUserGetter()` and `service.SetSubscriptionChecker()` calls (lines 626-627, 718-719)
- Remove mock setup (lines 609-618, 701-710)

### Frontend Changes (10+ files)

#### DELETE (3 files)
| File | What |
|------|------|
| `apps/web/src/features/rules/hooks/useRuleTierLimits.ts` | Dead hook (always returns unlimited) |
| `apps/web/src/features/rules/hooks/useRuleTierLimits.test.ts` | Tests for the dead hook |
| `apps/web/src/test/mocks/rule-tier-limits.ts` | Global mock with stale values |

#### MODIFY (7+ files)

**`apps/web/src/features/rules/components/RulesPage.tsx`**
- Remove `useRuleTierLimits` import and call
- Replace `tierLimits.rulesCountDisplay` with inline `${rules.length} ${rules.length === 1 ? 'rule' : 'rules'}`
- Remove `disabled={tierLimits.isAtLimit}` from create button
- Delete dead upgrade banner block (lines 256-270)

**`apps/web/src/features/rules/components/RuleBuilder.tsx`**
- Remove `useRuleTierLimits` import and call
- Remove `useGetAutomationRules` call (lines 112-114, only used for tier limits)
- Remove `canUseEventTriggers` prop from `TriggerSelector`

**`apps/web/src/features/rules/components/TriggerSelector.tsx`**
- Remove `canUseEventTriggers` from props interface
- Remove guard logic, disabled styling, and premium badge JSX
- Remove `Crown` icon import

**`apps/web/src/features/rules/index.ts`**
- Remove `useRuleTierLimits` re-export (line 39)

**`apps/web/src/features/rules/types.ts`**
- Delete `RuleTierLimits` interface (lines 184-192)

**Test files:**
- `RulesPage.test.tsx` -- Remove mock import/setup, update tier display test
- `RuleBuilder.test.tsx` -- Remove mock import/setup
- `TriggerSelector.test.tsx` -- Remove `canUseEventTriggers` prop, delete "Free Tier Restrictions" test block

**Config files:**
- `bunfig.toml` -- Remove preload entry for `rule-tier-limits.ts` mock
- `bunfig.real.toml` -- Remove reference comment
- `scripts/test-regular.sh` -- Remove exclusion entry
- `scripts/test-real-implementations.sh` -- Remove references

### Additional Cleanup (from Codex review)
- `TriggerSelector.tsx` -- Remove stale comments referencing tier gating behavior (lines ~7, 28, 52)
- `RuleBuilder.test.tsx` / `RulesPage.test.tsx` -- Ensure inline mock return shapes (containing `canUseEventTriggers`, `isAtLimit`, etc.) are fully removed alongside the mock import cleanup

### NOT Changed
- `useTierLimits` hook (account limits) -- separate feature with real logic, untouched
- `SubscriptionChecker`/`UserGetter` interfaces in `user_tag_service.go` -- used by other services
- `subscription_tiers.yaml` -- already correct
- `GetTodayExecutionCount()` in repository -- useful audit data, stays
- `rule_executions` table -- stays for execution history

### Verification
1. `cd backend && go test ./... -v` -- all backend tests pass
2. `cd apps/web && bun run test` -- all frontend tests pass
3. `cd apps/web && bun run lint` -- no lint errors
4. `cd apps/web && bun run test:e2e -- --reporter=line` -- e2e passes
5. Manual check: create/edit/trigger automation rules still works
6. Confirm no references to deleted symbols remain (grep for `SubscriptionLimitChecker`, `SubscriptionLimitAdapter`, `useRuleTierLimits`, `canUseEventTriggers`)
