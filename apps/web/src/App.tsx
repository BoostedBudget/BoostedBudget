import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@clerk/react-router'
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
import { ProtectedRoute } from './auth'
import { Header } from './components/layout'
import { ProfilePage } from './features/profile/ProfilePage'
import { PricingPage } from './features/pricing/PricingPage'
import { AccountsPage } from './features/accounts/AccountsPage'
import { PlaidOAuthCallback } from './features/accounts/PlaidOAuthCallback'
import { TransactionsPage } from './features/transactions/TransactionsPage'
import { DashboardPage } from './features/dashboard/components/DashboardPage'
import { useUpdatePrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import type { PrivacySettings as PrivacySettingsType } from './features/profile/types'
import { PrivacyPolicy } from './legal/PrivacyPolicy'
import { CancelDeletionPage } from './features/deletion/components/CancelDeletionPage'
import { AutoCategorizationPageWrapper } from './features/auto-categorization/components/AutoCategorizationPageWrapper'
import { BudgetsPage, BudgetDetailPage } from './features/budgets'
import { RulesPage, RuleBuilder } from './features/rules'
import { LandingPage } from './features/landing'
import { Toaster } from 'sonner'
import './App.css'

function App() {
  const { isSignedIn: isAuthenticated } = useAuth()
  const location = useLocation()
  const [, setConsentCookie] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME)

  // Use generated mutation hook for privacy settings
  const updatePrivacyMutation = useUpdatePrivacySettings()

  // Check if we're on the landing page (unauthenticated home)
  const isLandingPage = location.pathname === '/' && !isAuthenticated

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
        await updatePrivacyMutation.mutateAsync({ data: privacyUpdate })
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
    <div className={`app ${isLandingPage ? 'app--landing' : ''}`}>
        {/* Hide header on landing page - it has its own navigation */}
        {!isLandingPage && <Header />}

        {isLandingPage ? (
          <Routes>
            <Route path="/" element={<LandingPage />} />
          </Routes>
        ) : (
        <main className="app-main">
          <Routes>
            <Route
              path="/"
              element={<Navigate to="/dashboard" replace />}
            />
            <Route
              path="/dashboard"
              element={
                <ProtectedRoute>
                  <DashboardPage />
                </ProtectedRoute>
              }
            />
            <Route path="/callback" element={<Navigate to="/dashboard" replace />} />
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
              path="/budgets"
              element={
                <ProtectedRoute>
                  <BudgetsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/budgets/:budgetId"
              element={
                <ProtectedRoute>
                  <BudgetDetailPage />
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
            {/* Issue #39: Automation Rules */}
            <Route
              path="/rules"
              element={
                <ProtectedRoute>
                  <RulesPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/new"
              element={
                <ProtectedRoute>
                  <RuleBuilder />
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/:id/edit"
              element={
                <ProtectedRoute>
                  <RuleBuilder />
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
            <Route path="/pricing" element={<PricingPage />} />
            {/* Public route - JWT token validation handled by backend API */}
            <Route path="/cancel-deletion/:token?" element={<CancelDeletionPage />} />
          </Routes>
        </main>
        )}

        {/* Hide footer on landing page - it has its own footer */}
        {!isLandingPage && (
          <footer className="app-footer">
            <p>BoostedBudget © 2025 | GDPR Compliant | Single Region MVP (us-east-1)</p>
          </footer>
        )}

        {/* Cookie Consent Banner */}
        <CookieConsent onConsentChange={handleConsentChange} />

        {/* Toast Notifications - Issue #521 */}
        <Toaster position="bottom-right" richColors />
      </div>
  )
}

export default App
