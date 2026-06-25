# Remove useTierLimits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the `useTierLimits` hook and all dead tier-limit enforcement code; no user-visible behavior change.

**Architecture:** TDD order — test files first (RED), production files second (GREEN), then delete orphaned files. Each task ends with a passing test run and a commit.

**Tech Stack:** React 19, TypeScript 5, bun:test, `@testing-library/react`

**Design spec:** `docs/superpowers/specs/2026-06-25-remove-tier-limits-design.md`

## Global Constraints

- Branch naming: `bugfix/1294-remove-tier-limits`
- Commit prefix: `#1294:`
- All tiers have `accounts: -1` (unlimited) — never re-introduce limit enforcement
- `bun run test`, `bun run typecheck`, `bun run lint` must pass after every task
- Run frontend tests from `apps/web/`: `bun run test`
- Run typecheck: `bun run typecheck`
- Run lint: `bun run lint`
- `useSubscriptionTiers` hook is NOT deleted — other pages use it; only remove it from AccountsPage

---

## File Map

| Action | Path |
|--------|------|
| **Delete** | `apps/web/src/features/subscription/hooks/useTierLimits.ts` |
| **Delete** | `apps/web/src/features/subscription/hooks/useTierLimits.test.ts` |
| **Delete** | `apps/web/src/features/accounts/UpgradeBanner.tsx` |
| **Modify** | `apps/web/src/features/accounts/PlaidLinkButton.tsx` |
| **Modify** | `apps/web/src/features/auto-categorization/components/AutoCategorizationPage.tsx` |
| **Modify** | `apps/web/src/features/auto-categorization/components/AutoCategorizationPage.test.tsx` |
| **Modify** | `apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.tsx` |
| **Modify** | `apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.test.tsx` |
| **Modify** | `apps/web/src/features/accounts/AccountsPage.tsx` |
| **Modify** | `apps/web/src/features/accounts/AccountsPage.test.tsx` |

---

### Task 1: Create Feature Branch

- [ ] **Step 1: Create and switch to feature branch**

```bash
git checkout -b bugfix/1294-remove-tier-limits
```

Expected: `Switched to a new branch 'bugfix/1294-remove-tier-limits'`

---

### Task 2: RED — Remove tier-limit tests from AutoCategorizationPage

**Files:**
- Modify: `apps/web/src/features/auto-categorization/components/AutoCategorizationPage.test.tsx`

**Context:** Lines 312–330 contain a "Tier limits" describe block that tests `maxRules` and `currentTier` props. Those props are being deleted. Remove this entire block so the TypeScript compiler rejects the old prop usage in GREEN.

- [ ] **Step 1: Delete the "Tier limits" describe block**

Open `apps/web/src/features/auto-categorization/components/AutoCategorizationPage.test.tsx`.

Delete lines 312–330 in their entirety (the `describe('Tier limits', ...)` block below):

```tsx
  describe('Tier limits', () => {
    it('should show tier limit warning when at limit', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/rule limit reached/i)).toBeInTheDocument()
    })

    it('should disable create button when at limit', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByRole('button', { name: /create rule/i })).toBeDisabled()
    })

    it('should show upgrade prompt for free tier', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/upgrade/i)).toBeInTheDocument()
    })
  })
```

- [ ] **Step 2: Run tests to confirm they pass (no compile error yet — TypeScript only fails when props are checked)**

```bash
cd apps/web && bun run test src/features/auto-categorization/components/AutoCategorizationPage.test.tsx
```

Expected: All remaining tests pass. The deleted tests are simply gone.

---

### Task 3: GREEN — Remove tier-limit props from AutoCategorizationPage

**Files:**
- Modify: `apps/web/src/features/auto-categorization/components/AutoCategorizationPage.tsx`

**Context:** Remove `maxRules?`, `currentTier?` from the props interface, `isAtLimit` and `showTierWarning` local variables, `disabled={isAtLimit}` from the Create Rule button, and the warning banner block.

- [ ] **Step 1: Remove `maxRules` and `currentTier` from the props interface**

In `AutoCategorizationPage.tsx`, find the `AutoCategorizationPageProps` interface (around line 21) and the function signature (around line 44). Change:

```tsx
export interface AutoCategorizationPageProps {
  categories: string[][]
  plaidPrimaries?: Primary[]
  accounts?: Account[]
  tags?: UserTag[]  // User tags for target tags autocomplete
  maxRules?: number
  currentTier?: string
}
```

to:

```tsx
export interface AutoCategorizationPageProps {
  categories: string[][]
  plaidPrimaries?: Primary[]
  accounts?: Account[]
  tags?: UserTag[]  // User tags for target tags autocomplete
}
```

And change the function signature from:

```tsx
export function AutoCategorizationPage({ categories, plaidPrimaries = [], accounts = [], tags = [], maxRules, currentTier }: AutoCategorizationPageProps) {
```

to:

```tsx
export function AutoCategorizationPage({ categories, plaidPrimaries = [], accounts = [], tags = [] }: AutoCategorizationPageProps) {
```

- [ ] **Step 2: Remove `isAtLimit`, `showTierWarning`, and disabled button state**

Find these two lines (around line 76) and delete them:

```tsx
  const isAtLimit = maxRules !== undefined && rules.length >= maxRules
  const showTierWarning = isAtLimit && currentTier === 'free'
```

Find the Create Rule button (around line 304) and remove the `disabled` prop:

```tsx
          <Button onClick={handleCreateClick} disabled={isAtLimit}>
            Create Rule
          </Button>
```

becomes:

```tsx
          <Button onClick={handleCreateClick}>
            Create Rule
          </Button>
```

- [ ] **Step 3: Remove the tier-warning banner block**

Find and delete this entire block (around line 310):

```tsx
      {showTierWarning && (
        <div className="flex items-center justify-between gap-4 px-4 py-3 mb-4 bg-warning/10 border border-warning/30 rounded-md">
          <p className="m-0 text-sm text-warning-foreground">
            <strong>Rule limit reached.</strong> You&apos;ve reached the maximum of{' '}
            {maxRules} rule{maxRules !== 1 ? 's' : ''} for the free tier.
          </p>
          <a href="/settings/subscription" className="flex-shrink-0 text-sm font-medium text-primary hover:underline">
            Upgrade for more rules
          </a>
        </div>
      )}
```

- [ ] **Step 4: Run tests and typecheck**

```bash
cd apps/web && bun run test src/features/auto-categorization/components/AutoCategorizationPage.test.tsx && bun run typecheck
```

Expected: All tests pass, no TypeScript errors.

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/features/auto-categorization/components/AutoCategorizationPage.tsx \
        apps/web/src/features/auto-categorization/components/AutoCategorizationPage.test.tsx
git commit -m "$(cat <<'EOF'
#1294: Remove maxRules/currentTier props and tier-limit UI from AutoCategorizationPage

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: RED + GREEN — Clean up AutoCategorizationPageWrapper

**Files:**
- Modify: `apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.test.tsx`
- Modify: `apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.tsx`

**Context:** The wrapper computed `userTier` from `useProfile()` solely to pass `currentTier` to the page. Both are now dead. The wrapper test mocked these props and has three tests that assert on them.

- [ ] **Step 1: Update the AutoCategorizationPage mock in the wrapper test**

In `AutoCategorizationPageWrapper.test.tsx`, find the `mock.module('./AutoCategorizationPage', ...)` block (lines 20–30). Replace it with:

```tsx
mock.module('./AutoCategorizationPage', () => ({
  AutoCategorizationPage: ({ categories, accounts, tags }: { categories: string[][], accounts?: Account[], tags?: UserTag[] }) => (
    <div data-testid="auto-categorization-page">
      <span data-testid="categories-count">{categories.length}</span>
      <span data-testid="accounts-count">{accounts?.length ?? 0}</span>
      <span data-testid="tags-count">{tags?.length ?? 0}</span>
    </div>
  ),
}))
```

- [ ] **Step 2: Remove the four obsolete tests**

Delete the following four tests in their entirety (three named, plus one Codex-flagged fourth):

```ts
  it('should handle undefined tiers gracefully', async () => {
    configureHooks({ tiers: null })
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('max-rules')).toHaveTextContent('undefined')
  })
```

```ts
  it('should pass maxRules as undefined (unlimited rules for all tiers)', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    // All tiers now have unlimited rules, so maxRules is undefined
    expect(screen.getByTestId('max-rules')).toHaveTextContent('undefined')
  })

  it('should pass currentTier from user profile', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('current-tier')).toHaveTextContent('premium')
  })
```

And the "should default to premium tier when user is null" test. Replace it with a simpler version that just verifies the wrapper renders gracefully when there's no user:

```ts
  it('should render when user is null', async () => {
    configureHooks({ user: null })
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
  })
```

- [ ] **Step 3: Remove the profile-related mock from `configureHooks` and `beforeEach`**

In `configureHooks`, delete the profile and subscription mock blocks:

```ts
    mockUseGetUserProfile.mockReturnValue({
      data: overrides?.user === undefined ? mockUser : overrides?.user,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    mockUseGetSubscriptionTiers.mockReturnValue({
      data: overrides?.tiers === undefined ? mockSubscriptionTiersResponse : overrides?.tiers,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
```

Also remove `user` and `tiers` from the `configureHooks` overrides type:

```ts
  function configureHooks(overrides?: {
    categories?: CustomCategory[] | null
    categoriesError?: Error | null
    plaidCategories?: string[] | null
    accounts?: Account[] | null
    tags?: UserTag[] | null
  }) {
```

In `beforeEach`, delete:

```ts
    mockUseGetUserProfile.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
```

- [ ] **Step 4: Remove now-unused imports at the top of the wrapper test**

Remove these imports:

```ts
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import { mockUseGetUserProfile, mockRefetchUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers, mockRefetchSubscriptionTiers } from '@/test/mocks/subscription-api'
```

Also remove the `mockUser` constant definition (the object assigned around line 88–101).

- [ ] **Step 5: Update the production wrapper — remove `useProfile`, `userTier`, tier props, and stale JSDoc**

In `AutoCategorizationPageWrapper.tsx`, find the JSDoc block at the top of the file and remove these two stale bullet lines:

```
 * - User subscription tier for rule limits
 * - Subscription tiers config for max rules limit
```

Then remove the `useProfile` import:

```ts
import { useProfile } from '../../profile/hooks/useProfile'
```

Remove the `user` line from the component body:

```ts
  const { user } = useProfile()
```

Remove the `userTier` computation:

```ts
  const userTier = user?.effective_tier ?? user?.subscription_tier ?? 'premium'
```

Update the `<AutoCategorizationPage>` render — remove `maxRules` and `currentTier` props:

```tsx
  return (
    <AutoCategorizationPage
      categories={categories}
      plaidPrimaries={plaidPrimaries}
      accounts={accounts}
      tags={tags}
      maxRules={undefined}
      currentTier={userTier}
    />
  )
```

becomes:

```tsx
  return (
    <AutoCategorizationPage
      categories={categories}
      plaidPrimaries={plaidPrimaries}
      accounts={accounts}
      tags={tags}
    />
  )
```

- [ ] **Step 6: Run tests and typecheck**

```bash
cd apps/web && bun run test src/features/auto-categorization/components/AutoCategorizationPageWrapper.test.tsx && bun run typecheck
```

Expected: All tests pass, no TypeScript errors.

- [ ] **Step 7: Commit**

```bash
git add apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.tsx \
        apps/web/src/features/auto-categorization/components/AutoCategorizationPageWrapper.test.tsx
git commit -m "$(cat <<'EOF'
#1294: Remove userTier/useProfile from AutoCategorizationPageWrapper

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: RED + GREEN — Clean up AccountsPage

**Files:**
- Modify: `apps/web/src/features/accounts/AccountsPage.test.tsx`
- Modify: `apps/web/src/features/accounts/AccountsPage.tsx`

**Context:** AccountsPage uses all three outputs of `useTierLimits` (`isAtLimit`, `shouldShowUpgradeBanner`, `accountCountDisplay`). It also calls `useSubscriptionTiers` solely to feed tier limits. Both are removed here. The test mocks subscription tiers — that mock setup becomes unused.

- [ ] **Step 1: Remove subscription mock setup from AccountsPage.test.tsx**

Delete these import lines:

```ts
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
```

In the `beforeEach` block, delete:

```ts
    mockUseGetSubscriptionTiers.mockClear()
```

and delete:

```ts
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
```

- [ ] **Step 2: Remove imports from AccountsPage.tsx**

In `apps/web/src/features/accounts/AccountsPage.tsx`, remove these three import lines:

```ts
import { UpgradeBanner } from './UpgradeBanner'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { useTierLimits } from '../subscription/hooks/useTierLimits'
```

- [ ] **Step 3: Remove `user` from `useProfile()` and remove `useSubscriptionTiers` call**

Find (around line 83):

```ts
  const { user, loading: userLoading, refetch: refetchProfile } = useProfile()
  const { tiers, loading: tiersLoading } = useSubscriptionTiers()
```

Replace with:

```ts
  const { loading: userLoading, refetch: refetchProfile } = useProfile()
```

- [ ] **Step 4: Remove the `useTierLimits` call**

Find and delete (around line 283):

```ts
  const { isAtLimit, shouldShowUpgradeBanner, accountCountDisplay } = useTierLimits({
    user,
    tiers,
    currentAccountCount: activeAccountsCount,
  })
```

- [ ] **Step 5: Replace `tiersLoading` references and remove tier-limit UI**

There are three `tiersLoading` references and two `isAtLimit` disabled props. Apply all of the following changes:

**Change 1** — Remove `{shouldShowUpgradeBanner && <UpgradeBanner />}` (around line 343):

```tsx
      {shouldShowUpgradeBanner && <UpgradeBanner />}

      {!(loading || tiersLoading) && accounts.length === 0 ? (
```

becomes:

```tsx
      {!loading && accounts.length === 0 ? (
```

**Change 2** — Remove `disabled={isAtLimit}` from the header PlaidLinkButton (around line 335):

```tsx
          <ReadOnlyGuard>
            <PlaidLinkButton
              onSuccess={handleAccountsLinked}
              disabled={isAtLimit}
            />
          </ReadOnlyGuard>
```

becomes:

```tsx
          <ReadOnlyGuard>
            <PlaidLinkButton
              onSuccess={handleAccountsLinked}
            />
          </ReadOnlyGuard>
```

**Change 3** — Remove `disabled={isAtLimit}` from the EmptyState PlaidLinkButton (around line 352):

```tsx
            <ReadOnlyGuard>
              <PlaidLinkButton
                onSuccess={handleAccountsLinked}
                disabled={isAtLimit}
              />
            </ReadOnlyGuard>
```

becomes:

```tsx
            <ReadOnlyGuard>
              <PlaidLinkButton
                onSuccess={handleAccountsLinked}
              />
            </ReadOnlyGuard>
```

**Change 4** — Replace `{accountCountDisplay}` with inline count (around line 362):

```tsx
          <p className="text-base text-muted-foreground mb-4">
            {accountCountDisplay}
```

becomes:

```tsx
          <p className="text-base text-muted-foreground mb-4">
            {activeAccountsCount} {activeAccountsCount === 1 ? 'account' : 'accounts'}
```

**Change 5** — Remove `tiersLoading` from AccountTable's loading prop (around line 372):

```tsx
              loading={loading || tiersLoading}
```

becomes:

```tsx
              loading={loading}
```

**Change 6** — Remove `tiersLoading` from the cards loading guard (around line 387):

```tsx
          ) : (loading || tiersLoading) && displayedAccounts.length === 0 ? (
```

becomes:

```tsx
          ) : loading && displayedAccounts.length === 0 ? (
```

- [ ] **Step 6: Run tests and typecheck**

```bash
cd apps/web && bun run test src/features/accounts/AccountsPage.test.tsx && bun run typecheck
```

Expected: All tests pass, no TypeScript errors.

- [ ] **Step 7: Commit**

```bash
git add apps/web/src/features/accounts/AccountsPage.tsx \
        apps/web/src/features/accounts/AccountsPage.test.tsx
git commit -m "$(cat <<'EOF'
#1294: Remove useTierLimits and useSubscriptionTiers from AccountsPage

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5.5: Fix Stale Tier-Limit Copy in PlaidLinkButton

**Files:**
- Modify: `apps/web/src/features/accounts/PlaidLinkButton.tsx`

**Context:** `mapErrorToMessage` at line 43 returns "Account limit reached for your subscription tier. Please upgrade to add more accounts." when the backend returns 403. All tiers are unlimited so this path is never triggered, but the message is still stale tier-specific copy. Replace with a generic message so it remains safe for any future 403 scenario.

- [ ] **Step 1: Update the 403 error message**

In `PlaidLinkButton.tsx`, find (around line 42):

```ts
    if (error.response.status === 403) {
      return 'Account limit reached for your subscription tier. Please upgrade to add more accounts.'
    }
```

Replace with:

```ts
    if (error.response.status === 403) {
      return 'Unable to link this account. Please contact support.'
    }
```

- [ ] **Step 2: Run tests**

```bash
cd apps/web && bun run typecheck
```

Expected: No TypeScript errors. (PlaidLinkButton has no unit test for this error message path — no test update needed.)

- [ ] **Step 3: Commit**

```bash
git add apps/web/src/features/accounts/PlaidLinkButton.tsx
git commit -m "$(cat <<'EOF'
#1294: Remove tier-specific language from PlaidLinkButton 403 error message

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Delete Orphaned Files

**Files:**
- Delete: `apps/web/src/features/subscription/hooks/useTierLimits.ts`
- Delete: `apps/web/src/features/subscription/hooks/useTierLimits.test.ts`
- Delete: `apps/web/src/features/accounts/UpgradeBanner.tsx`

**Context:** After Tasks 2–5, nothing imports these files. Delete them.

- [ ] **Step 1: Delete all three files**

```bash
rm apps/web/src/features/subscription/hooks/useTierLimits.ts \
   apps/web/src/features/subscription/hooks/useTierLimits.test.ts \
   apps/web/src/features/accounts/UpgradeBanner.tsx
```

- [ ] **Step 2: Verify no remaining imports**

```bash
grep -r "useTierLimits\|UpgradeBanner" apps/web/src/
```

Expected: No output. If any files appear, fix them before continuing.

- [ ] **Step 3: Run full frontend test suite**

```bash
cd apps/web && bun run test && bun run typecheck && bun run lint
```

Expected: All tests pass, no TypeScript errors, no lint errors.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
#1294: Delete useTierLimits hook, its tests, and UpgradeBanner

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Final Verification and PR

- [ ] **Step 1: Run code-simplifier agent**

Dispatch the `code-simplifier` subagent (haiku) against all modified files to catch any remaining redundancy or style issues. Fix any issues it surfaces before running the full suite.

- [ ] **Step 2: Run the full test suite**

```bash
cd apps/web && bun run test && bun run typecheck && bun run lint
```

Expected: All pass.

- [ ] **Step 3: Run Codex adversarial review**

Dispatch `codex:codex-rescue` with the full diff to get a second opinion. If Codex is unavailable, use `feature-dev:code-reviewer` with fresh context and note the fallback in the PR body.

- [ ] **Step 4: Create PR**

Run `/commit-push-pr` (or manually):

```bash
gh pr create \
  --title "#1294: Remove useTierLimits hook and dead tier-limit enforcement" \
  --body "$(cat <<'EOF'
## Summary

- Deletes `useTierLimits` hook, its 400-line test suite, and `UpgradeBanner` component — all dead code (all real tiers have `accounts: -1`)
- Removes `maxRules`/`currentTier` props from `AutoCategorizationPage` (wrapper always passed `maxRules={undefined}` so `isAtLimit` was never true)
- Removes `useSubscriptionTiers` and `user` from `AccountsPage` (only consumed to feed tier limits)
- No user-visible behavior change

## Test plan

- [ ] `bun run test` passes
- [ ] `bun run typecheck` passes
- [ ] `bun run lint` passes
- [ ] Accounts page loads and PlaidLinkButton is always enabled
- [ ] Auto-categorization page loads and Create Rule button is always enabled

Closes #1294
EOF
)"
```
