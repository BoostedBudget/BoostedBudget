import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@clerk/react-router'
import { useCookieState } from './shared/hooks/useCookieState'
import {
  CookieConsent,
  type CookieConsentSettings,
  cookieToPrivacySettings,
  privacySettingsToCookie,
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  type CookieConsentData
} from './privacy'
import { ProtectedRoute } from './auth'
import { PremiumRoute } from './auth/PremiumRoute'
import { Header, AppFooter } from './components/layout'
import { ProfilePage } from './features/profile/ProfilePage'
import { PricingPage } from './features/pricing/PricingPage'
import { SubscriptionManagementPage } from './features/billing/SubscriptionManagementPage'
import { CheckoutSuccessPage } from './features/billing/CheckoutSuccessPage'
import { SubscriptionManagementSkeleton } from './features/billing/components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './features/billing/components/SubscriptionErrorState'
import { AccountsPage } from './features/accounts/AccountsPage'
import { PlaidOAuthCallback } from './features/accounts/PlaidOAuthCallback'
import { TransactionsPage } from './features/transactions/TransactionsPage'
import { DashboardPage } from './features/dashboard/components/DashboardPage'
import { useUpdatePrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import type { PrivacySettings as PrivacySettingsType } from './features/profile/types'
import { PrivacyPolicy } from './legal/PrivacyPolicy'
import { TermsOfService } from './legal/TermsOfService'
import { CookiePolicy } from './legal/CookiePolicy'
import { CancelDeletionPage } from './features/deletion/components/CancelDeletionPage'
import { AutoCategorizationPageWrapper } from './features/auto-categorization/components/AutoCategorizationPageWrapper'
import { BudgetsPage, BudgetDetailPage } from './features/budgets'
import { RulesPage, RuleBuilder } from './features/rules'
import { LandingPage } from './features/landing'
import { AdminRoute, AdminLayout, AdminDashboardPage, UsersPage, HealthDashboardPage, WebhooksPage } from './features/admin'
import { NotFoundPage } from './features/not-found/NotFoundPage'
import { Toaster } from 'sonner'
import { ErrorBoundary } from './components/ErrorBoundary'
import { useEffect, Suspense } from 'react'
import { posthog } from './shared/lib/posthog'
import { usePostHogIdentify } from './shared/hooks/usePostHogIdentify'
import './App.css'


function App() {
  const { isSignedIn: isAuthenticated } = useAuth()
  const location = useLocation()
  const [, setConsentCookie] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME)

  usePostHogIdentify()

  useEffect(() => {
    posthog.capture('$pageview')
  }, [location.pathname])

  // Use generated mutation hook for privacy settings
  const updatePrivacyMutation = useUpdatePrivacySettings()

  const isLandingPage = location.pathname === '/' && !isAuthenticated

  // Initialize Paddle.js SDK
  useEffect(() => {
    const paddleToken = import.meta.env.VITE_PADDLE_CLIENT_TOKEN
    const paddleEnv = import.meta.env.VITE_PADDLE_ENV || 'sandbox'

    if (!paddleToken) {
      console.warn('Paddle client token not configured (VITE_PADDLE_CLIENT_TOKEN)')
      return
    }

    if (!window.Paddle) {
      console.warn('Paddle.js SDK not loaded')
      return
    }

    try {
      window.Paddle.Environment.set(paddleEnv === 'production' ? 'production' : 'sandbox')
      window.Paddle.Initialize({
        token: paddleToken,
        eventCallback: (event) => {
          console.log('Paddle event:', event)

          // Dispatch custom events for checkout lifecycle
          if (event.name === 'checkout.completed') {
            window.dispatchEvent(new CustomEvent('paddle:checkout.completed', { detail: event }))
          } else if (event.name === 'checkout.closed') {
            window.dispatchEvent(new CustomEvent('paddle:checkout.closed', { detail: event }))
          }
        },
      })
      console.log('Paddle initialized successfully', { environment: paddleEnv })
    } catch (error) {
      console.error('Failed to initialize Paddle:', error)
    }
  }, [])

  const updateConsentCookie = (consents: CookieConsentSettings) => {
    const expires = new Date()
    expires.setDate(expires.getDate() + 365) // 1 year

    const consentData: CookieConsentData = {
      consents,
      timestamp: new Date().toISOString(),
      version: COOKIE_CONSENT_VERSION,
    }

    setConsentCookie(consentData, {
      path: '/',
      expires,
      sameSite: 'strict',
      secure: window.location.protocol === 'https:',
    })
  }

  const handleConsentChange = async (consents: CookieConsentSettings) => {
    console.log('Cookie consents updated:', consents)

    if (consents.analytics) {
      posthog.opt_in_capturing()
    } else {
      posthog.opt_out_capturing()
    }

    if (isAuthenticated) {
      try {
        const privacyUpdate = cookieToPrivacySettings(consents)
        await updatePrivacyMutation.mutateAsync({ data: privacyUpdate })
        console.log('Cookie consent synced to backend')
      } catch (error) {
        console.error('Failed to sync cookie consent to backend:', error)
      }
    }
  }

  const handlePrivacySettingsChange = (settings: PrivacySettingsType) => {
    console.log('Privacy settings updated:', settings)
    const cookieSettings = privacySettingsToCookie(settings)
    updateConsentCookie(cookieSettings)
    console.log('Privacy settings synced to cookie')
  }

  return (
    <div className={`app ${isLandingPage ? 'app--landing' : ''}`}>
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
                  <PremiumRoute>
                    <DashboardPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route path="/callback" element={<Navigate to="/dashboard" replace />} />
            <Route
              path="/oauth-callback"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <PlaidOAuthCallback />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/accounts"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <AccountsPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/transactions"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <TransactionsPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/budgets"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <BudgetsPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/budgets/:budgetId"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <BudgetDetailPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/auto-categorization"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <AutoCategorizationPageWrapper />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <RulesPage />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/new"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <RuleBuilder />
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/:id/edit"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <RuleBuilder />
                  </PremiumRoute>
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
            <Route path="/legal/terms" element={<TermsOfService />} />
            <Route path="/legal/cookie-policy" element={<CookiePolicy />} />
            <Route path="/pricing" element={<PricingPage />} />
            <Route
              path="/billing/manage"
              element={
                <ProtectedRoute>
                  <ErrorBoundary
                    fallback={
                      <SubscriptionErrorState
                        message="Something went wrong. Please try refreshing the page."
                        onRetry={() => window.location.reload()}
                      />
                    }
                  >
                    <Suspense fallback={<SubscriptionManagementSkeleton />}>
                      <SubscriptionManagementPage />
                    </Suspense>
                  </ErrorBoundary>
                </ProtectedRoute>
              }
            />
            <Route
              path="/billing/success"
              element={
                <ProtectedRoute>
                  <CheckoutSuccessPage />
                </ProtectedRoute>
              }
            />
            <Route path="/cancel-deletion/:token?" element={<CancelDeletionPage />} />

            <Route
              path="/admin"
              element={
                <ProtectedRoute>
                  <AdminRoute>
                    <AdminLayout />
                  </AdminRoute>
                </ProtectedRoute>
              }
            >
              <Route index element={<AdminDashboardPage />} />
              <Route path="users" element={<UsersPage />} />
              <Route path="health" element={<HealthDashboardPage />} />
              <Route path="webhooks" element={<WebhooksPage />} />
            </Route>
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </main>
      )}

      {!isLandingPage && <AppFooter />}

      <CookieConsent onConsentChange={handleConsentChange} />
      <Toaster position="bottom-right" richColors />
    </div>
  )
}

export default App
