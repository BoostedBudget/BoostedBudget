import { useEffect, Suspense, lazy } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@clerk/react-router'
import { Toaster } from 'sonner'
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
import { SubscriptionManagementSkeleton } from './features/billing/components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './features/billing/components/SubscriptionErrorState'
import { useUpdatePrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import type { PrivacySettings as PrivacySettingsType } from './features/profile/types'
import { LandingPage } from './features/landing'
import { AdminRoute, AdminLayout } from './features/admin'
import { NotFoundPage } from './features/not-found/NotFoundPage'
import { ErrorBoundary } from './components/ErrorBoundary'
import { posthog } from './shared/lib/posthog'
import { usePostHogIdentify } from './shared/hooks/usePostHogIdentify'
import { PageSkeleton } from './shared/components/PageSkeleton'
import './App.css'

const ProfilePage = lazy(() =>
  import('./features/profile/ProfilePage').then(m => ({ default: m.ProfilePage }))
)
const PricingPage = lazy(() =>
  import('./features/pricing/PricingPage').then(m => ({ default: m.PricingPage }))
)
const SubscriptionManagementPage = lazy(() =>
  import('./features/billing/SubscriptionManagementPage').then(m => ({ default: m.SubscriptionManagementPage }))
)
const CheckoutSuccessPage = lazy(() =>
  import('./features/billing/CheckoutSuccessPage').then(m => ({ default: m.CheckoutSuccessPage }))
)
const AccountsPage = lazy(() =>
  import('./features/accounts/AccountsPage').then(m => ({ default: m.AccountsPage }))
)
const PlaidOAuthCallback = lazy(() =>
  import('./features/accounts/PlaidOAuthCallback').then(m => ({ default: m.PlaidOAuthCallback }))
)
const TransactionsPage = lazy(() =>
  import('./features/transactions/TransactionsPage').then(m => ({ default: m.TransactionsPage }))
)
const DashboardPage = lazy(() =>
  import('./features/dashboard/components/DashboardPage').then(m => ({ default: m.DashboardPage }))
)
const BudgetsPage = lazy(() =>
  import('./features/budgets').then(m => ({ default: m.BudgetsPage }))
)
const BudgetDetailPage = lazy(() =>
  import('./features/budgets').then(m => ({ default: m.BudgetDetailPage }))
)
const AutoCategorizationPageWrapper = lazy(() =>
  import('./features/auto-categorization/components/AutoCategorizationPageWrapper').then(m => ({ default: m.AutoCategorizationPageWrapper }))
)
const RulesPage = lazy(() =>
  import('./features/rules').then(m => ({ default: m.RulesPage }))
)
const RuleBuilder = lazy(() =>
  import('./features/rules').then(m => ({ default: m.RuleBuilder }))
)
const PrivacyPolicy = lazy(() =>
  import('./legal/PrivacyPolicy').then(m => ({ default: m.PrivacyPolicy }))
)
const TermsOfService = lazy(() =>
  import('./legal/TermsOfService').then(m => ({ default: m.TermsOfService }))
)
const CookiePolicy = lazy(() =>
  import('./legal/CookiePolicy').then(m => ({ default: m.CookiePolicy }))
)
const CancelDeletionPage = lazy(() =>
  import('./features/deletion/components/CancelDeletionPage').then(m => ({ default: m.CancelDeletionPage }))
)
const AdminDashboardPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.AdminDashboardPage }))
)
const UsersPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.UsersPage }))
)
const HealthDashboardPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.HealthDashboardPage }))
)
const WebhooksPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.WebhooksPage }))
)


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
                    <Suspense fallback={<PageSkeleton />}>
                      <DashboardPage />
                    </Suspense>
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
                    <Suspense fallback={<PageSkeleton />}>
                      <PlaidOAuthCallback />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/accounts"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <AccountsPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/transactions"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <TransactionsPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/budgets"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BudgetsPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/budgets/:budgetId"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BudgetDetailPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/auto-categorization"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <AutoCategorizationPageWrapper />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <RulesPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/new"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <RuleBuilder />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/rules/:id/edit"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <RuleBuilder />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/profile"
              element={
                <ProtectedRoute>
                  <Suspense fallback={<PageSkeleton />}>
                    <ProfilePage onPrivacySettingsChange={handlePrivacySettingsChange} />
                  </Suspense>
                </ProtectedRoute>
              }
            />
            <Route path="/legal/privacy-policy" element={<Suspense fallback={<PageSkeleton />}><PrivacyPolicy /></Suspense>} />
            <Route path="/privacy" element={<Suspense fallback={<PageSkeleton />}><PrivacyPolicy /></Suspense>} />
            <Route path="/legal/terms" element={<Suspense fallback={<PageSkeleton />}><TermsOfService /></Suspense>} />
            <Route path="/legal/cookie-policy" element={<Suspense fallback={<PageSkeleton />}><CookiePolicy /></Suspense>} />
            <Route path="/pricing" element={<Suspense fallback={<PageSkeleton />}><PricingPage /></Suspense>} />
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
                  <Suspense fallback={<PageSkeleton />}>
                    <CheckoutSuccessPage />
                  </Suspense>
                </ProtectedRoute>
              }
            />
            <Route path="/cancel-deletion/:token?" element={<Suspense fallback={<PageSkeleton />}><CancelDeletionPage /></Suspense>} />

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
              <Route index element={<Suspense fallback={<PageSkeleton />}><AdminDashboardPage /></Suspense>} />
              <Route path="users" element={<Suspense fallback={<PageSkeleton />}><UsersPage /></Suspense>} />
              <Route path="health" element={<Suspense fallback={<PageSkeleton />}><HealthDashboardPage /></Suspense>} />
              <Route path="webhooks" element={<Suspense fallback={<PageSkeleton />}><WebhooksPage /></Suspense>} />
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
