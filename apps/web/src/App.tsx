import { useEffect, useRef, Suspense, lazy } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router'
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
import { SubscriptionStatusBanner } from './features/billing/components/SubscriptionStatusBanner'
import { HouseholdOverCapacityBanner } from './features/household/components/HouseholdOverCapacityBanner'
import { SubscriptionManagementSkeleton } from './features/billing/components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './features/billing/components/SubscriptionErrorState'
import { useUpdatePrivacySettings } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import type { PrivacySettings as PrivacySettingsType } from './features/profile/types'
import { LandingPage } from './features/landing'
import { AdminRoute, AdminLayout } from './features/admin'
import { NotFoundPage } from './features/not-found/NotFoundPage'
import { ErrorBoundary } from './components/ErrorBoundary'
import { useTheme } from './shared/theme'
import { posthog } from './shared/lib/posthog'
import { usePostHogIdentify } from './shared/hooks/usePostHogIdentify'
import { PageSkeleton } from './shared/components/PageSkeleton'

// Redirect that forwards any extra query params from the old route to the new one.
// e.g. /budgets?sort=name:asc → /planning?tab=budgets&sort=name:asc
function ForwardSearch({ to }: { to: string }) {
  const location = useLocation()
  const [targetPath, targetQs] = to.split('?')
  const targetParams = new URLSearchParams(targetQs ?? '')
  const sourceParams = new URLSearchParams(location.search)
  for (const [key, value] of sourceParams) {
    if (!targetParams.has(key)) targetParams.set(key, value)
  }
  const qs = targetParams.toString()
  return <Navigate to={qs ? `${targetPath}?${qs}` : targetPath} replace />
}

const ProfilePage = lazy(() =>
  import('./features/profile/ProfilePage').then(m => ({ default: m.ProfilePage }))
)
const PricingPage = lazy(() =>
  import('./features/pricing/PricingPage').then(m => ({ default: m.PricingPage }))
)
const McpPage = lazy(() =>
  import('./features/mcp').then(m => ({ default: m.McpPage }))
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
const AccountDetailPage = lazy(() =>
  import('./features/accounts/AccountDetailPage').then(m => ({ default: m.AccountDetailPage }))
)
const PlaidOAuthCallback = lazy(() =>
  import('./features/accounts/PlaidOAuthCallback').then(m => ({ default: m.PlaidOAuthCallback }))
)
const TransactionsPage = lazy(() =>
  import('./features/transactions/TransactionsPage').then(m => ({ default: m.TransactionsPage }))
)
const ReviewInboxPage = lazy(() =>
  import('./features/transactions/review/ReviewInboxPage').then(m => ({ default: m.ReviewInboxPage }))
)
const DashboardPage = lazy(() =>
  import('./features/dashboard/components/DashboardPage').then(m => ({ default: m.DashboardPage }))
)
const BudgetDetailPage = lazy(() =>
  import('./features/budgets').then(m => ({ default: m.BudgetDetailPage }))
)
const AutoCategorizationPageWrapper = lazy(() =>
  import('./features/auto-categorization/components/AutoCategorizationPageWrapper').then(m => ({ default: m.AutoCategorizationPageWrapper }))
)
const BoostsPage = lazy(() =>
  import('./features/boosts').then(m => ({ default: m.BoostsPage }))
)
const BoostDetailPage = lazy(() =>
  import('./features/boosts').then(m => ({ default: m.BoostDetailPage }))
)
const BoostBuilder = lazy(() =>
  import('./features/boosts').then(m => ({ default: m.BoostBuilder }))
)
const PlanningPage = lazy(() =>
  import('./features/planning').then(m => ({ default: m.PlanningPage }))
)
const InsightsPage = lazy(() =>
  import('./features/insights').then(m => ({ default: m.InsightsPage }))
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
const AcceptInvitePage = lazy(() =>
  import('./features/household/components/AcceptInvitePage').then(m => ({ default: m.AcceptInvitePage }))
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
const DeadLetterPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.DeadLetterPage }))
)

function App() {
  const { isSignedIn: isAuthenticated } = useAuth()
  const location = useLocation()
  const { resolvedTheme } = useTheme()
  const [, setConsentCookie] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME)
  const lastScrolledLocationKeyRef = useRef<string | null>(null)

  usePostHogIdentify()

  useEffect(() => {
    posthog.capture('$pageview')
  }, [location.pathname])

  // Issue #1411 (B1 fix): react-router's <Link> only performs client-side navigation
  // (history.pushState) — it never scrolls to a URL fragment. Without this effect,
  // hash links like AppFooter's `/#features` changed the URL but left the viewport in
  // place. This scrolls to the hash target on every location change, so it covers
  // same-page hash links, cross-page fragment links, and hard loads/refreshes — for
  // any current or future hash link, app-wide, not just the footer.
  //
  // Several routes below (e.g. /mcp) are lazy-loaded via `lazy(() => import(...))`.
  // On a cold load of a fragment like `/mcp#capabilities`, this effect can run while
  // only <PageSkeleton /> has rendered — the target isn't in the DOM yet. Rather than
  // give up, poll briefly for it to appear, bounded to a fixed number of attempts so a
  // genuinely-missing id doesn't retry forever.
  useEffect(() => {
    if (!location.hash) return

    const targetId = location.hash.slice(1)

    const scrollToTarget = (el: HTMLElement) => {
      const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
      el.scrollIntoView({ behavior: prefersReducedMotion ? 'auto' : 'smooth' })
    }

    // Returns true once the target exists (whether this call scrolled to it or a
    // previous call already did), so the retry loop below knows when to stop.
    const attemptScroll = (): boolean => {
      const target = document.getElementById(targetId)
      if (!target) return false

      // Dev-only guard: React 18 StrictMode double-invokes this effect on mount
      // (mount -> cleanup -> mount again) with the same `location.key`. Skip the
      // repeat call so scrollIntoView doesn't fire twice for one real navigation.
      // `location.key` (not pathname+hash) is what changes on every *real*
      // navigation — including a later revisit to the same hash — so this never
      // suppresses a legitimate scroll, only the StrictMode artifact.
      if (lastScrolledLocationKeyRef.current !== location.key) {
        lastScrolledLocationKeyRef.current = location.key
        scrollToTarget(target)
      }
      return true
    }

    if (attemptScroll()) return

    let attempts = 0
    const MAX_ATTEMPTS = 20 // ~2s at 100ms intervals — generous for a lazy chunk to resolve
    const intervalId = window.setInterval(() => {
      attempts += 1
      if (attemptScroll() || attempts >= MAX_ATTEMPTS) {
        window.clearInterval(intervalId)
      }
    }, 100)

    return () => window.clearInterval(intervalId)
  }, [location.pathname, location.hash, location.key])

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
    <div className={`w-full${isLandingPage ? ' landing-page' : ''}`}>
      {!isLandingPage && <Header />}
      {isAuthenticated && !isLandingPage && <SubscriptionStatusBanner />}
      {isAuthenticated && !isLandingPage && <HouseholdOverCapacityBanner />}

      {isLandingPage ? (
        <Routes>
          <Route path="/" element={<LandingPage />} />
        </Routes>
      ) : (
        <main className="flex flex-col gap-8 mb-12 pt-4">
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
              path="/accounts/:accountId"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <AccountDetailPage />
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
              path="/review"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <ReviewInboxPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            {/* Issue #1300: /reports and /net-worth redirect to /insights?tab= (query params forwarded) */}
            <Route path="/reports" element={<ForwardSearch to="/insights?tab=spending" />} />
            <Route path="/net-worth" element={<ForwardSearch to="/insights?tab=net-worth" />} />
            <Route
              path="/insights"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <InsightsPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            {/* Issue #1300: /savings and /budgets redirect to /planning?tab= (query params forwarded) */}
            <Route path="/savings" element={<ForwardSearch to="/planning?tab=savings" />} />
            <Route path="/budgets" element={<ForwardSearch to="/planning?tab=budgets" />} />
            <Route
              path="/planning"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <PlanningPage />
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
              path="/boosts"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BoostsPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/boosts/new"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BoostBuilder />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/boosts/:id"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BoostDetailPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path="/boosts/:id/edit"
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <BoostBuilder />
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
            <Route path="/mcp" element={<Suspense fallback={<PageSkeleton />}><McpPage /></Suspense>} />
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
            {/* Real link generated by household_service.go's invite_link (frontendURL + "/households/invites/" + token) */}
            <Route path="/households/invites/:token" element={<Suspense fallback={<PageSkeleton />}><AcceptInvitePage /></Suspense>} />
            {/* Alias matching the original issue text — both routes render the same page */}
            <Route path="/household/invite/:token" element={<Suspense fallback={<PageSkeleton />}><AcceptInvitePage /></Suspense>} />

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
              <Route path="dead-letter" element={<Suspense fallback={<PageSkeleton />}><DeadLetterPage /></Suspense>} />
            </Route>
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </main>
      )}

      <AppFooter />

      <CookieConsent onConsentChange={handleConsentChange} />
      <Toaster position="bottom-right" richColors theme={resolvedTheme} />
    </div>
  )
}

export default App
