# Production Paddle Catalog Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the five-product, ten-price live Paddle subscription catalog and bind every live price ID to the production tier configuration.

**Architecture:** Paddle remains the billing catalog while `config/subscription_tiers.production.yaml` maps stable application tier keys to environment-specific price IDs. Catalog creation is idempotent: exact existing products and prices are reused, conflicting definitions stop the run, and a final read-only reconciliation proves the live state matches the repository.

**Tech Stack:** Paddle Billing API/MCP, Go 1.25, YAML, GitHub CLI, Bun/React verification

---

### Task 1: Add production price-ID validation coverage

**Files:**
- Modify: `backend/internal/config/tier_config_test.go`
- Test: `backend/internal/config/tier_config_test.go`

**Step 1: Write the failing test**

Add `TestLoadTierConfig_ProductionHasCompletePaddleCatalog` that sets `PADDLE_ENVIRONMENT=production`, resets the singleton, loads the tier config, and verifies:

```go
seen := make(map[string]domain.TierName)
for tierName, tier := range cfg.Tiers {
    ids := []*string{
        tier.Pricing.PaddlePriceIDMonthly,
        tier.Pricing.PaddlePriceIDYearly,
    }
    for _, id := range ids {
        require.NotNil(t, id, "tier %s must configure both production Paddle prices", tierName)
        require.True(t, strings.HasPrefix(*id, "pri_"))
        require.NotContains(t, seen, *id, "production Paddle price IDs must be unique")
        seen[*id] = tierName
    }
}
assert.Len(t, seen, 10)
```

Restore environment state and reset the singleton during cleanup so later tests do not inherit production configuration.

**Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/config -run TestLoadTierConfig_ProductionHasCompletePaddleCatalog -v`

Expected: FAIL because the production YAML currently contains null price IDs.

### Task 2: Create the idempotent live Paddle catalog

**Files:**
- External: Paddle live catalog
- Reference: `config/subscription_tiers.production.yaml`

**Step 1: Search Paddle API methods**

Search for the exact `products.create`, `products.list`, `prices.create`, and `prices.list` parameter shapes before executing mutations.

**Step 2: Re-read the live catalog**

List active and archived products and prices. Expected initial state: zero products and zero prices. If state changed, reconcile rather than creating duplicates.

**Step 3: Create or reuse five SaaS products**

Use these definitions:

```text
premium     -> BoostedBudget Premium       -> 1 seat
household_2 -> BoostedBudget Household (2) -> 2 seats
household_3 -> BoostedBudget Household (3) -> 3 seats
household_4 -> BoostedBudget Household (4) -> 4 seats
household_5 -> BoostedBudget Household (5) -> 5 seats
```

Each product is `standard`, active, uses `tax_category: saas`, includes the YAML description, and stores `tier` and `users_included` in custom data.

**Step 4: Create or reuse monthly and yearly prices**

Create recurring USD prices with quantity fixed to one, `tax_mode: location`, no Paddle trial period, and custom data containing `tier` and `billing_interval`:

```text
premium:     899/month,  7900/year
household_2: 1299/month, 11900/year
household_3: 1499/month, 13900/year
household_4: 1699/month, 15900/year
household_5: 1899/month, 17900/year
```

Use customer-facing price names `Monthly` and `Annual`, with internal descriptions naming the tier and cycle.

**Step 5: Return all created or reused IDs**

The mutation must return a stable mapping from tier and interval to product and price IDs for direct repository configuration.

### Task 3: Bind production configuration to Paddle

**Files:**
- Modify: `config/subscription_tiers.production.yaml`
- Test: `backend/internal/config/tier_config_test.go`

**Step 1: Replace null production IDs**

Use `apply_patch` to set each `paddle_price_id_monthly` and `paddle_price_id_yearly` to the corresponding live ID returned by Paddle.

**Step 2: Run the focused test to verify it passes**

Run: `cd backend && go test ./internal/config -run TestLoadTierConfig_ProductionHasCompletePaddleCatalog -v`

Expected: PASS with ten unique production price IDs.

**Step 3: Run all config tests**

Run: `cd backend && go test ./internal/config -v`

Expected: PASS.

### Task 4: Reconcile the completed live catalog

**Files:**
- External: Paddle live catalog
- Reference: `config/subscription_tiers.production.yaml`

**Step 1: Query every live product and price**

List active and archived products and prices with product relationships.

**Step 2: Compare all billing fields**

Verify exactly five active standard SaaS products and ten active recurring USD prices. For every configured ID verify product tier, amount, interval/frequency, location tax mode, quantity 1..1, no trial period, and expected custom data.

**Step 3: Stop on any mismatch**

Do not continue to commit or PR if any repository ID is missing, archived, duplicated, attached to the wrong product, or priced incorrectly.

### Task 5: Simplify, verify, and review

**Files:**
- Review: `backend/internal/config/tier_config_test.go`
- Review: `config/subscription_tiers.production.yaml`
- Review: `docs/plans/2026-08-02-production-paddle-catalog*.md`

**Step 1: Run code simplification review**

Apply the repository code-simplifier workflow to the test and configuration diff without changing behavior.

**Step 2: Run required verification**

Run:

```bash
cd backend && go test ./... -v
cd apps/web && bun run test
cd apps/web && bun run typecheck
cd apps/web && bun run lint
cd apps/web && bun run test:e2e -- --reporter=line
```

Expected: all commands exit zero. If environment-dependent E2E cannot run, report the exact blocker rather than treating it as passing.

**Step 3: Run an adversarial second opinion**

Review the full diff for incorrect IDs, cross-environment leakage, missing validation, unsafe assumptions, and repository convention violations. Address findings and rerun affected verification.

### Task 6: Commit, push, and create the PR

**Files:**
- Commit all tracked changes on `feature/746-production-paddle-catalog`

**Step 1: Inspect the final diff**

Run `git diff staging...HEAD`, `git status --short`, and `git diff --check`.

**Step 2: Commit implementation changes**

Commit message:

```text
#746: Configure production Paddle subscription catalog
```

**Step 3: Push the branch**

Push `feature/746-production-paddle-catalog` to `origin` with upstream tracking.

**Step 4: Open the PR against staging**

Use title `#746: Configure production Paddle subscription catalog`. Explain the live catalog created, the config mapping, validation coverage, and follow-up naming issue #1467. Include `Closes #746` and target `staging`.
