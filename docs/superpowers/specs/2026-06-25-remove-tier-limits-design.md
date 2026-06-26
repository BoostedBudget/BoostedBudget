# Design: Remove useTierLimits (Issue #1294)

## Context

`useTierLimits` enforced per-tier account and rule limits. All real tiers now have
`accounts: -1` (unlimited), and rule limits are not enforced anywhere. The hook is
functionally dead: every flag it returns (`isAtLimit`, `shouldShowUpgradeBanner`) is
always `false` in production. The `accountCountDisplay` it computes always takes the
"unlimited" branch.

Related: PR #1292 / Issue #1288 (broader tier-metadata purge that surfaced these).

## Decision

Delete `useTierLimits` and all downstream dead code. No behavior change for users.

## Files Deleted

| File | Reason |
|---|---|
| `apps/web/src/features/subscription/hooks/useTierLimits.ts` | The hook itself |
| `apps/web/src/features/subscription/hooks/useTierLimits.test.ts` | 400-line test suite for dead code |
| `apps/web/src/features/accounts/UpgradeBanner.tsx` | Only shown when `shouldShowUpgradeBanner` (always false); no test file |

## Files Modified

### `AccountsPage.tsx`
- Remove `useTierLimits`, `UpgradeBanner`, `useSubscriptionTiers` imports
- Remove `user` from `useProfile()` destructure (only used to feed tier limits; `userLoading` and `refetchProfile` stay)
- Remove `{ tiers, loading: tiersLoading }` from `useSubscriptionTiers()` call → remove the call entirely
- Inline account count: `${activeAccountsCount} ${activeAccountsCount === 1 ? 'account' : 'accounts'}`
- Remove `disabled={isAtLimit}` from both `PlaidLinkButton` instances
- Remove `{shouldShowUpgradeBanner && <UpgradeBanner />}`
- Replace `loading || tiersLoading` → `loading` in two places

### `AccountsPage.test.tsx`
- Remove `mockSubscriptionTiersResponse` import
- Remove `mockUseGetSubscriptionTiers`/`mockRefetchSubscriptionTiers` imports, `.mockClear()`, and `.mockReturnValue()` calls

### `AutoCategorizationPage.tsx`
- Remove `maxRules?` and `currentTier?` from `AutoCategorizationPageProps`
- Remove `isAtLimit` and `showTierWarning` local variables
- Remove `disabled={isAtLimit}` from Create Rule button
- Remove tier-warning banner block

### `AutoCategorizationPage.test.tsx`
- Delete the "Tier limits" describe block (3 tests)

### `AutoCategorizationPageWrapper.tsx`
- Remove `useProfile` import and call (only used for `userTier`)
- Remove `userTier` variable
- Remove `maxRules={undefined}` and `currentTier={userTier}` props

### `AutoCategorizationPageWrapper.test.tsx`
- Remove `maxRules` and `currentTier` from the mock component type and rendered spans
- Remove "should pass currentTier from user profile" test
- Remove "should pass maxRules as undefined" test
- Update "should default to premium tier when user is null" to remove `current-tier` assertion

## Testing Strategy

**TDD order:** test files first (RED), production files second (GREEN).

- No new tests needed — we are removing dead code paths
- Existing tests should pass after cleanup with no behavior assertions changed
- After changes: `bun run test`, `bun run typecheck`, `bun run lint` must all pass

## Out of Scope

- `useSubscriptionTiers` hook itself — kept; used by pricing/settings pages
- Any future re-introduction of rule or account limits — that would be a new feature
