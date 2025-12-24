# Frontend Scripts

This directory contains build-time code generation and utility scripts for the BoostedBudget frontend.

## Subscription Tier Type Generation

The primary purpose of these scripts is to generate TypeScript types from `config/subscription_tiers.yaml`.

### Files

**Core Generation:**
- `generate-tier-types.ts` - Main generation logic (reads YAML, generates TS)
- `generate-tier-types-cli.ts` - CLI entry point (orchestrates generation)
- `generate-tier-types-robust.ts` - Robust file writing utilities with fsync
- `generate-tier-types.test.ts` - Unit tests for generation logic

**Verification:**
- `verify-generated-file.js` - Standalone verification script for CI

**Integration Tests:**
- `integration/generation-timing.test.ts` - Timing reliability tests

### How It Works

1. **Reads YAML**: Parses `config/subscription_tiers.yaml`
2. **Generates Types**: Creates TypeScript interfaces and constants
3. **Writes Robustly**: Uses fsync to force kernel buffer flush to disk
4. **Verifies**: Reads back and validates content with retry logic
5. **Auto-triggers**: pnpm hooks and Vite plugin ensure automatic generation

### Generated Output

**Location**: `frontend/src/types/generated/subscriptionTiers.ts`

**Git Status**: Git-ignored (never commit this file!)

**Contains**:
```typescript
export interface SubscriptionTier {
  display_name: string
  description: string
  price_monthly: number
  // ... more fields
}

export const SUBSCRIPTION_TIERS: {
  free: SubscriptionTier
  premium_lite: SubscriptionTier
  premium: SubscriptionTier
  premium_plus: SubscriptionTier
}
```

### Usage

**Manual Generation**:
```bash
pnpm run generate:types
```

**With Verification**:
```bash
pnpm run generate:types:verify
```

**Automatic Generation** (you usually don't need to run manually):
- `pnpm run dev` - Vite plugin generates before server starts
- `pnpm run build` - prebuild hook generates
- `pnpm test` - pretest hook generates

### Timing Issue Prevention

Historical Context: Early implementations had race conditions on macOS APFS where the OS hadn't flushed the file to disk before tests tried to read it.

**Solution - Three Layers**:

1. **Layer 1: Robust Writing** (`generate-tier-types-robust.ts`)
   - Uses `fsyncSync()` to force kernel buffer flush
   - Blocks until data is physically on disk
   - Prevents "file doesn't exist" race conditions

2. **Layer 2: Verification with Retry** (`writeFileWithVerification()`)
   - Reads back file and validates content
   - Exponential backoff: 10ms, 20ms, 40ms, 80ms...
   - Up to 10 retry attempts before failing
   - Ensures file is readable and valid

3. **Layer 3: Vite Plugin** (`vite.config.ts`)
   - Generates before module resolution starts
   - Prevents Vite cache issues
   - One-time per server lifetime

### Testing

**Unit Tests**:
```bash
pnpm test -- scripts/generate-tier-types.test.ts --run
```

**Integration Tests** (timing reliability):
```bash
pnpm test -- scripts/integration/generation-timing.test.ts --run
```

**Stress Test** (manual):
```bash
# Run generation 100 times rapidly
for i in {1..100}; do
  pnpm run generate:types || exit 1
done
```

### Troubleshooting

**Error: "Cannot find module '../../types/generated/subscriptionTiers'"**

The generated file doesn't exist. This is normal on fresh clone.

Fix: Run any of these commands:
```bash
pnpm run generate:types
pnpm run dev
pnpm test
pnpm run build
```

**Error: "File verification failed after 10 attempts"**

This indicates a serious filesystem issue. Possible causes:
- Disk full
- Permission denied
- Filesystem corruption
- Antivirus interference

Check:
```bash
df -h  # Check disk space
ls -la src/types/generated/  # Check permissions
```

**Error: "Generated file too small/large"**

The generation script produced unexpected output. This could indicate:
- YAML file corrupted
- Generation logic bug
- Filesystem issue

Fix:
1. Check `config/subscription_tiers.yaml` is valid
2. Run: `pnpm run generate:types -- --verbose`
3. Check the generated file manually
4. Report if it persists

### Performance

**Typical timings**:
- Generation: 50-200ms
- Verification: 10-50ms
- Total: 60-250ms

**Platform differences**:
- Linux (ext4): ~60-100ms (fastest)
- macOS (APFS): ~100-200ms (fsync overhead)
- Windows (NTFS): ~80-150ms

### CI/CD Integration

The CI pipeline:
1. Generates types fresh on every run
2. Verifies file exists and is valid
3. Checks file is properly gitignored
4. Ensures file is NOT tracked by git

If CI fails with "file tracked by git":
```bash
git rm --cached src/types/generated/subscriptionTiers.ts
git commit -m "Untrack generated file"
```

### Development Tips

**Adding New Tiers**:
1. Edit `config/subscription_tiers.yaml`
2. Run `pnpm test` (auto-generates + runs tests)
3. Check `src/types/generated/subscriptionTiers.ts` was updated
4. **Do NOT commit the generated file**

**Modifying Generation Logic**:
1. Edit `generate-tier-types.ts`
2. Run tests: `pnpm test -- scripts/generate-tier-types.test.ts`
3. Test manually: `pnpm run generate:types`
4. Check generated output looks correct

**Debugging Generation**:
```bash
# Verbose output
pnpm run generate:types

# Check what was generated
cat src/types/generated/subscriptionTiers.ts

# Run with Node debugger
node --inspect-brk ./node_modules/.bin/tsx scripts/generate-tier-types-cli.ts
```

### Architecture Decisions

**Why git-ignore the generated file?**
- Keeps git history clean (no generated code noise)
- Prevents merge conflicts on generated file
- Forces fresh generation on every build (always in sync)
- Aligns with best practices (don't commit derived artifacts)

**Why not runtime YAML loading?**
- TypeScript type safety requires generated types
- Zero runtime performance cost (types compiled away)
- IDE autocomplete works perfectly
- Compile-time error checking

**Why fsync?**
- macOS APFS delays writes (can take 1-10ms to flush)
- Without fsync: test starts before file exists → failure
- With fsync: guaranteed on disk before function returns
- Small performance cost (10-50ms) worth the reliability

**Why Vite plugin?**
- Ensures generation before module resolution
- Prevents "Cannot find module" errors
- Works even if pnpm hooks somehow don't run
- Defense in depth (multiple safety nets)

### Contributing

When modifying generation scripts:
1. ✅ Add tests for new functionality
2. ✅ Test on macOS, Linux, Windows
3. ✅ Run integration tests 10+ times
4. ✅ Update this README
5. ✅ Document any new pnpm scripts

### References

- YAML Config: `config/subscription_tiers.yaml`
- Generated Output: `src/types/generated/subscriptionTiers.ts` (git-ignored)
- Vite Plugin: `vite.config.ts` (`tierTypesGeneratorPlugin`)
- pnpm Scripts: `package.json`
- CI Workflow: `.github/workflows/ci-cd.yml`
- Main Documentation: `config/README.md`

---

Last Updated: 2025-10-23 (Issue #180 - Option 3 Implementation)
