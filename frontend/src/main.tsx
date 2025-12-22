import React from 'react'
import ReactDOM from 'react-dom/client'
import { Auth0Provider, useAuth0 } from '@auth0/auth0-react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { store } from './store/store'
import { AuthWrapper } from './auth'
import { setupAuthInterceptor } from './api/axiosConfig'
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

// Get Auth0 configuration from runtime config (injected by docker-entrypoint.sh)
// Falls back to build-time env vars for local development
// In test/development environment without credentials, use placeholders
const isTestEnv = import.meta.env.MODE === 'test' || (typeof window !== 'undefined' && (window as { __VITEST__?: boolean }).__VITEST__)
const isDevelopment = import.meta.env.MODE === 'development'

let auth0Domain: string
let auth0ClientId: string
let auth0Audience: string | undefined

if (isTestEnv) {
  // Unit tests (Vitest): always use placeholders
  auth0Domain = 'placeholder.auth0.com'
  auth0ClientId = 'placeholder-client-id'
  auth0Audience = 'placeholder-audience'
} else {
  const domain = window.ENV?.AUTH0_DOMAIN || import.meta.env.VITE_AUTH0_DOMAIN
  const clientId = window.ENV?.AUTH0_CLIENT_ID || import.meta.env.VITE_AUTH0_CLIENT_ID

  if (!domain || !clientId) {
    // In development (E2E tests, local dev without .env), use placeholders
    // In production/staging, fail fast
    if (isDevelopment) {
      console.warn('[Auth0] No credentials found - using placeholders (development mode)')
      auth0Domain = 'dev-placeholder.auth0.com'
      auth0ClientId = 'dev-placeholder-client-id'
      auth0Audience = undefined
    } else {
      throw new Error('FATAL: Auth0 configuration not loaded. window.ENV is undefined or empty. Check that config.js is loading properly.')
    }
  } else {
    auth0Domain = domain
    auth0ClientId = clientId
    auth0Audience = window.ENV?.AUTH0_AUDIENCE || import.meta.env.VITE_AUTH0_AUDIENCE
  }
}

// Component to initialize axios interceptor with Auth0 token
const InitializeAuth = () => {
  const { getAccessTokenSilently } = useAuth0()

  React.useEffect(() => {
    const tokenProvider = async () => {
      try {
        return await getAccessTokenSilently()
      } catch {
        return null
      }
    }

    // Setup legacy axios interceptor (for existing API calls during migration)
    setupAuthInterceptor(tokenProvider)

    // Setup new client auth provider (for generated API calls)
    setAuthTokenProvider(tokenProvider)
  }, [getAccessTokenSilently])

  return null
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <Auth0Provider
          domain={auth0Domain}
          clientId={auth0ClientId}
          authorizationParams={{
            redirect_uri: `${window.location.origin}/callback`,
            audience: auth0Audience,
          }}
          useRefreshTokens={true}
          cacheLocation="localstorage"
        >
          <Provider store={store}>
            <BrowserRouter>
              <AuthWrapper>
                <InitializeAuth />
                <App />
              </AuthWrapper>
            </BrowserRouter>
          </Provider>
        </Auth0Provider>
      </QueryClientProvider>
    </ErrorBoundary>
  </React.StrictMode>,
)
