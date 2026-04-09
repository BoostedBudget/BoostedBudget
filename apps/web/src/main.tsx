import ReactDOM from 'react-dom/client'
import { StrictMode, useLayoutEffect, useRef } from 'react'
import { ClerkProvider, useAuth } from '@clerk/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { store } from './store/store'
import { setAuthTokenProvider } from '@boostedbudget/api-client/client'
import { ErrorBoundary } from './components/ErrorBoundary'
import { initSentry } from './shared/lib/sentry'
import { initPostHog, posthog } from './shared/lib/posthog'
import { PostHogProvider } from 'posthog-js/react'
import { ThemeProvider } from './shared/theme'
import './index.css'

initSentry()
initPostHog()

// Create React Query client with sensible defaults
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 60 * 1000, // 5 minutes
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

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

  // Don't render children until Clerk is loaded
  if (!isLoaded) {
    return null
  }

  return <>{children}</>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <ThemeProvider defaultTheme="system">
      <ClerkProvider publishableKey={clerkPublishableKey}>
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
