# BoostedBudget Centralized Configuration

This directory contains configuration files shared across both frontend and backend services.

## Philosophy

**Single Source of Truth**: Configuration defined once, consumed everywhere.

All configuration in this directory is:
- Version controlled
- Environment-independent (environment-specific values use env vars)
- Type-safe (backend: Go structs, frontend: auto-generated TypeScript)
- Validated at load time

## Files

### `subscription_tiers.yaml`

**Primary configuration** for all subscription tier definitions, limits, pricing, and features.

**Used by**:
- **Backend** (`backend/internal/config/tier_config.go`): Loaded at runtime, validated, cached as singleton
- **Frontend** (`frontend/scripts/generate-tier-types.ts`): Build-time TypeScript generation

**Editing**:
```bash
# 1. Edit the YAML file
vim config/subscription_tiers.yaml

# 2. Frontend types auto-regenerate on:
npm run dev        # Before dev server starts
npm run build      # Before production build
npm test           # Before tests run

# Or manually:
npm run generate:types

# 3. Backend auto-loads changes via hot-reload (or restart server)
cd backend && go run cmd/server/main.go
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
- Frontend validates during type generation (see `generate-tier-types.ts:validateTierConfig()`)
- Both reject invalid configs before deployment

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
- Backend loads directly
- Frontend generates TypeScript at build time
- Documentation references YAML

**Benefits**:
- No sync issues (had: frontend showed 2 accounts, backend had 8)
- No missing tiers (had: frontend missing Premium Lite entirely)
- No naming mismatches (had: `premium+` vs `premium_plus`)
- Change once, update everywhere automatically

### Why Build-Time Generation (Frontend)?

**Alternatives considered**:
1. **Runtime API fetch** - Extra network call, slower, requires backend
2. **Duplicate config** - Sync issues (we had bugs from this!)
3. **Build-time generation** ✅ - Type-safe, zero runtime cost, always in sync

**Benefits**:
- TypeScript compiler catches tier name typos at build time
- Autocomplete for tier names in IDE
- No runtime fetch needed (embedded in bundle)
- Generated file gitignored (never commit generated code)

## Future Extensions

When adding new config files:

1. **Create**: `config/new_config.yaml`
2. **Backend**: Load in `internal/config/` package
3. **Frontend**: Add generator script if needed
4. **Document**: Add section to this README

Example future configs:
- `feature_flags.yaml` - Feature toggle configuration
- `regions.yaml` - Multi-region deployment config (Phase 7+)
- `integrations.yaml` - Third-party service endpoints

## Troubleshooting

**Frontend: "Cannot find module '../../types/generated/subscriptionTiers'"**
```bash
cd frontend
npm run generate:types
```

**Backend: "config file not found"**
```bash
# Ensure running from project root or set env var
export SUBSCRIPTION_TIERS_CONFIG=/path/to/config/subscription_tiers.yaml
cd backend && go run cmd/server/main.go
```

**Generated types out of sync**
```bash
# Frontend
npm run generate:types

# Check changes
git diff frontend/src/types/generated/subscriptionTiers.ts
```

## Testing

**Backend**:
```bash
cd backend
go test ./internal/config/... -v
```

**Frontend generator**:
```bash
cd frontend
npm test -- scripts/generate-tier-types.test.ts --run
```

**Integration** (ensure frontend matches backend):
```bash
# Check tier names match
cd backend && go test ./internal/config/... -v | grep "premium_plus"
cd frontend && npm run generate:types && grep "premium_plus" src/types/generated/subscriptionTiers.ts
```

---

Last updated: 2025-10-21 (Issue #158)
