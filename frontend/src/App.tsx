import { Routes, Route, Link, Navigate } from 'react-router-dom'
import { useAuth0 } from '@auth0/auth0-react'
import { useCookieState } from './shared/hooks/useCookieState'
import {
  CookieConsent,
  initializeAdSense,
  type CookieConsentSettings,
  cookieToPrivacySettings,
  privacySettingsToCookie,
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  COOKIE_CONSENT_EXPIRES_DAYS,
  type CookieConsentData
} from './privacy'
import { LoginButton, LogoutButton, ProtectedRoute } from './auth'
import { ProfilePage } from './features/profile/ProfilePage'
import { AccountsPage } from './features/accounts/AccountsPage'
import { PlaidOAuthCallback } from './features/accounts/PlaidOAuthCallback'
import { TransactionsPage } from './features/transactions/TransactionsPage'
import { DashboardPage } from './features/dashboard/components/DashboardPage'
import { updatePrivacySettings } from './features/profile/api/privacyApi'
import type { PrivacySettings as PrivacySettingsType } from './features/profile/types'
import { PrivacyPolicy } from './legal/PrivacyPolicy'
import { CancelDeletionPage } from './features/deletion/components/CancelDeletionPage'
import { AutoCategorizationPageWrapper } from './features/auto-categorization/components/AutoCategorizationPageWrapper'
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

// Home page content component
function HomePage({ health, loading }: { health: HealthCheck | null; loading: boolean }) {
  return (
    <>
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
        <p>✅ User Profile Management</p>
        <p>⏳ Database & migrations (coming next)</p>
        <p>⏳ Docker development environment (coming next)</p>
      </section>
    </>
  )
}

function App() {
  const { isAuthenticated, user } = useAuth0()
  const [, setConsentCookie] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME)

  // Health check removed - was calling frontend /health which returns text/plain
  // Backend health is checked by workflow smoke tests
  const health = null
  const loading = false

  // Helper function to update cookie consent data
  const updateConsentCookie = (consents: CookieConsentSettings) => {
    const consentData: CookieConsentData = {
      consents,
      timestamp: new Date().toISOString(),
      version: COOKIE_CONSENT_VERSION,
    }

    const expires = new Date()
    expires.setDate(expires.getDate() + COOKIE_CONSENT_EXPIRES_DAYS)

    setConsentCookie(consentData, {
      path: '/',
      expires,
      sameSite: 'strict',
      secure: window.location.protocol === 'https:',
    })
  }

  // Handle cookie consent changes (Cookie → Backend sync)
  const handleConsentChange = async (consents: CookieConsentSettings) => {
    console.log('Cookie consents updated:', consents)

    // Sync cookie consent to backend if user is authenticated
    if (isAuthenticated) {
      try {
        const privacyUpdate = cookieToPrivacySettings(consents)
        await updatePrivacySettings(privacyUpdate)
        console.log('Cookie consent synced to backend')
      } catch (error) {
        console.error('Failed to sync cookie consent to backend:', error)
        // Non-blocking error - cookie is still saved locally
      }
    }

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

  // Handle privacy settings changes (Backend → Cookie sync)
  const handlePrivacySettingsChange = (settings: PrivacySettingsType) => {
    console.log('Privacy settings updated:', settings)

    // Convert backend privacy settings to cookie format
    const cookieSettings = privacySettingsToCookie(settings)

    // Update cookie to match backend settings
    updateConsentCookie(cookieSettings)

    console.log('Privacy settings synced to cookie')
  }

  return (
    <div className="app">
        <header className="app-header">
          <div>
            <h1>🚀 BoostedBudget</h1>
            <p>Supercharge your finances with intelligent automation</p>
          </div>
          <nav className="main-nav">
            {isAuthenticated ? (
              <>
                <Link to="/dashboard">Dashboard</Link>
                <Link to="/accounts">Accounts</Link>
                <Link to="/transactions">Transactions</Link>
                <Link to="/auto-categorization">Auto-Cat</Link>
                <Link to="/profile">Profile</Link>
              </>
            ) : (
              <Link to="/">Home</Link>
            )}
          </nav>
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
          <Routes>
            <Route
              path="/"
              element={
                isAuthenticated ? (
                  <Navigate to="/dashboard" replace />
                ) : (
                  <HomePage health={health} loading={loading} />
                )
              }
            />
            <Route
              path="/dashboard"
              element={
                <ProtectedRoute>
                  <DashboardPage />
                </ProtectedRoute>
              }
            />
            <Route path="/callback" element={<HomePage health={health} loading={loading} />} />
            <Route
              path="/oauth-callback"
              element={
                <ProtectedRoute>
                  <PlaidOAuthCallback />
                </ProtectedRoute>
              }
            />
            <Route
              path="/accounts"
              element={
                <ProtectedRoute>
                  <AccountsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/transactions"
              element={
                <ProtectedRoute>
                  <TransactionsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/auto-categorization"
              element={
                <ProtectedRoute>
                  <AutoCategorizationPageWrapper />
                </ProtectedRoute>
              }
            />
            <Route
              path="/profile"
              element={
                <ProtectedRoute>
                  <ProfilePage onPrivacySettingsChange={handlePrivacySettingsChange} />
                </ProtectedRoute>
              }
            />
            <Route path="/legal/privacy-policy" element={<PrivacyPolicy />} />
            <Route path="/privacy" element={<PrivacyPolicy />} />
            {/* Public route - JWT token validation handled by backend API */}
            <Route path="/cancel-deletion/:token?" element={<CancelDeletionPage />} />
          </Routes>
        </main>

        <footer className="app-footer">
          <p>BoostedBudget © 2025 | GDPR Compliant | Single Region MVP (us-east-1)</p>
        </footer>

        {/* Cookie Consent Banner */}
        <CookieConsent onConsentChange={handleConsentChange} />
      </div>
  )
}

export default App
