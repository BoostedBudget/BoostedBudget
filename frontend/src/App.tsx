import { useState, useEffect } from 'react'
import { useAuth0 } from '@auth0/auth0-react'
import { CookiesProvider } from 'react-cookie'
import { CookieConsent, initializeAdSense, type CookieConsentSettings } from './privacy'
import { LoginButton, LogoutButton } from './auth'
import './App.css'

interface HealthCheck {
  status: string
  version: string
  timestamp: string
  checks: {
    database: boolean
    redis: boolean
  }
}

function App() {
  const { isAuthenticated, user } = useAuth0()
  const [health, setHealth] = useState<HealthCheck | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/health')
      .then(res => res.json())
      .then(data => {
        setHealth(data)
        setLoading(false)
      })
      .catch(err => {
        console.error('Failed to fetch health:', err)
        setLoading(false)
      })
  }, [])

  // Handle cookie consent changes
  const handleConsentChange = (consents: CookieConsentSettings) => {
    console.log('Cookie consents updated:', consents)

    // Initialize AdSense if advertising consent is given
    // and user is on free tier (TODO: check subscription when auth is implemented)
    if (consents.advertising) {
      const adsenseClientId = import.meta.env.VITE_ADSENSE_CLIENT_ID
      if (adsenseClientId) {
        initializeAdSense({
          clientId: adsenseClientId,
          testMode: import.meta.env.DEV,
        })
      } else {
        console.warn('AdSense client ID not configured')
      }
    }

    // TODO: Initialize Google Analytics if analytics consent is given
    // if (consents.analytics) {
    //   initializeAnalytics()
    // }
  }

  return (
    <CookiesProvider>
      <div className="app">
        <header className="app-header">
          <div>
            <h1>🚀 BoostedBudget</h1>
            <p>Supercharge your finances with intelligent automation</p>
          </div>
          <div className="auth-section">
            {isAuthenticated ? (
              <div className="user-profile">
                {user?.picture && (
                  <img
                    src={user.picture}
                    alt={user.name || 'User'}
                    className="user-avatar"
                  />
                )}
                <div className="user-info">
                  <p className="user-name">{user?.name}</p>
                  <p className="user-email">{user?.email}</p>
                </div>
                <LogoutButton className="btn-auth" />
              </div>
            ) : (
              <LoginButton className="btn-auth" />
            )}
          </div>
        </header>

        <main className="app-main">
          <section className="health-check">
            <h2>System Status</h2>
            {loading ? (
              <p>Loading...</p>
            ) : health ? (
              <div className="health-status">
                <p className={`status status-${health.status}`}>
                  Status: {health.status.toUpperCase()}
                </p>
                <p>Version: {health.version}</p>
                <p>Region: us-east-1 (MVP)</p>
                <div className="checks">
                  <p>Database: {health.checks.database ? '✅' : '❌'}</p>
                  <p>Redis: {health.checks.redis ? '✅' : '❌'}</p>
                </div>
              </div>
            ) : (
              <p className="error">Failed to load system status</p>
            )}
          </section>

          <section className="info">
            <h2>Phase 0: Foundation</h2>
            <p>✅ Backend API with health endpoint</p>
            <p>✅ OpenAPI specification</p>
            <p>✅ Frontend skeleton (React + TypeScript + Vite)</p>
            <p>✅ GDPR Cookie Consent</p>
            <p>✅ Auth0 integration</p>
            <p>⏳ Database & migrations (coming next)</p>
            <p>⏳ Docker development environment (coming next)</p>
          </section>
        </main>

        <footer className="app-footer">
          <p>BoostedBudget © 2025 | GDPR Compliant | Single Region MVP (us-east-1)</p>
        </footer>

        {/* Cookie Consent Banner */}
        <CookieConsent onConsentChange={handleConsentChange} />
      </div>
    </CookiesProvider>
  )
}

export default App
