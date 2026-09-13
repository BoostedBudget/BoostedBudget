import ReactDOM from 'react-dom/client'
import { StrictMode, useLayoutEffect, useRef } from 'react'
import { ClerkProvider, useAuth } from '@clerk/react-router'
import { QueryClientProvider } from '@tanstack/react-query'
import { createAppQueryClient } from './shared/lib/queryClient'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router'
import App from './App'
import { store } from './store/store'
import { setAuthTokenProvider } from '@boostedbudget/api-client/client'
import { ErrorBoundary } from './components/ErrorBoundary'
import { initSentry } from './shared/lib/sentry'
import { posthog } from './shared/lib/posthog'
import { PostHogProvider } from 'posthog-js/react'
import { ThemeProvider } from './shared/theme'
import './index.css'

initSentry()

// Create React Query client with sensible defaults
const queryClient = createAppQueryClient()

// Get Clerk configuration from runtime config (injected by docker-entrypoint.sh)
// Falls back to build-time env vars for local development
// In test/development environment without credentials, use placeholders
const isTestEnv = import.meta.env.MODE === 'test' || (typeof window !== 'undefined' && (window as { __VITEST__?: boolean }).__VITEST__)
const isDevelopment = import.meta.env.MODE === 'development'

let clerkPublishableKey: string

if (isTestEnv) {
  // Unit tests (Vitest): always use placeholders
  clerkPublishableKey = 'pk_test_placeholder'
} else {
  const publishableKey = (window as { ENV?: { CLERK_PUBLISHABLE_KEY?: string } }).ENV?.CLERK_PUBLISHABLE_KEY || import.meta.env.VITE_CLERK_PUBLISHABLE_KEY

  if (!publishableKey) {
    // In development (E2E tests, local dev without .env), use placeholders
    // In production/qa, fail fast
    if (isDevelopment) {
      console.warn('[Clerk] No credentials found - using placeholders (development mode)')
      clerkPublishableKey = 'pk_test_dev_placeholder'
    } else {
      throw new Error('FATAL: Clerk configuration not loaded. window.ENV is undefined or empty. Check that config.js is loading properly.')
    }
  } else {
    clerkPublishableKey = publishableKey
  }
}

/**
 * Auth gate component that ensures token provider is set before children render.
 *
 * This fixes a race condition where Dashboard's React Query hooks would fire
 * API calls before setAuthTokenProvider had run, causing 401 errors.
 *
 * Issue #691: E2E auth.setup.ts was failing because API calls were made
 * without auth tokens after page.goto('/dashboard').
 */
const AuthReadyGate = ({ children }: { children: React.ReactNode }) => {
  const { getToken, isLoaded, isSignedIn } = useAuth()
  // Store getToken in a ref so the closure always uses the latest version
  const getTokenRef = useRef(getToken)

  // Set up token provider - re-register when isSignedIn changes to ensure fresh getToken
  useLayoutEffect(() => {
    // Update ref inside effect (not during render) to comply with React rules
    getTokenRef.current = getToken

    if (isLoaded) {
      setAuthTokenProvider(async () => {
        try {
          // Always use the ref which has the latest getToken
          return await getTokenRef.current()
        } catch (error) {
          console.error('[AuthReadyGate] getToken() error:', error)
          return null
        }
      })
    }
  }, [getToken, isLoaded, isSignedIn]) // Re-register when sign-in state or getToken changes

  // ⚠ Paints a shell rather than `null` while Clerk is in flight, and the distinction is the
  // whole point: FCP is FIRST CONTENTFUL PAINT, and `null` is not contentful. Nothing at all
  // reached the screen until Clerk's serialised chain (clerk.browser.js -> /v1/environment ->
  // /v1/client) resolved, which is why #1601 measured FCP ~5.8s while total-blocking-time was
  // 0ms, bootup-time 0.2s and server-response-time 30ms all scored 1 — the page was not busy,
  // it was blank. Cutting the eager payload 36% (847 -> 540 KiB) moved performance 0.55 -> 0.57,
  // because bytes were never the constraint; this gate was.
  //
  // A user staring at a white screen for several seconds is the actual product problem here; the
  // metric follows from fixing it rather than the other way round.
  //
  // ⚠ What this deliberately does NOT change: children are still gated on `isLoaded`, so the
  // race this component exists for (#691 — React Query hooks firing before
  // setAuthTokenProvider registers, producing 401s) is untouched. Making the gate ROUTE-AWARE so
  // public routes render their real content early is the larger change still open on #1601, and
  // it carries that 401 risk; this does not attempt it.
  //
  // It also does not help the E2E suite: every .auth spec navigates to a protected route, so it
  // must wait for Clerk regardless. This is a Lighthouse/UX fix, not a flake fix.
  //
  // Geometry matches the real header (sticky h-16, border-b — see App.tsx's HeaderPlaceholder and
  // Header.tsx:64) so the swap cannot shift layout, and the brand mark is identical in both
  // states so it does not flash.
  if (!isLoaded) {
    return (
      <div data-testid="auth-loading">
        <div
          aria-hidden="true"
          className="sticky top-0 z-50 h-16 border-b border-border bg-background/80 backdrop-blur-sm flex items-center px-4 md:px-6 gap-2"
        >
          <span className="text-2xl">🚀</span>
          <span className="font-bold text-xl hidden sm:inline">BoostedBudget</span>
        </div>
        <span className="sr-only" role="status">
          Loading BoostedBudget
        </span>
      </div>
    )
  }

  return <>{children}</>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <ThemeProvider defaultTheme="system">
      {/* ⚠ signInUrl/signUpUrl point at "/" DELIBERATELY, and omitting them was a real bug.
          This app has no sign-in PAGE by design — it signs in through Clerk's modal, opened by
          openSignIn()/openSignUp() from LandingHeader.tsx. But Clerk defaults its redirect target
          to /sign-in, and App.tsx has no such route, so a Clerk-initiated sign-in redirect fell
          through to `path="*"` and rendered NotFoundPage. A user whose session expired was shown
          "404 Page Not Found" instead of a way to sign back in.
          Evidence (#1598 run 33688968531): a URL assertion caught it directly —
          "unexpected value http://localhost:33600/dashboard" then
          "unexpected value http://localhost:33600/sign-in", and all 12 failing/flaky specs in
          that run captured the 404 page WITH a full authenticated header, because the header
          renders outside <Routes>.
          "/" is the right target: it is the landing page that hosts the sign-in modal, and it is
          already where ProtectedRoute sends unauthenticated users (ProtectedRoute.tsx:34), so
          this makes Clerk agree with the app instead of contradicting it. */}
      <ClerkProvider
        publishableKey={clerkPublishableKey}
        signInUrl="/"
        signUpUrl="/"
      >
        <PostHogProvider client={posthog}>
          <ErrorBoundary>
            <Provider store={store}>
              <AuthReadyGate>
                <QueryClientProvider client={queryClient}>
                  <App />
                </QueryClientProvider>
              </AuthReadyGate>
            </Provider>
          </ErrorBoundary>
        </PostHogProvider>
      </ClerkProvider>
      </ThemeProvider>
    </BrowserRouter>
  </StrictMode>,
)
