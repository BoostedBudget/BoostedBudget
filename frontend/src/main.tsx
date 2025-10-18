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

// Get Auth0 configuration from environment variables
const auth0Domain = import.meta.env.VITE_AUTH0_DOMAIN
const auth0ClientId = import.meta.env.VITE_AUTH0_CLIENT_ID
const auth0Audience = import.meta.env.VITE_AUTH0_AUDIENCE

if (!auth0Domain || !auth0ClientId) {
  console.warn('Auth0 configuration missing. Please set VITE_AUTH0_DOMAIN and VITE_AUTH0_CLIENT_ID environment variables.')
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
      domain={auth0Domain || 'placeholder.auth0.com'}
      clientId={auth0ClientId || 'placeholder-client-id'}
      authorizationParams={{
        redirect_uri: window.location.origin,
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
