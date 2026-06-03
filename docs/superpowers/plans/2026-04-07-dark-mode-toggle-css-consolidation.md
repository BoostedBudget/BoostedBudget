# Dark Mode Toggle & CSS Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make dark/light mode fully functional and toggleable across the entire BoostedBudget frontend, consolidating ~40 CSS files into Tailwind utilities with semantic tokens.

**Architecture:** Custom ThemeProvider toggles `.dark` class on `<html>`. FOUC prevented by blocking script in `index.html`. All standalone CSS files migrated to Tailwind utility classes using the existing oklch semantic variable system from `index.css`. Theme preference persisted to localStorage with OS system preference as default.

**Tech Stack:** React 19, Tailwind CSS v4, shadcn/ui, oklch CSS variables, Vite 7

**Design Spec:** `docs/superpowers/specs/2026-04-07-dark-mode-toggle-and-css-consolidation-design.md`

---

## File Structure

### New Files
- `apps/web/src/shared/theme/ThemeProvider.tsx` — React context + provider component
- `apps/web/src/shared/theme/useTheme.ts` — Consumer hook
- `apps/web/src/shared/theme/index.ts` — Barrel export
- `apps/web/src/features/profile/components/ThemeSelector.tsx` — Theme toggle UI
- `apps/web/src/features/profile/components/ThemeSelector.test.tsx` — Tests
- `apps/web/src/shared/theme/__tests__/ThemeProvider.test.tsx` — Provider tests

### Modified Files
- `apps/web/index.html` — Add FOUC-prevention script
- `apps/web/src/index.css` — Remove Vite scaffold defaults, add skeleton token
- `apps/web/src/main.tsx` — Wrap App in ThemeProvider
- `apps/web/src/shared/charts/useDarkMode.ts` — Read `.dark` class via MutationObserver
- `apps/web/src/shared/analytics/events.ts` — Add THEME_CHANGED event
- `apps/web/src/features/profile/ProfilePage.tsx` — Add ThemeSelector, remove CSS import
- `apps/web/src/App.tsx` — Remove App.css import
- ~55 TSX files — Replace hardcoded colors with semantic Tailwind classes
- ~15 TSX files — Add `dark:` variants to existing Tailwind classes

### Deleted Files (~40 CSS files)
All standalone CSS files listed in the design spec sections 5.5 (admin: 13 files, features: 22 files, wrong-strategy: 5 files).

---

## Task 1: FOUC Prevention Script & index.css Fixes

**Files:**
- Modify: `apps/web/index.html`
- Modify: `apps/web/src/index.css:6-19` (`:root` block) and `:129` (`@theme inline`)

- [ ] **Step 1: Write test for FOUC script behavior**

Create `apps/web/src/shared/theme/__tests__/fouc-script.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'

describe('FOUC prevention script logic', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  function applyTheme() {
    const stored = localStorage.getItem('bb-theme')
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    if (stored === 'dark' || (stored !== 'light' && prefersDark)) {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
  }

  it('adds dark class when localStorage is "dark"', () => {
    localStorage.setItem('bb-theme', 'dark')
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not add dark class when localStorage is "light"', () => {
    localStorage.setItem('bb-theme', 'light')
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('does not add dark class when no preference and system is light', () => {
    // vitest default: prefers-color-scheme is light
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/web && bun run test -- --run src/shared/theme/__tests__/fouc-script.test.ts`
Expected: FAIL (file doesn't exist yet — create it first, then tests should pass since logic is inline)

- [ ] **Step 3: Add FOUC-prevention script to index.html**

In `apps/web/index.html`, add this script as the FIRST child of `<head>`, before any `<meta>` tags:

```html
<script>
(function(){
  var s=localStorage.getItem('bb-theme');
  var d=window.matchMedia('(prefers-color-scheme:dark)').matches;
  if(s==='dark'||(s!=='light'&&d))document.documentElement.classList.add('dark');
})();
</script>
```

- [ ] **Step 4: Fix index.css — remove Vite scaffold defaults**

In `apps/web/src/index.css`, remove these 3 lines from the `:root` block (lines 11-13):

```css
/* DELETE: */
  color-scheme: light dark;
  color: rgba(255, 255, 255, 0.87);
  background-color: #242424;
```

- [ ] **Step 5: Add skeleton token to index.css**

In `:root` block (after `--sidebar-ring`), add:
```css
  --skeleton: oklch(0.905 0 0);
```

In `.dark` block (after `--sidebar-ring`), add:
```css
  --skeleton: oklch(0.371 0 0);
```

In `@theme inline` block, add:
```css
  --color-skeleton: var(--skeleton);
```

- [ ] **Step 6: Run tests to verify**

Run: `cd apps/web && bun run test -- --run src/shared/theme/__tests__/fouc-script.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/web/index.html apps/web/src/index.css apps/web/src/shared/theme/__tests__/fouc-script.test.ts
git commit -m "#835: Add FOUC prevention script and fix index.css defaults"
```

---

## Task 2: ThemeProvider & useTheme Hook

**Files:**
- Create: `apps/web/src/shared/theme/ThemeProvider.tsx`
- Create: `apps/web/src/shared/theme/useTheme.ts`
- Create: `apps/web/src/shared/theme/index.ts`
- Create: `apps/web/src/shared/theme/__tests__/ThemeProvider.test.tsx`
- Modify: `apps/web/src/main.tsx`

- [ ] **Step 1: Write failing tests for ThemeProvider**

Create `apps/web/src/shared/theme/__tests__/ThemeProvider.test.tsx`:

```tsx
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { ThemeProvider } from '../ThemeProvider'
import { useTheme } from '../useTheme'

function ThemeConsumer() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <span data-testid="resolved">{resolvedTheme}</span>
      <button onClick={() => setTheme('dark')}>Set Dark</button>
      <button onClick={() => setTheme('light')}>Set Light</button>
      <button onClick={() => setTheme('system')}>Set System</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('defaults to system theme', () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    expect(screen.getByTestId('theme').textContent).toBe('system')
  })

  it('applies dark class when theme is set to dark', async () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Dark').click()
    })
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByTestId('resolved').textContent).toBe('dark')
  })

  it('removes dark class when theme is set to light', async () => {
    document.documentElement.classList.add('dark')
    render(
      <ThemeProvider defaultTheme="dark">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Light').click()
    })
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(screen.getByTestId('resolved').textContent).toBe('light')
  })

  it('persists theme to localStorage', async () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Dark').click()
    })
    expect(localStorage.getItem('bb-theme')).toBe('dark')
  })

  it('reads theme from localStorage on mount', () => {
    localStorage.setItem('bb-theme', 'dark')
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    expect(screen.getByTestId('theme').textContent).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd apps/web && bun run test -- --run src/shared/theme/__tests__/ThemeProvider.test.tsx`
Expected: FAIL (modules don't exist)

- [ ] **Step 3: Implement ThemeProvider**

Create `apps/web/src/shared/theme/ThemeProvider.tsx`:

```tsx
import { createContext, useCallback, useEffect, useMemo, useState } from 'react'

export type Theme = 'light' | 'dark' | 'system'

export interface ThemeContextValue {
  theme: Theme
  resolvedTheme: 'light' | 'dark'
  setTheme: (theme: Theme) => void
}

export const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

const STORAGE_KEY = 'bb-theme'

function getSystemTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function getStoredTheme(): Theme | null {
  if (typeof window === 'undefined') return null
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  return null
}

interface ThemeProviderProps {
  children: React.ReactNode
  defaultTheme?: Theme
}

export function ThemeProvider({ children, defaultTheme = 'system' }: ThemeProviderProps) {
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme() ?? defaultTheme)
  const [systemTheme, setSystemTheme] = useState<'light' | 'dark'>(getSystemTheme)

  const resolvedTheme = theme === 'system' ? systemTheme : theme

  const applyTheme = useCallback((resolved: 'light' | 'dark') => {
    const root = document.documentElement
    if (resolved === 'dark') {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
  }, [])

  const setTheme = useCallback((newTheme: Theme) => {
    setThemeState(newTheme)
    localStorage.setItem(STORAGE_KEY, newTheme)
  }, [])

  // Apply theme on mount and when resolved theme changes
  useEffect(() => {
    applyTheme(resolvedTheme)
  }, [resolvedTheme, applyTheme])

  // Listen for system theme changes
  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (e: MediaQueryListEvent) => setSystemTheme(e.matches ? 'dark' : 'light')
    mediaQuery.addEventListener('change', handler)
    return () => mediaQuery.removeEventListener('change', handler)
  }, [])

  const value = useMemo(
    () => ({ theme, resolvedTheme, setTheme }),
    [theme, resolvedTheme, setTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
```

- [ ] **Step 4: Implement useTheme hook**

Create `apps/web/src/shared/theme/useTheme.ts`:

```ts
import { useContext } from 'react'
import { ThemeContext } from './ThemeProvider'
import type { ThemeContextValue } from './ThemeProvider'

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}
```

- [ ] **Step 5: Create barrel export**

Create `apps/web/src/shared/theme/index.ts`:

```ts
export { ThemeProvider } from './ThemeProvider'
export { useTheme } from './useTheme'
export type { Theme, ThemeContextValue } from './ThemeProvider'
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd apps/web && bun run test -- --run src/shared/theme/__tests__/ThemeProvider.test.tsx`
Expected: PASS

- [ ] **Step 7: Wire ThemeProvider into main.tsx**

In `apps/web/src/main.tsx`, add import:
```ts
import { ThemeProvider } from './shared/theme'
```

Wrap `<App />` — insert `<ThemeProvider>` inside `<QueryClientProvider>` and around `<App />`:
```tsx
<QueryClientProvider client={queryClient}>
  <ThemeProvider defaultTheme="system">
    <App />
  </ThemeProvider>
</QueryClientProvider>
```

- [ ] **Step 8: Run full frontend tests**

Run: `cd apps/web && bun run test`
Expected: All tests pass

- [ ] **Step 9: Commit**

```bash
git add apps/web/src/shared/theme/ apps/web/src/main.tsx
git commit -m "#835: Add ThemeProvider with localStorage persistence and system preference fallback"
```

---

## Task 3: Update useDarkMode Chart Hook

**Files:**
- Modify: `apps/web/src/shared/charts/useDarkMode.ts`

- [ ] **Step 1: Write failing test**

Create `apps/web/src/shared/charts/__tests__/useDarkMode.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDarkMode } from '../useDarkMode'

describe('useDarkMode', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('returns false when no dark class on html', () => {
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)
  })

  it('returns true when dark class is on html', () => {
    document.documentElement.classList.add('dark')
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)
  })

  it('reacts to dark class being added via MutationObserver', async () => {
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)

    await act(async () => {
      document.documentElement.classList.add('dark')
      // MutationObserver is async, give it a tick
      await new Promise(resolve => setTimeout(resolve, 0))
    })
    expect(result.current).toBe(true)
  })

  it('reacts to dark class being removed', async () => {
    document.documentElement.classList.add('dark')
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)

    await act(async () => {
      document.documentElement.classList.remove('dark')
      await new Promise(resolve => setTimeout(resolve, 0))
    })
    expect(result.current).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/web && bun run test -- --run src/shared/charts/__tests__/useDarkMode.test.ts`
Expected: FAIL (current hook reads media query, not `.dark` class)

- [ ] **Step 3: Rewrite useDarkMode to use MutationObserver on html element**

Replace `apps/web/src/shared/charts/useDarkMode.ts` entirely:

```ts
import { useState, useEffect } from 'react'

export function useDarkMode(): boolean {
  const [isDark, setIsDark] = useState(() => {
    if (typeof window === 'undefined') return false
    return document.documentElement.classList.contains('dark')
  })

  useEffect(() => {
    const root = document.documentElement

    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        if (mutation.attributeName === 'class') {
          setIsDark(root.classList.contains('dark'))
        }
      }
    })

    observer.observe(root, { attributes: true, attributeFilter: ['class'] })

    // Sync initial state
    setIsDark(root.classList.contains('dark'))

    return () => observer.disconnect()
  }, [])

  return isDark
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd apps/web && bun run test -- --run src/shared/charts/__tests__/useDarkMode.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/shared/charts/useDarkMode.ts apps/web/src/shared/charts/__tests__/useDarkMode.test.ts
git commit -m "#835: Update useDarkMode hook to read .dark class via MutationObserver"
```

---

## Task 4: Theme Selector Component & Analytics Event

**Files:**
- Create: `apps/web/src/features/profile/components/ThemeSelector.tsx`
- Create: `apps/web/src/features/profile/components/ThemeSelector.test.tsx`
- Modify: `apps/web/src/features/profile/ProfilePage.tsx`
- Modify: `apps/web/src/shared/analytics/events.ts`

- [ ] **Step 1: Add THEME_CHANGED analytics event**

In `apps/web/src/shared/analytics/events.ts`, add after the Profile section (after `PROFILE_UPDATED`):

```ts
  // Theme
  THEME_CHANGED: 'theme_changed',
```

- [ ] **Step 2: Write failing test for ThemeSelector**

Create `apps/web/src/features/profile/components/ThemeSelector.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeSelector } from './ThemeSelector'
import { ThemeProvider } from '@/shared/theme'

describe('ThemeSelector', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('renders three theme options', () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeSelector />
      </ThemeProvider>
    )
    expect(screen.getByText('Light')).toBeInTheDocument()
    expect(screen.getByText('Dark')).toBeInTheDocument()
    expect(screen.getByText('System')).toBeInTheDocument()
  })

  it('highlights the current theme', () => {
    localStorage.setItem('bb-theme', 'dark')
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeSelector />
      </ThemeProvider>
    )
    const darkButton = screen.getByText('Dark').closest('button')
    expect(darkButton?.getAttribute('data-active')).toBe('true')
  })

  it('changes theme when option is clicked', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeSelector />
      </ThemeProvider>
    )
    await user.click(screen.getByText('Dark'))
    expect(localStorage.getItem('bb-theme')).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd apps/web && bun run test -- --run src/features/profile/components/ThemeSelector.test.tsx`
Expected: FAIL (component doesn't exist)

- [ ] **Step 4: Implement ThemeSelector component**

Create `apps/web/src/features/profile/components/ThemeSelector.tsx`:

```tsx
import { Sun, Moon, Monitor } from 'lucide-react'
import { useTheme } from '@/shared/theme'
import { useTrackEvent } from '@/shared/analytics/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { cn } from '@/lib/utils'
import type { Theme } from '@/shared/theme'

const options: { value: Theme; label: string; icon: typeof Sun; description: string }[] = [
  { value: 'light', label: 'Light', icon: Sun, description: 'Always use light theme' },
  { value: 'dark', label: 'Dark', icon: Moon, description: 'Always use dark theme' },
  { value: 'system', label: 'System', icon: Monitor, description: 'Match your device settings' },
]

export function ThemeSelector() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const trackEvent = useTrackEvent()

  function handleSelect(newTheme: Theme) {
    setTheme(newTheme)
    trackEvent(ANALYTICS_EVENTS.THEME_CHANGED, {
      theme: newTheme,
      resolved: newTheme === 'system' ? resolvedTheme : newTheme,
    })
  }

  return (
    <div className="space-y-3">
      <div>
        <h3 className="text-sm font-medium text-foreground">Appearance</h3>
        <p className="text-sm text-muted-foreground">Choose how BoostedBudget looks to you</p>
      </div>
      <div className="flex gap-2">
        {options.map(({ value, label, icon: Icon }) => (
          <button
            key={value}
            data-active={theme === value}
            onClick={() => handleSelect(value)}
            className={cn(
              'flex items-center gap-2 rounded-lg border px-4 py-2.5 text-sm font-medium transition-colors',
              theme === value
                ? 'border-primary bg-primary/5 text-foreground'
                : 'border-border bg-card text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
          >
            <Icon className="h-4 w-4" />
            {label}
          </button>
        ))}
      </div>
      {theme === 'system' && (
        <p className="text-xs text-muted-foreground">
          Currently using {resolvedTheme} mode
        </p>
      )}
    </div>
  )
}
```

- [ ] **Step 5: Add ThemeSelector to ProfilePage**

In `apps/web/src/features/profile/ProfilePage.tsx`:

Add import:
```ts
import { ThemeSelector } from './components/ThemeSelector'
```

Add `<ThemeSelector />` in the profile tab content, before `<SubscriptionCard>` or after the Account Information section. Place it in a section wrapper:

```tsx
<div className="rounded-lg border border-border bg-card p-6">
  <ThemeSelector />
</div>
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd apps/web && bun run test -- --run src/features/profile/components/ThemeSelector.test.tsx`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/web/src/features/profile/components/ThemeSelector.tsx apps/web/src/features/profile/components/ThemeSelector.test.tsx apps/web/src/features/profile/ProfilePage.tsx apps/web/src/shared/analytics/events.ts
git commit -m "#835: Add ThemeSelector component with analytics event"
```

---

## Task 5: Migrate App.css to Tailwind & Remove Vite Scaffold Styles

**Files:**
- Modify: `apps/web/src/App.tsx`
- Modify: `apps/web/src/App.css`

- [ ] **Step 1: Migrate App.css styles to Tailwind**

The `App.css` has two categories of styles:
1. `.app`, `.app--landing`, `.app-main` — layout styles
2. `.health-check`, `.info`, `.status-*` — health check styles (likely dev-only)

For the layout styles, convert to Tailwind in `App.tsx`:
- `.app` → keep as a class name for the landing page conditional, but add Tailwind: `min-h-screen bg-background text-foreground`
- `.app-main` → `flex-1`

For the health check styles: these appear unused in the main App (they're an older dev health check page). Check if still referenced. If not, delete entirely. If yes, inline as Tailwind.

Replace the `@media (prefers-color-scheme: light)` block entirely — the theme system now handles this.

- [ ] **Step 2: Update App.tsx**

Remove `import './App.css'` from `App.tsx`.

Update the root div:
```tsx
<div className={cn('min-h-screen bg-background text-foreground', isLandingPage && 'app--landing')}>
```

If the `app--landing` class is still needed for the `#root:has(.landing-page)` CSS in `index.css`, keep it. Otherwise add a `landing-page` class directly.

- [ ] **Step 3: Delete or minimize App.css**

If the `health-check` styles are unused, delete `App.css` entirely. If referenced somewhere, keep only those styles and convert them to use semantic tokens.

- [ ] **Step 4: Run frontend tests and verify build**

Run: `cd apps/web && bun run test && bun run build`
Expected: All pass, build succeeds

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/App.tsx apps/web/src/App.css
git commit -m "#835: Migrate App.css to Tailwind semantic tokens"
```

---

## Task 6: Migrate Admin CSS Files (13 files)

**Files:**
- Modify + delete CSS: All 13 admin component pairs in `apps/web/src/features/admin/components/`

This is the largest single task. Each CSS file is converted to Tailwind utility classes on the corresponding TSX component, then the CSS file is deleted.

**Token mapping reference** (from design spec):

| CSS Value | Tailwind Class |
|-----------|---------------|
| `white`, `#f8fafc` | `bg-card` |
| `#1e293b`, `#111827` | `text-foreground` |
| `#64748b`, `#6b7280`, `#475569` | `text-muted-foreground` |
| `#e2e8f0`, `#374151` (dark) | `border-border` |
| `#f1f5f9` | `bg-muted` |
| `#f59e0b` (amber header) | `bg-amber-500 dark:bg-amber-800` |
| `#78350f` (amber text) | `text-amber-900 dark:text-amber-100` |
| `#3b82f6` (blue accents) | `text-info` or `border-info` |
| `#22c55e` (success) | `text-success` or `border-success` |
| `#ef4444` (error) | `text-destructive` or `border-destructive` |
| `#f59e0b` (warning) | `text-warning` or `border-warning` |
| Status badge greens | `bg-success/10 text-success` |
| Status badge reds | `bg-destructive/10 text-destructive` |
| Status badge blues | `bg-info/10 text-info` |
| Status badge yellows | `bg-warning/10 text-warning` |

- [ ] **Step 1: Migrate AdminLayout.css → AdminLayout.tsx**

In `AdminLayout.tsx`, remove `import './AdminLayout.css'`.

Convert the layout div classes to Tailwind:
```tsx
<div className="flex flex-col min-h-screen">
  <div className="flex items-center gap-2 px-4 py-2 text-sm bg-amber-500 text-amber-900 dark:bg-amber-800 dark:text-amber-100">
    <Shield size={18} />
    <span><strong>Admin Mode</strong> — You are viewing as administrator</span>
  </div>
  <div className="flex flex-1">
    <AdminSidebar />
    <main className="flex-1 p-6 bg-background">
      <Outlet />
    </main>
  </div>
</div>
```

Delete `AdminLayout.css`.

- [ ] **Step 2: Migrate AdminSidebar.css → AdminSidebar.tsx**

Remove CSS import. Convert all sidebar styles to Tailwind using semantic tokens. The sidebar background becomes `bg-card`, links become `text-muted-foreground hover:text-foreground hover:bg-accent`, active state becomes `bg-accent text-foreground font-medium`. Delete `AdminSidebar.css`.

- [ ] **Step 3: Migrate AdminDashboardPage.css → AdminDashboardPage.tsx**

Remove CSS import. Convert page title to `text-foreground`, section headers to `text-foreground font-semibold`, cards to `bg-card rounded-lg border border-border`. Delete `AdminDashboardPage.css`.

- [ ] **Step 4: Migrate SummaryCard.css → SummaryCard.tsx**

Remove CSS import. Convert using semantic tokens:
- Card: `bg-card border border-border rounded-lg p-4 hover:shadow-md transition-shadow`
- Status borders: `border-l-4` with `border-info` (neutral), `border-success`, `border-warning`, `border-destructive`
- Icon/title: `text-muted-foreground`
- Value: `text-foreground text-2xl font-bold`
- Trend up: `text-success`, trend down: `text-destructive`, trend neutral: `text-muted-foreground`

Delete `SummaryCard.css`.

- [ ] **Step 5: Migrate QuickActionLink.css → QuickActionLink.tsx**

Remove CSS import. Convert: card → `bg-card border border-border rounded-lg p-4 hover:bg-accent transition-colors`, icon container → `bg-muted rounded-lg p-2`, label → `text-foreground font-medium`, description → `text-muted-foreground text-sm`. Delete `QuickActionLink.css`.

- [ ] **Step 6: Migrate EnvironmentBadge.css → EnvironmentBadge.tsx**

Remove CSS import. Convert badge variants to semantic Tailwind:
- production: `bg-destructive/10 text-destructive border border-destructive/20`
- staging: `bg-warning/10 text-warning border border-warning/20`
- development: `bg-info/10 text-info border border-info/20`
- test: `bg-muted text-muted-foreground border border-border`

Delete `EnvironmentBadge.css`.

- [ ] **Step 7: Migrate AdminSettingsSection.css → AdminSettingsSection.tsx**

Remove CSS import. Convert sections to `bg-card border border-border rounded-lg p-6`, headings to `text-foreground font-semibold`, labels to `text-muted-foreground`, values to `text-foreground`. Delete `AdminSettingsSection.css`.

- [ ] **Step 8: Migrate HealthDashboardPage.css → HealthDashboardPage.tsx**

Remove CSS import. This is a complex page — convert status indicators to semantic colors, queue cards to `bg-card border border-border`, dead-letter alerts to `bg-warning/10 border border-warning/20 text-warning`. Delete `HealthDashboardPage.css`.

- [ ] **Step 9: Migrate UsersPage.css → UsersPage.tsx**

Remove CSS import. This file uses undefined CSS variables — replace ALL instances of `var(--text-primary)` etc. with Tailwind semantic classes. Tier badges use `bg-success/10 text-success` pattern. Status badges similarly. Delete `UsersPage.css`.

- [ ] **Step 10: Migrate UserDetailModal.css → UserDetailModal.tsx**

Remove CSS import. Same pattern as UsersPage — replace undefined CSS vars with Tailwind semantics. Delete `UserDetailModal.css`.

- [ ] **Step 11: Migrate SubscriptionOverrideModal.css → SubscriptionOverrideModal.tsx**

Remove CSS import. Replace undefined CSS vars, convert badge colors. Delete `SubscriptionOverrideModal.css`.

- [ ] **Step 12: Migrate WebhooksPage.css → WebhooksPage.tsx**

Remove CSS import. Replace undefined CSS vars, convert status badges. Delete `WebhooksPage.css`.

- [ ] **Step 13: Migrate WebhookDetailModal.css → WebhookDetailModal.tsx**

Remove CSS import. Replace undefined CSS vars, convert status badges. Delete `WebhookDetailModal.css`.

- [ ] **Step 14: Run frontend tests and lint**

Run: `cd apps/web && bun run test && bun run lint`
Expected: All pass

- [ ] **Step 15: Commit**

```bash
git add apps/web/src/features/admin/
git commit -m "#835: Migrate all admin CSS files to Tailwind semantic tokens"
```

---

## Task 7: Migrate Feature CSS Files — Profile, Pricing, Billing, Deletion (7 files)

**Files:**
- Modify + delete CSS: `ProfilePage.css`, `PrivacySettings.css`, `SubscriptionCard.css`, `PricingPage.css`, `CheckoutSuccessPage.css`, `CancelDeletionPage.css`, `DeleteAccountSection.css`

- [ ] **Step 1: Migrate ProfilePage.css → ProfilePage.tsx**

Remove `import './ProfilePage.css'` from ProfilePage.tsx.

Convert all 312 lines of CSS to Tailwind. Key mappings:
- Page title `#212121` → `text-foreground`
- Tab buttons: `text-muted-foreground hover:text-foreground hover:bg-muted` active: `text-primary border-b-2 border-primary`
- Profile sections: `bg-card border border-border rounded-lg p-6`
- Info grid: `grid grid-cols-1 sm:grid-cols-2 gap-4`
- Info labels: `text-muted-foreground text-sm`
- Info values: `text-foreground font-medium`
- Form inputs: `border border-border rounded-md px-3 py-2 bg-background text-foreground focus:ring-2 focus:ring-ring`
- Buttons: Use shadcn `<Button>` component where possible

Delete `ProfilePage.css`.

- [ ] **Step 2: Migrate PrivacySettings.css → PrivacySettings.tsx**

Remove CSS import. Convert headings → `text-foreground`, sections → `bg-card border border-border`, descriptions → `text-muted-foreground`. Delete `PrivacySettings.css`.

- [ ] **Step 3: Migrate SubscriptionCard.css → SubscriptionCard.tsx**

Remove CSS import. Convert card → `bg-card border border-border rounded-lg`, tier name → `text-foreground font-bold`, price → `text-foreground text-3xl font-bold`, features → `text-muted-foreground`. Delete `SubscriptionCard.css`.

- [ ] **Step 4: Migrate PricingPage.css → PricingPage.tsx**

Remove CSS import. Convert pricing cards → `bg-card border border-border`, featured card → `border-primary ring-2 ring-primary/20`, FAQ section → `bg-muted/50 rounded-lg`. Delete `PricingPage.css`.

- [ ] **Step 5: Migrate CheckoutSuccessPage.css → CheckoutSuccessPage.tsx**

Remove CSS import. Convert container → `bg-card border border-border rounded-lg p-8`, title → `text-foreground`, message → `text-muted-foreground`. Delete `CheckoutSuccessPage.css`.

- [ ] **Step 6: Migrate CancelDeletionPage.css → CancelDeletionPage.tsx**

Remove CSS import. Convert page bg → `bg-background`, card → `bg-card border border-border rounded-lg`. Delete `CancelDeletionPage.css`.

- [ ] **Step 7: Migrate DeleteAccountSection.css → DeleteAccountSection.tsx**

Remove CSS import. Convert warning sections to `bg-destructive/5 border border-destructive/20`, info sections to `bg-info/5 border border-info/20`. Delete `DeleteAccountSection.css`.

- [ ] **Step 8: Run frontend tests**

Run: `cd apps/web && bun run test && bun run lint`
Expected: All pass

- [ ] **Step 9: Commit**

```bash
git add apps/web/src/features/profile/ apps/web/src/features/pricing/ apps/web/src/features/billing/ apps/web/src/features/deletion/
git commit -m "#835: Migrate profile, pricing, billing, deletion CSS to Tailwind"
```

---

## Task 8: Migrate Feature CSS Files — Transactions, Categories, Tags (6 files)

**Files:**
- Modify + delete CSS: `TransactionsPage.css`, `TransactionEditor.css`, `TransactionCard.css`, `CategoryEditor.css`, `TagEditor.css`
- Modify: `TransactionFilterPanel.tsx` + 5 filter sub-components (add `dark:` variants or replace with semantic tokens)

- [ ] **Step 1: Migrate TransactionsPage.css → TransactionsPage.tsx**

Remove CSS import. Convert page title → `text-foreground`, scrollbar styles → keep as minimal CSS-in-JS or Tailwind `scrollbar-*` if available. Delete `TransactionsPage.css`.

- [ ] **Step 2: Migrate TransactionCard.css → TransactionCard.tsx**

Remove CSS import. Convert card → `bg-card border border-border`, merchant name → `text-foreground font-medium`, amount → `text-foreground`, date → `text-muted-foreground text-sm`. Delete `TransactionCard.css`.

- [ ] **Step 3: Migrate TransactionEditor.css → TransactionEditor.tsx**

Remove CSS import. This is a complex modal — convert form fields → `bg-background border border-border`, labels → `text-foreground text-sm font-medium`, inputs → `border border-input rounded-md bg-background text-foreground`. Delete `TransactionEditor.css`.

- [ ] **Step 4: Migrate CategoryEditor.css → CategoryEditor.tsx**

Remove CSS import. Convert form layout, color picker area, icon selector. Use semantic tokens throughout. Delete `CategoryEditor.css`.

- [ ] **Step 5: Migrate TagEditor.css → TagEditor.tsx**

Remove CSS import. Similar pattern to CategoryEditor. Delete `TagEditor.css`.

- [ ] **Step 6: Fix TransactionFilterPanel.tsx and 5 filter components**

In each file, replace hardcoded Tailwind colors with semantic equivalents:
- `bg-white` → `bg-card`
- `border-gray-200` → `border-border`
- `text-gray-900` → `text-foreground`
- `text-gray-700` → `text-foreground`
- `text-gray-600` → `text-muted-foreground`
- `text-gray-500` → `text-muted-foreground`
- `bg-gray-50` → `bg-muted`
- `hover:bg-gray-50` → `hover:bg-accent`
- `border-gray-300` → `border-input`
- `bg-gray-100` → `bg-muted`

Files to update:
- `TransactionFilterPanel.tsx`
- `filters/TransactionAccountFilter.tsx`
- `filters/TransactionTypeFilter.tsx`
- `filters/TransactionStatusFilter.tsx`
- `filters/TransactionAmountFilter.tsx`
- `filters/TransactionCategoryFilter.tsx`

- [ ] **Step 7: Run frontend tests**

Run: `cd apps/web && bun run test && bun run lint`
Expected: All pass

- [ ] **Step 8: Commit**

```bash
git add apps/web/src/features/transactions/ apps/web/src/features/categories/ apps/web/src/features/tags/
git commit -m "#835: Migrate transactions, categories, tags CSS to Tailwind"
```

---

## Task 9: Migrate Feature CSS Files — Rules, Auto-categorization, Budgets, Accounts, Shared (15 files)

**Files:**
- Modify + delete CSS: `RuleExecutionHistory.css`, `RuleTestResults.css`, `AutoCategorizationPage.css`, `AutoCategorizationRuleCard.css`, `AutoCategorizationRuleEditor.css`, `BudgetCard.css`, `BudgetProgressBar.css`, `SavingsGoalCard.css`, `UpgradeBanner.css`, `AccountCard.css`, `AccountsPage.css`, `PlaidLinkButton.css`, `CookieConsent.css`, `SearchableSelect.css`

- [ ] **Step 1: Migrate Rules CSS files (2)**

`RuleExecutionHistory.css` and `RuleTestResults.css` — convert status badge colors to semantic tokens (`text-success`, `text-destructive`, `text-info`), convert backgrounds to `bg-card`. Delete both CSS files.

- [ ] **Step 2: Migrate Auto-categorization CSS files (3)**

`AutoCategorizationPage.css`, `AutoCategorizationRuleCard.css`, `AutoCategorizationRuleEditor.css` — these use CSS variables with light fallbacks. Replace all `var(--color-text-primary, #1a1a1a)` patterns with direct Tailwind classes (`text-foreground`). Delete all 3 CSS files.

- [ ] **Step 3: Migrate Budget CSS files (3)**

`BudgetCard.css`, `BudgetProgressBar.css`, `SavingsGoalCard.css` — same pattern as auto-categorization. Replace CSS variable approach with Tailwind semantic classes. Delete all 3 CSS files.

- [ ] **Step 4: Migrate Accounts CSS files (3)**

`AccountCard.css` — has the most thorough `@media (prefers-color-scheme: dark)` block. Convert all to Tailwind with `dark:` variants for account-specific colors.

`AccountsPage.css` — convert page layout styles. 

`PlaidLinkButton.css` — convert button styles.

Delete all 3 CSS files.

- [ ] **Step 5: Migrate UpgradeBanner.css**

Convert amber warning banner: `bg-warning/10 border border-warning/20 text-warning`. Delete CSS file.

- [ ] **Step 6: Migrate CookieConsent.css**

Convert consent banner to Tailwind. This is a privacy-critical component — ensure the banner is visible and accessible in both themes. Delete CSS file.

- [ ] **Step 7: Migrate SearchableSelect.css**

Convert dropdown styles: options bg → `bg-card`, hover → `bg-accent`, text → `text-foreground`, border → `border-border`. Delete CSS file.

- [ ] **Step 8: Run frontend tests**

Run: `cd apps/web && bun run test && bun run lint`
Expected: All pass

- [ ] **Step 9: Commit**

```bash
git add apps/web/src/features/rules/ apps/web/src/features/auto-categorization/ apps/web/src/features/budgets/ apps/web/src/features/accounts/ apps/web/src/shared/ apps/web/src/privacy/
git commit -m "#835: Migrate remaining feature CSS files to Tailwind"
```

---

## Task 10: Fix TSX Components with Missing dark: Variants

**Files:**
- Modify: `ProtectedRoute.tsx`, `PremiumRoute.tsx`, `AdminRoute.tsx`, `ErrorBoundary.tsx`, `AccountBalanceCards.tsx`, `NetWorthDisplay.tsx`, `PlanChangeModal.tsx`, `CancelConfirmationModal.tsx`, `AllocationBreakdown.tsx`, `CategoryEditor.tsx`

- [ ] **Step 1: Fix loading skeleton components**

In `ProtectedRoute.tsx`, `PremiumRoute.tsx`, and `AdminRoute.tsx`:
- `bg-gray-50` → `bg-muted`
- `bg-gray-200` → `bg-skeleton`

- [ ] **Step 2: Fix ErrorBoundary.tsx**

Already partially has `dark:` variants. Add missing:
- `bg-gray-500` button → `bg-muted-foreground` or `bg-secondary`
- Verify all text colors have dark variants

- [ ] **Step 3: Fix AccountBalanceCards.tsx**

Add `dark:` variants to the account type color map:
```ts
depository: { bg: 'bg-green-50 dark:bg-green-950', border: 'border-green-200 dark:border-green-800', text: 'text-green-900 dark:text-green-100' },
credit: { bg: 'bg-red-50 dark:bg-red-950', border: 'border-red-200 dark:border-red-800', text: 'text-red-900 dark:text-red-100' },
investment: { bg: 'bg-purple-50 dark:bg-purple-950', border: 'border-purple-200 dark:border-purple-800', text: 'text-purple-900 dark:text-purple-100' },
loan: { bg: 'bg-amber-50 dark:bg-amber-950', border: 'border-amber-200 dark:border-amber-800', text: 'text-amber-900 dark:text-amber-100' },
other: { bg: 'bg-muted', border: 'border-border', text: 'text-foreground' },
```

Also fix `text-gray-600` → `text-muted-foreground`.

- [ ] **Step 4: Fix NetWorthDisplay.tsx**

- `text-gray-500` → `text-muted-foreground`
- `text-gray-400` → `text-muted-foreground`

- [ ] **Step 5: Fix PlanChangeModal.tsx**

- `bg-gray-50` → `bg-muted`
- `border-gray-200` → `border-border`
- `text-gray-600` → `text-muted-foreground`
- `text-gray-700` → `text-foreground`
- `bg-blue-50` → `bg-info/10`

- [ ] **Step 6: Fix CancelConfirmationModal.tsx**

- `border-gray-300` → `border-input`

- [ ] **Step 7: Fix AllocationBreakdown.tsx inline styles**

Replace `style={{ backgroundColor: '#d1d5db' }}` with a Tailwind class `bg-border` or `bg-muted-foreground/30`.

- [ ] **Step 8: Fix CategoryEditor.tsx inline style**

Replace `style={{ backgroundColor: '#9CA3AF' }}` with `className="bg-muted-foreground"`.

- [ ] **Step 9: Run full frontend tests**

Run: `cd apps/web && bun run test && bun run lint`
Expected: All pass

- [ ] **Step 10: Commit**

```bash
git add apps/web/src/auth/ apps/web/src/components/ apps/web/src/features/dashboard/ apps/web/src/features/billing/ apps/web/src/features/budgets/ apps/web/src/features/categories/
git commit -m "#835: Fix TSX components with missing dark: variants"
```

---

## Task 11: Final Verification & Cleanup

**Files:**
- Potentially modify: Any files found during audit

- [ ] **Step 1: Search for remaining hardcoded colors**

Run grep for common hardcoded patterns that should have been caught:
```bash
cd apps/web && grep -r "bg-white\|bg-gray-\|text-gray-\|border-gray-\|#[0-9a-fA-F]\{6\}" src/ --include="*.tsx" --include="*.css" -l
```

Fix any remaining files found.

- [ ] **Step 2: Search for remaining CSS file imports**

```bash
cd apps/web && grep -r "import.*\.css" src/ --include="*.tsx" -l | grep -v index.css | grep -v node_modules
```

Any CSS imports found (other than `index.css`) indicate a CSS file that wasn't migrated. Fix these.

- [ ] **Step 3: Verify no undefined CSS variables remain**

```bash
cd apps/web && grep -r "var(--text-primary\|var(--bg-primary\|var(--bg-secondary\|var(--text-secondary\|var(--text-tertiary\|var(--border-primary\|var(--color-primary" src/ --include="*.css" --include="*.tsx" -l
```

Should return NO results.

- [ ] **Step 4: Run full test suite**

Run: `cd apps/web && bun run test && bun run lint`
Run: `cd backend && go test ./... -v`
Expected: All pass

- [ ] **Step 5: Run E2E tests**

Run: `cd apps/web && bun run test:e2e -- --reporter=line`
Expected: All pass

- [ ] **Step 6: Build verification**

Run: `cd apps/web && bun run build`
Expected: Build succeeds with no errors

- [ ] **Step 7: Run code-simplifier subagent**

Dispatch `code-simplifier` subagent to review all changes for cleanup opportunities.

- [ ] **Step 8: Run Codex adversarial review**

Dispatch `codex:codex-rescue` subagent for second-opinion review on all changes.

- [ ] **Step 9: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "#835: Dark mode cleanup and final fixes"
```

---

## Task 12: Update GitHub Issue & Create PR

**Files:** None (git/GitHub operations only)

- [ ] **Step 1: Update GitHub issue #835 with implementation details**

```bash
gh issue comment 835 --body "## Implementation Summary

### What was done
- Added ThemeProvider with system/light/dark modes, persisted to localStorage
- FOUC prevention via blocking script in index.html
- Theme toggle in Profile settings (Appearance section)
- Migrated ~40 standalone CSS files to Tailwind utility classes
- Fixed ~15 TSX components with missing dark: variants
- Updated useDarkMode chart hook to use MutationObserver on .dark class
- Added skeleton design token for loading states
- Added theme_changed PostHog analytics event
- Removed Vite scaffold dark defaults from index.css

### Files changed
- ~55+ files modified
- ~40 CSS files deleted
- 6 new files created (ThemeProvider, useTheme, ThemeSelector + tests)

### Testing
- Unit tests for ThemeProvider, useDarkMode, ThemeSelector
- All existing tests passing
- Manual QA needed for visual verification in both themes"
```

- [ ] **Step 2: Create PR**

Use `/commit-push-pr` skill to:
- Create branch `feature/835-dark-mode-toggle`
- Push all commits
- Create PR with title `#835: Dark mode toggle and CSS consolidation`
- PR body includes `Closes #835`

---

## QA Validation Rounds (Post-PR, Manual)

After the PR is created, use agent-browser / dogfood to validate each page in both light and dark mode on the QA instance:

1. **Round 1**: Dashboard, accounts, transactions, transaction filters
2. **Round 2**: Categories, tags, budgets, savings goals
3. **Round 3**: Rules, auto-categorization, rule test results
4. **Round 4**: Profile (+ verify toggle works), privacy settings, subscription, pricing
5. **Round 5**: Admin pages (all), health dashboard
6. **Round 6**: Modals/dialogs (transaction editor, category editor, plan change, cancel confirmation)
7. **Round 7**: Edge cases (loading skeletons, error states, empty states, cookie consent, Plaid link)

For each round: toggle between light and dark mode, checking for:
- Text readability (no dark-on-dark or light-on-light)
- Border visibility
- Status badge color correctness
- Chart theme sync
- Form input visibility
- Modal/dialog backdrop and content
