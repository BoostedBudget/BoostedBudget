import React from 'react'
import ReactDOM from 'react-dom/client'
import { Auth0Provider, useAuth0 } from '@auth0/auth0-react'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { store } from './store/store'
import { AuthWrapper } from './auth'
import { setupAuthInterceptor } from './api/axiosConfig'
import './index.css'

// Get Auth0 configuration from runtime config (injected by docker-entrypoint.sh)
// Falls back to build-time env vars for local development
const auth0Domain = window.ENV?.AUTH0_DOMAIN || import.meta.env.VITE_AUTH0_DOMAIN
const auth0ClientId = window.ENV?.AUTH0_CLIENT_ID || import.meta.env.VITE_AUTH0_CLIENT_ID
const auth0Audience = window.ENV?.AUTH0_AUDIENCE || import.meta.env.VITE_AUTH0_AUDIENCE

if (!auth0Domain || !auth0ClientId) {
  throw new Error('FATAL: Auth0 configuration not loaded. window.ENV is undefined or empty. Check that config.js is loading properly.')
}

// Component to initialize axios interceptor with Auth0 token
const InitializeAuth = () => {
  const { getAccessTokenSilently } = useAuth0()

  React.useEffect(() => {
    setupAuthInterceptor(async () => {
      try {
        return await getAccessTokenSilently()
      } catch {
        return null
      }
    })
  }, [getAccessTokenSilently])

  return null
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
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
  </React.StrictMode>,
)
