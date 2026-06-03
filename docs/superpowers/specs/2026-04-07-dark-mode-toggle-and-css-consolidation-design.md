# Design Spec: Dark Mode Toggle & CSS Consolidation (#835)

## Context

BoostedBudget has dark mode CSS variables and `dark:` Tailwind utilities defined but never activated — the `.dark` class is never added to the HTML element. There's no theme toggle anywhere in the UI. Additionally, the CSS architecture is fragmented across 3 conflicting approaches: raw hex in CSS files, undefined CSS custom properties, and Tailwind utilities. This spec covers making dark/light mode fully functional everywhere with a single, consistent CSS approach.

**Problem**: Users can't choose their theme. Pages appear broken because of Vite scaffold dark defaults in `:root` that conflict with the semantic variable system. ~40+ CSS/TSX files have hardcoded colors that won't adapt to dark mode.

**Goal**: Ship a toggleable, fully-functional dark/light mode with all CSS consolidated to Tailwind utilities using the existing semantic token system.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| CSS strategy | Full migration to Tailwind utilities | Eliminates ~40 standalone CSS files, matches shadcn/ui pattern |
| Toggle location | Profile settings only | Cleaner header, discoverable in settings |
| Default theme | Follow OS system preference | Respects user's existing choice, standard for modern apps |
| Execution approach | Big bang (single PR) | One comprehensive change, no intermediate states |
| Theme library | Custom provider (no external lib) | Lightweight, no dependency, matches shadcn/ui pattern |
| Persistence | `localStorage` key `bb-theme` | Simple, instant, no API call needed |

## Architecture

### 1. ThemeProvider

**New files**:
- `apps/web/src/shared/theme/ThemeProvider.tsx` — React context provider
- `apps/web/src/shared/theme/useTheme.ts` — Consumer hook
- `apps/web/src/shared/theme/index.ts` — Barrel export

**Behavior**:
- Reads `localStorage.getItem('bb-theme')` on mount
- Three modes: `light`, `dark`, `system` (default)
- `system` mode listens to `prefers-color-scheme` media query
- Toggles `.dark` class on `document.documentElement`
- Exposes: `theme` (stored value), `resolvedTheme` (actual light/dark), `setTheme()`

**Integration**: Wrap `<App>` in `<ThemeProvider defaultTheme="system">` in `main.tsx`

### 2. FOUC Prevention

**File**: `apps/web/index.html` — add blocking `<script>` in `<head>` before any CSS/JS:

```js
<script>
(function() {
  var stored = localStorage.getItem('bb-theme');
  var prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  if (stored === 'dark' || (stored !== 'light' && prefersDark)) {
    document.documentElement.classList.add('dark');
  }
})();
</script>
```

This runs synchronously before first paint, preventing any flash.

### 3. index.css Fixes

**Remove Vite scaffold defaults** from `:root` (lines 11-13):
```css
/* DELETE these lines */
color-scheme: light dark;
color: rgba(255, 255, 255, 0.87);
background-color: #242424;
```

The `@layer base` block already handles this correctly:
```css
body { @apply bg-background text-foreground; }
```

**Add skeleton token** to both `:root` and `.dark`:
```css
:root {
  --skeleton: oklch(0.905 0 0);  /* gray-200 equivalent */
}
.dark {
  --skeleton: oklch(0.371 0 0);  /* gray-700 equivalent */
}
```

Add to `@theme inline`:
```css
--color-skeleton: var(--skeleton);
```

### 4. Profile Settings Toggle

**Location**: Profile page → new "Appearance" section (or within existing Preferences)

**Component**: `apps/web/src/features/profile/components/ThemeSelector.tsx`

**Design**:
- Three-option segmented control or radio group
- Options: Light (Sun icon), Dark (Moon icon), System (Monitor icon)
- System shows current resolved state as hint text: "Currently using dark mode"
- Changes apply instantly (no save button needed)

### 5. CSS-to-Tailwind Migration

#### Hex → Semantic Token Mapping

| Hardcoded Colors | Semantic Tailwind Class |
|---|---|
| `white`, `#ffffff`, `#fafafa`, `#f8fafc`, `#f9fafb` | `bg-background` or `bg-card` |
| `#1e293b`, `#111827`, `#212121`, `#333`, `#1a1a1a` | `text-foreground` |
| `#424242`, `#374151`, `#4b5563` | `text-foreground` or `text-muted-foreground` |
| `#64748b`, `#6b7280`, `#616161`, `#666`, `#757575` | `text-muted-foreground` |
| `#94a3b8`, `#9CA3AF`, `#bdbdbd` | `text-muted-foreground` (lighter) |
| `#e2e8f0`, `#e5e7eb`, `#e0e0e0`, `#d1d5db`, `#E5E7EB` | `border-border` |
| `#f1f5f9`, `#f3f4f6`, `#f5f5f5`, `#f0f0f0`, `#F3F4F6` | `bg-muted` |
| `bg-gray-50`, `bg-gray-100` | `bg-muted` or `bg-accent` |
| `bg-gray-200` | `bg-skeleton` (loading states) |
| `text-gray-500`, `text-gray-600` | `text-muted-foreground` |
| `text-gray-700`, `text-gray-900` | `text-foreground` |
| `border-gray-200`, `border-gray-300` | `border-border` |
| Success greens (`#16a34a`, `#166534`, `#dcfce7`, `#f0fdf4`) | `text-success` / `bg-success/10` |
| Error reds (`#dc2626`, `#b91c1c`, `#991b1b`, `#fee2e2`, `#fef2f2`) | `text-destructive` / `bg-destructive/10` |
| Info blues (`#1e40af`, `#3b82f6`, `#dbeafe`, `#eff6ff`, `#f0f9ff`) | `text-info` / `bg-info/10` |
| Warning ambers (`#f59e0b`, `#92400e`, `#856404`, `#fef3c7`, `#fff3cd`) | `text-warning` / `bg-warning/10` |

#### Edge Cases

- **Account type colors** (AccountBalanceCards.tsx): Keep Tailwind color classes, add `dark:` counterparts:
  - `bg-green-50 dark:bg-green-950`, `text-green-900 dark:text-green-100`
  - Same pattern for red, purple, amber, gray
- **User-chosen dynamic colors** (category icons, tag colors): Keep as inline styles — intentionally user-controlled
- **ECharts charts** (migrated from Plotly in Issue #1169): Handled via `bb-light`/`bb-dark` ECharts theme registration in `shared/charts/themes.ts`. The `<EChartsChart isDark={isDark}>` prop triggers a `key`-based remount on theme flip — see `EChartsChart.tsx`.
- **Tier/status badges**: Use semantic opacity variants (`bg-success/10 text-success`)
- **Hover states**: `hover:bg-accent` replaces `hover:bg-gray-50`, `hover:bg-gray-100`, etc.
- **Focus rings**: Already handled by `ring-ring` in shadcn/ui base styles
- **Skeleton loaders**: New `bg-skeleton` token with `animate-pulse`

#### Files to Convert & Delete

**Admin CSS files (13)** — convert all to Tailwind, delete CSS:
- `AdminDashboardPage.css` → inline Tailwind on `AdminDashboardPage.tsx`
- `AdminLayout.css` → inline on `AdminLayout.tsx`
- `AdminSidebar.css` → inline on `AdminSidebar.tsx`
- `AdminSettingsSection.css` → inline on `AdminSettingsSection.tsx`
- `EnvironmentBadge.css` → inline on `EnvironmentBadge.tsx`
- `QuickActionLink.css` → inline on `QuickActionLink.tsx`
- `SummaryCard.css` → inline on `SummaryCard.tsx`
- `HealthDashboardPage.css` → inline on `HealthDashboardPage.tsx`
- `UsersPage.css` → inline on `UsersPage.tsx`
- `UserDetailModal.css` → inline on `UserDetailModal.tsx`
- `SubscriptionOverrideModal.css` → inline on `SubscriptionOverrideModal.tsx`
- `WebhookDetailModal.css` → inline on `WebhookDetailModal.tsx`
- `WebhooksPage.css` → inline on `WebhooksPage.tsx`

**Feature CSS files (22)** — convert all to Tailwind, delete CSS:
- `ProfilePage.css`, `PrivacySettings.css`, `SubscriptionCard.css`
- `PricingPage.css`, `CheckoutSuccessPage.css`
- `CancelDeletionPage.css`, `DeleteAccountSection.css`
- `TransactionsPage.css`, `TransactionEditor.css`, `TransactionCard.css`
- `CategoryEditor.css`, `TagEditor.css`
- `UpgradeBanner.css`
- `RuleExecutionHistory.css`, `RuleTestResults.css`
- `AutoCategorizationPage.css`, `AutoCategorizationRuleCard.css`, `AutoCategorizationRuleEditor.css`
- `BudgetCard.css`, `BudgetProgressBar.css`, `SavingsGoalCard.css`
- `SearchableSelect.css`

**CSS files using wrong dark strategy (5)** — convert to Tailwind, delete CSS:
- `CookieConsent.css`, `App.css`, `PlaidLinkButton.css`, `AccountsPage.css`, `AccountCard.css`

**TSX files needing dark: variants (15+)** — add dark: counterparts or replace with semantic tokens:
- `TransactionFilterPanel.tsx` + 5 filter components
- `ProtectedRoute.tsx`, `PremiumRoute.tsx`, `AdminRoute.tsx`
- `ErrorBoundary.tsx`, `AccountBalanceCards.tsx`, `NetWorthDisplay.tsx`
- `PlanChangeModal.tsx`, `CancelConfirmationModal.tsx`
- `AllocationBreakdown.tsx` (inline styles)
- `CategoryEditor.tsx` (inline style for no-category dot)

### 6. Chart System Update

**File**: `apps/web/src/shared/charts/useDarkMode.ts`

**Current**: Reads `window.matchMedia('(prefers-color-scheme: dark)')`

**New**: 
1. Check `document.documentElement.classList.contains('dark')`
2. Use `MutationObserver` on `<html>` to watch for class changes
3. Return reactive boolean that updates when theme toggles

This keeps `THEME_COLORS` in `colors.ts` and all chart layouts in sync with the class-based theme system.

### 7. PostHog Considerations

- Theme toggle events: Fire `theme_changed` event with `{ theme: 'light' | 'dark' | 'system', resolved: 'light' | 'dark' }` 
- Session replay: Dark mode state should be captured automatically (class on `<html>`)
- No additional PII masking needed for theme preference

## Verification Plan

### Automated Testing
1. **Unit tests**: ThemeProvider, useTheme hook, useDarkMode hook
2. **Playwright dark mode fixture**: Helper to set theme before page load
   ```ts
   async function setDarkMode(page: Page, dark: boolean) {
     await page.addInitScript(() => {
       localStorage.setItem('bb-theme', dark ? 'dark' : 'light');
     });
   }
   ```
3. **Existing E2E tests**: Should continue to pass (no functional changes)

### Manual QA Rounds
Walk every page in the QA instance in both themes:
1. **Round 1**: Dashboard, accounts, transactions, transaction filters
2. **Round 2**: Categories, tags, budgets, savings goals
3. **Round 3**: Rules, auto-categorization, rule test results
4. **Round 4**: Profile, privacy settings, subscription, pricing
5. **Round 5**: Admin pages (all), health dashboard
6. **Round 6**: Modals/dialogs (transaction editor, category editor, plan change, etc.)
7. **Round 7**: Edge cases (loading skeletons, error states, empty states, cookie consent)

### Contrast Audit
- Run axe-core accessibility checks in both light and dark modes
- Verify WCAG AA compliance (4.5:1 text contrast, 3:1 UI components)

## Impeccable Skills Applied

| Skill | Application |
|-------|-------------|
| `normalize` | Standardize all CSS to one approach (Tailwind utilities + semantic tokens) |
| `adapt` | Dark mode as a context adaptation — rethink color, not just invert |
| `colorize` | Use oklch semantic tokens for consistent, perceptually uniform color across themes |
| `extract` | Extract reusable patterns: skeleton token, status badge variants, theme toggle component |
| `simplify` | Eliminate ~40 CSS files, reduce to one approach, flatten complexity |
| `optimize` | FOUC prevention, no external library, efficient MutationObserver for charts |
| `frontend-design` | No AI slop colors, tinted neutrals via oklch, purposeful color semantics |
| `audit` | Comprehensive codebase scan identified all 55+ affected files across 4 categories |

## Risks

1. **Big PR size**: ~55+ files changed. Mitigate with thorough QA and good commit organization within the PR.
2. **CSS specificity issues**: Some CSS files may have specificity that Tailwind classes don't match. Test each conversion carefully.
3. **Complex CSS (animations, pseudo-elements)**: Some CSS files have complex selectors that can't be expressed purely in Tailwind. These may need `@apply` or targeted CSS kept in the component.
4. **Third-party component styling**: Ensure Plaid Link, Clerk components, and other third-party UIs aren't broken by theme changes.
