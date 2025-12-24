# BoostedBudget Centralized Configuration

This directory contains configuration files shared across both frontend and backend services.

## Philosophy

**Single Source of Truth**: Configuration defined once, consumed everywhere.

All configuration in this directory is:
- Version controlled
- Environment-independent (environment-specific values use env vars)
- Type-safe (backend: Go structs, frontend: TypeScript via API response)
- Validated at load time

## Files

### `subscription_tiers.yaml`

**Primary configuration** for all subscription tier definitions, limits, pricing, and features.

**Used by**:
- **Backend** (`backend/internal/config/tier_config.go`): Loaded at runtime, validated, cached as singleton, served via API
- **Frontend** (`frontend/src/features/subscription/hooks/useSubscriptionTiers.ts`): Fetched from `GET /api/v1/subscription/tiers` with 24hr caching

**Editing**:
```bash
# 1. Edit the YAML file
vim config/subscription_tiers.yaml

# 2. Backend auto-reloads on next API request (singleton cache)
cd backend && go run cmd/server/main.go

# 3. Frontend auto-refreshes after 24hr cache expiry
# Or manually refresh: call refetch() from useSubscriptionTiers hook
```

**Structure**:
```yaml
tiers:
  free:
    display_name: "Free"
    price_monthly: 0.00
    price_yearly: 0.00
    limits:
      accounts: 8          # Max linked bank accounts
      rules: 2             # Max automation rules
      executions_per_day: 1
      min_execution_interval_minutes: 1440  # 24 hours
    features:
      ads_enabled: true
      ai_insights: false
      # ... more features

  premium_lite:
    # ... tier definition

  premium:
    # ... tier definition

  premium_plus:
    # ... tier definition (use -1 for unlimited)

default_tier: "free"
trial:
  enabled: false
  duration_days: 14
  tier: "premium"
```

**Tier Naming Convention**: Use underscores (`premium_lite`, `premium_plus`), not hyphens or symbols.

**Validation**:
- Backend validates on load (see `tier_config.go:validate()`)
- Invalid configs prevent server startup
- Frontend receives validated data via API

### `default_dashboard_layout.yaml`

**Primary configuration** for the default dashboard layout (widget arrangement).

**CRITICAL: Version Sync Requirement**

This config's `layout_version` MUST be kept in sync with:
- `frontend/src/features/dashboard/widgets/registry.ts` - `DEFAULT_LAYOUT.version`
- `backend/internal/domain/dashboard_preferences.go` - `DefaultDashboardLayout().Version`

**If versions are out of sync, users experience infinite dashboard reset loops!**

**Used by**:
- **Backend** (`backend/internal/config/dashboard_layout_config.go`): Loaded at runtime, served via `GET /api/v1/dashboard/default-layout`
- **Backend** (`backend/internal/services/dashboard_preferences_service.go`): Used for new users and reset-to-default
- **Frontend** (`frontend/src/features/dashboard/hooks/useDefaultDashboardLayout.ts`): Fetched from API with 1hr caching

**Structure**:
```yaml
layout_version: 4  # INCREMENT when changing default widgets

widgets:
  - id: "cash-flow-chart-1"
    type: "cash-flow-chart"
    x: 0
    y: 0
    w: 12
    h: 5
    minW: 4
    minH: 4
```

**Editing**:
1. Update `layout_version` (increment by 1)
2. Modify widgets array as needed
3. Update matching version in:
   - `frontend/src/features/dashboard/widgets/registry.ts`
   - `backend/internal/domain/dashboard_preferences.go`
4. Deploy and verify with: `curl /api/v1/dashboard/default-layout | jq .version`

## Design Decisions

### Why YAML?

- Human-readable and easy to edit
- Supports comments for documentation
- Standard format with mature Go/Node parsers
- Better for config than JSON (trailing commas, comments)

### Why Centralized?

**Before** (duplicated):
- `backend/config/subscription_tiers.yaml` (3 accounts)
- `frontend/src/features/profile/types.ts` SUBSCRIPTION_TIERS constant (1 rule - BUG!)
- `CLAUDE.md` documentation (third copy)

**After** (single source):
- `config/subscription_tiers.yaml` (single source of truth)
- Backend loads directly, serves via API
- Frontend fetches from API with 24hr caching
- Documentation references YAML

**Benefits**:
- No sync issues (had: frontend showed 2 accounts, backend had 8)
- No missing tiers (had: frontend missing Premium Lite entirely)
- No naming mismatches (had: `premium+` vs `premium_plus`)
- Change once, update everywhere automatically

### Why API-Based (Frontend)?

**Alternatives considered**:
1. **Build-time generation** - Sync issues, git conflicts, manual regeneration
2. **Duplicate config** - Sync issues (we had bugs from this!)
3. **Runtime API fetch** ✅ - Type-safe, always in sync, no generation complexity

**Benefits**:
- No generated files to maintain or commit
- No git conflicts from generated code
- No manual regeneration step
- Backend YAML is truly single source of truth
- 24hr client-side caching minimizes network overhead
- Hook-based pattern (useSubscriptionTiers) follows React best practices

## Future Extensions

When adding new config files:

1. **Create**: `config/new_config.yaml`
2. **Backend**: Load in `internal/config/` package, expose via API if needed
3. **Frontend**: Create hook to fetch from API if needed
4. **Document**: Add section to this README

Example future configs:
- `feature_flags.yaml` - Feature toggle configuration
- `regions.yaml` - Multi-region deployment config (Phase 7+)
- `integrations.yaml` - Third-party service endpoints

## Troubleshooting

**Frontend: "Failed to fetch subscription tiers"**
```bash
# Ensure backend is running and serving API
cd backend && go run cmd/server/main.go

# Check API endpoint
curl http://localhost:8080/api/v1/subscription/tiers
```

**Backend: "config file not found"**
```bash
# Ensure running from project root or set env var
export SUBSCRIPTION_TIERS_CONFIG=/path/to/config/subscription_tiers.yaml
cd backend && go run cmd/server/main.go
```

**Frontend: Stale tier data after YAML update**
```bash
# Option 1: Wait 24 hours for cache to expire
# Option 2: Call refetch() from useSubscriptionTiers hook
# Option 3: Clear localStorage in browser DevTools
localStorage.removeItem('subscription_tiers_cache_timestamp')
```

## Testing

**Backend**:
```bash
cd backend
go test ./internal/config/... -v
```

**Frontend API integration**:
```bash
cd frontend
pnpm test -- src/features/subscription/api/subscriptionApi.test.ts --run
pnpm test -- src/features/subscription/hooks/useSubscriptionTiers.test.tsx --run
```

**Integration** (ensure API returns valid data):
```bash
# Start backend
cd backend && go run cmd/server/main.go

# Test API endpoint
curl http://localhost:8080/api/v1/subscription/tiers | jq '.tiers.premium_plus'
```

---

Last updated: 2025-10-23 (Issue #188 - Migration to API-based architecture)
