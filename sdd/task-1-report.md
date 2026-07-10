# Task 1 Report: Feature scaffolding + household badges

**Issue:** #1370 (BoostedBudget household frontend feature)  
**Branch:** `feature/1370-household-frontend`  
**Commit:** `9572eb6c` (#1370: Add household badge variants and module scaffold)

## Implementation Summary

Successfully implemented the household feature module scaffold with badge components following TDD methodology.

### Files Created

1. **`apps/web/src/features/household/components/householdBadgeVariants.ts`**
   - CVA (Class Variance Authority) definition for household-specific badges
   - Five variants modeled on existing `badge.tsx`: `owner`, `member`, `available`, `full`, `over-limit`
   - Semantic token pairs only (no raw Tailwind color classes):
     - `owner`: `bg-primary text-primary-foreground`
     - `member`: `bg-secondary text-secondary-foreground`
     - `available`: `bg-success text-success-foreground`
     - `full`: `bg-warning text-warning-foreground`
     - `over-limit`: `bg-destructive text-destructive-foreground`
   - Size variants: `default`, `sm`, `lg`

2. **`apps/web/src/features/household/components/RoleBadge.tsx`** (+ test)
   - Component displays household member role (owner or member)
   - Renders label "Owner" or "Member" with appropriate variant styling
   - Props: `role: 'owner' | 'member'`, optional `size`, standard HTML attributes
   - 9 passing tests verifying:
     - Label text renders correctly per role
     - Correct variant classes applied (semantic tokens only, no raw colors)
     - Size prop respected
     - Custom className and HTML attributes merge properly

3. **`apps/web/src/features/household/components/SeatUsageBadge.tsx`** (+ test)
   - Component displays household seat usage as "used/total" (e.g., "2/3")
   - Automatically determines state variant based on usage:
     - `available` if `used < total`
     - `full` if `used === total`
     - `over-limit` if `used > total`
   - Props: `used: number`, `total: number`, optional `size`, standard HTML attributes
   - 12 passing tests verifying:
     - Label text renders in correct used/total format
     - State determination logic correct (available → full → over-limit)
     - Correct variant classes applied (semantic tokens only)
     - Size prop respected
     - Custom className and HTML attributes merge properly

4. **`apps/web/src/features/household/types.ts`**
   - Re-exports generated household model types from `@boostedbudget/api-client/models`:
     - `Household`
     - `HouseholdMember`
     - `HouseholdMemberView`
     - `HouseholdInvite`
     - `HouseholdEntitlementSummary`
     - `HouseholdAccountShare`
   - Follows convention used in other features (e.g., `accounts/types.ts`, `boosts/types.ts`)

5. **`apps/web/src/features/household/index.ts`**
   - Public API exports for household module:
     - Badge components: `RoleBadge`, `SeatUsageBadge`
     - Badge variants: `householdBadgeVariants`
     - Types: all household domain types
   - Enables clean imports: `import { RoleBadge, SeatUsageBadge } from '@features/household'`

## TDD Evidence

### RED Phase
- Wrote failing test `RoleBadge.test.tsx` expecting non-existent `RoleBadge` component
- Wrote failing test `SeatUsageBadge.test.tsx` expecting non-existent `SeatUsageBadge` component
- Both test files failed with "Cannot find module" errors

### GREEN Phase
- Implemented `RoleBadge.tsx` → all 9 RoleBadge tests pass
- Implemented `SeatUsageBadge.tsx` → all 12 SeatUsageBadge tests pass
- Total: 21 tests passing, 0 failures

### Test Coverage
- **RoleBadge tests (9)**
  - Label rendering: owner/member labels
  - Badge variants: correct semantic token classes
  - No raw color classes (semantic tokens only)
  - Size prop: default and custom
  - HTML attributes: className merge, data-testid passthrough

- **SeatUsageBadge tests (12)**
  - Label rendering: used/total format
  - State determination: available/full/over-limit logic
  - Badge variants: correct semantic token classes per state
  - No raw color classes (semantic tokens only)
  - Size prop: default and custom
  - HTML attributes: className merge, data-testid passthrough

## Code Quality Checks

✅ **Tests:** 21 pass, 0 fail  
✅ **TypeScript:** `bun run typecheck` passes (no errors)  
✅ **Linting:** `bun run lint` passes (no issues)  
✅ **Semantic tokens:** All color classes use semantic pairs (primary, secondary, success, warning, destructive), zero raw Tailwind color classes  
✅ **Module structure:** Clean public API via `index.ts`, types via `types.ts`, components in `components/`  
✅ **Consistency:** Follows exact patterns from existing features (badge.tsx, boosts/, accounts/)  

## Design Notes

- **CVA single flat variant enum:** All five household badge states (owner, member, available, full, over-limit) are in one `variant` slot, not split into two CVAs. This keeps variant selection logic centralized (in `determineSeatState()` function in SeatUsageBadge).
- **Semantic tokens validation:** Tests explicitly assert semantic token class names are present and raw color classes are NOT present (using `.toMatch(/bg-(primary|secondary|success|warning|destructive)/)` and `.not.toMatch(/bg-blue-|bg-red-|.../)`)
- **Label text as children:** Both badge components render text content (e.g., "Owner", "2/3") as span children, not via props, keeping the component contract simple and consistent with standard badge patterns
- **Size inheritance:** Both components accept optional `size` prop to support future use cases (e.g., `.lg` badge in larger UI contexts)

## Self-Review Findings

✅ All requirements from task brief implemented:
- householdBadgeVariants.ts with one CVA
- RoleBadge component for owner|member roles
- SeatUsageBadge component for used/total + state
- Tests verify correct labels and variant classes
- No raw color literals anywhere
- Module scaffold (index.ts, types.ts) complete

✅ No overbuilding detected:
- Only requested badge variants (5 total)
- Only requested component props
- No extra helper functions or utility exports
- No custom hooks or hooks directory (planned for later tasks)

✅ Code follows established patterns:
- CVA structure matches `components/ui/badge.tsx` base classes
- Component interfaces follow React convention (HTMLAttributes + VariantProps)
- Test structure matches existing test files (card.test.tsx, etc.)
- Import paths match project conventions (@/lib/utils, @boostedbudget/api-client/models)
- Issue reference format follows convention (#1370 in file headers)

✅ Pristine test output:
- No warnings in household feature tests
- All 21 tests output clean
- No stray console output or unhandled errors

## Review Fix: RoleBadge Variant Prop Leak

**Commit:** `37080606` (#1370: Fix RoleBadge variant prop leak (review fix))

### Issue Identified
Code review found that `RoleBadgeProps` extended the full `HouseholdBadgeVariants` type (including `variant`), unlike its sibling `SeatUsageBadge` which correctly used `Omit<HouseholdBadgeVariants, 'variant'>`. This caused the following problem:

- Type-valid caller code like `<RoleBadge role="owner" variant="member" />` would compile without error
- At runtime, the `variant` prop would be spread onto the underlying `<span>` element via `{...props}`
- React would emit a warning: invalid DOM attribute `variant` on `<span>`
- The passed `variant` value had zero visual effect (component computes variant from `role` prop only)

### Fix Applied
1. **Updated `RoleBadgeProps` type definition** (line 16-21):
   - Changed from: `extends React.HTMLAttributes<HTMLSpanElement>, HouseholdBadgeVariants`
   - Changed to: `extends React.HTMLAttributes<HTMLSpanElement>, Omit<HouseholdBadgeVariants, 'variant'>`
   - Matches exact pattern used in `SeatUsageBadgeProps` (line 16-18 of SeatUsageBadge.tsx)

2. **Added tests to verify the fix** (added to RoleBadge.test.tsx, new "Variant prop handling" describe block):
   - Test: "does not leak variant attribute onto DOM element" — confirms `variant` attribute is never present on the rendered `<span>`
   - Test: "correctly computes variant from role prop only" — confirms component still applies correct variant classes based only on `role` prop

### Test Results
```
$ bun test src/features/household
✅ 23 pass, 0 fail (was 21, now includes 2 new tests)
✅ All tests output pristine, no warnings
```

### TypeScript Results
```
$ bun run typecheck
✅ Clean output (no errors)
```

The `Omit<HouseholdBadgeVariants, 'variant'>` type change now correctly prevents TypeScript from accepting `variant` as a prop on `<RoleBadge />`.

## Next Steps (for later tasks)

This scaffold provides a solid foundation for:
- Task 2: Hooks (useHousehold, useHouseholdMembers, etc.)
- Task 3: UI tabs and member list display
- Task 4: Invite flow and accept/decline flows
- Future expansions: household settings, share visibility, role management

The module's clean public API via `index.ts` means future tasks can extend exports without affecting this foundation.
