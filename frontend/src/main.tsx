import React from 'react'
import ReactDOM from 'react-dom/client'
import { ClerkProvider, useAuth } from '@clerk/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { store } from './store/store'
import { setAuthTokenProvider } from './api/client'
import { ErrorBoundary } from './components/ErrorBoundary'
import './index.css'

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

// Component to initialize axios interceptor with Clerk token
const InitializeAuth = () => {
  const { getToken } = useAuth()

  React.useEffect(() => {
    const tokenProvider = async () => {
      try {
        return await getToken()
      } catch {
        return null
      }
    }

    // Setup auth token provider for generated API calls
    setAuthTokenProvider(tokenProvider)
  }, [getToken])

  return null
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ClerkProvider publishableKey={clerkPublishableKey}>
        <ErrorBoundary>
          <Provider store={store}>
            <InitializeAuth />
            <QueryClientProvider client={queryClient}>
              <App />
            </QueryClientProvider>
          </Provider>
        </ErrorBoundary>
      </ClerkProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
