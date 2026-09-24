import { useEffect, useRef, useState, Suspense, lazy } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router'
import { useAuth } from '@clerk/react-router'
import { useRevenueCatIdentity } from './features/billing/hooks/useRevenueCatCheckout'
import { Toaster } from 'sonner'
import { useCookieState } from './shared/hooks/useCookieState'
import {
  CookieConsent,
  type CookieConsentSettings,
  COOKIE_CONSENT_NAME,
  COOKIE_CONSENT_VERSION,
  type CookieConsentData
} from './privacy'
import { ProtectedRoute } from './auth'
import { PremiumRoute } from './auth/PremiumRoute'
// #1601: AppFooter is imported CONCRETELY and Header is lazy, and the split matters.
//
// Both live in src/components/layout/, which vite.config.ts's `shared` group sweeps into the
// eagerly-imported `shared` chunk. Header's own imports (@/features/notifications and
// @/features/transactions/hooks/useReviewSummary) were therefore static edges out of `shared`
// into the feature chunks, and transactions pulls accounts, which pulls
// investments/net-worth/household/profile/admin, which pulls dashboard, which pulls echarts.
// That single edge is most of the 847 KiB the landing route used to download — for a component
// `!isLandingPage` means it does not even RENDER on `/`.
//
// It must be BOTH lazy here AND excluded from the `shared` group (see vite.config.ts's
// `layout-chrome` group): groups assign chunks by path glob, so lazy() alone would leave the
// module sitting in an eager chunk with its static imports intact.
//
// AppFooter stays eager on purpose — it renders on the landing page, so making it lazy would
// buy nothing and cost a paint. Importing it via the barrel would re-create the Header edge,
// because the barrel re-exports Header.
import { AppFooter } from './components/layout/AppFooter'
// #1601: both banners render only under `isAuthenticated && !isLandingPage`, so on / they were
// pure eager weight — and HouseholdOverCapacityBanner's own imports (useProfile, useHousehold)
// were static edges from the landing entry into the profile and household chunks.
//
// SubscriptionManagementSkeleton and SubscriptionErrorState stay EAGER on purpose: they are a
// Suspense fallback and an error-boundary fallback respectively, which have to be available
// synchronously at the moment the thing they stand in for is still loading or has failed. A
// lazy fallback needs a fallback of its own. They are why the billing chunk remains on /'s
// eager set; that is a deliberate 11 KiB, not an oversight.
const SubscriptionStatusBanner = lazy(() =>
  import('./features/billing/components/SubscriptionStatusBanner').then((m) => ({
    default: m.SubscriptionStatusBanner,
  }))
)
const HouseholdOverCapacityBanner = lazy(() =>
  import('./features/household/components/HouseholdOverCapacityBanner').then((m) => ({
    default: m.HouseholdOverCapacityBanner,
  }))
)
import { SubscriptionManagementSkeleton } from './features/billing/components/SubscriptionManagementSkeleton'
import { SubscriptionErrorState } from './features/billing/components/SubscriptionErrorState'
import { LandingPage } from './features/landing'
// #1601: concrete modules, NOT the './features/admin' barrel. That barrel re-exports every
// admin page (AdminDashboardPage, UsersPage, HealthDashboardPage, WebhooksPage,
// DeadLetterPage, PlaidOrphanedItemsPage), so importing two guards from it pulled the whole
// admin chunk onto the LANDING page's eager critical path even though every admin route
// below is React.lazy(). Verified with scripts/check-route-weight.sh.
import { AdminRoute } from './features/admin/components/AdminRoute'
import { AdminLayout } from './features/admin/components/AdminLayout'
import { NotFoundPage } from './features/not-found/NotFoundPage'

// #1601 — see the AppFooter import note above for why this is lazy.
const Header = lazy(() =>
  import('./components/layout/Header').then((m) => ({ default: m.Header }))
)

// #1650 — lazy for the same reason as Header, plus one more: this component's
// static import graph reaches posthog-js through shared/posthog/engagement, and
// the SDK is consent-gated and not needed for first paint. It renders null for
// anonymous users, so gating the render on isAuthenticated below is
// behavior-preserving while keeping the SDK off the landing route.
const AuthenticatedPostHogExperiences = lazy(() =>
  import('./shared/posthog/AuthenticatedPostHogExperiences').then((m) => ({
    default: m.AuthenticatedPostHogExperiences,
  }))
)

// Reserves the header's exact box so the lazy boundary cannot cause layout shift: Header's root
// is `sticky top-0 z-50 ... border-b border-border ... h-16`, and this mirrors it. Deliberately
// empty rather than a shimmer — the chrome is not content, and a pulsing bar reads as an error.
// aria-hidden so a screen reader is not told about a banner that has no navigation in it yet.
// Exported ONLY so the no-layout-shift contract can be asserted directly. RTL's render() wraps
// in act(), which flushes the microtask that resolves the lazy chunk, so the suspended frame is
// already gone by the time a test can query the DOM for it — the placeholder is real in a browser
// but unobservable from a unit test. Comparing it to Header's own geometry is the only
// deterministic way to pin the contract. See App.test.tsx.
export function HeaderPlaceholder() {
  return (
    <div
      data-testid="header-placeholder"
      aria-hidden="true"
      className="sticky top-0 z-50 h-16 border-b border-border bg-background/80 backdrop-blur-sm"
    />
  )
}
import { ErrorBoundary } from './components/ErrorBoundary'
import { useTheme } from './shared/theme'
import {
  grantProductAnalytics,
  withdrawProductAnalytics,
} from './shared/lib/posthog'
import { devLog } from './shared/lib/devLog'
import { usePostHogIdentify } from './shared/hooks/usePostHogIdentify'
import {
  isPostHogEngagementRoute,
  POSTHOG_ENGAGEMENT_ROUTES,
} from './shared/posthog/routes'
import {
  useConsentAwarePageViews,
  type PageViewDependencies,
} from './shared/hooks/useConsentAwarePageViews'
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
const FeaturesPage = lazy(() =>
  import('./features/features').then(m => ({ default: m.FeaturesPage }))
)
const FaqPage = lazy(() =>
  import('./features/faq').then(m => ({ default: m.FaqPage }))
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
const ReconnectPage = lazy(() =>
  import('./features/accounts/ReconnectPage').then(m => ({ default: m.ReconnectPage }))
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
const RevenueCatPage = lazy(() => import('./features/admin/components/RevenueCatPage').then(m => ({ default: m.RevenueCatPage })))
const WebhooksPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.WebhooksPage }))
)
const PlaidOrphanedItemsPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.PlaidOrphanedItemsPage }))
)
const DeadLetterPage = lazy(() =>
  import('./features/admin').then(m => ({ default: m.DeadLetterPage }))
)

interface AppProps {
  analyticsPageViews?: PageViewDependencies
}

function App({ analyticsPageViews }: AppProps = {}) {
  const { isSignedIn: isAuthenticated } = useAuth()
  const location = useLocation()
  const { resolvedTheme } = useTheme()
  const [consentCookie] = useCookieState<CookieConsentData>(COOKIE_CONSENT_NAME)
  const lastScrolledLocationKeyRef = useRef<string | null>(null)

  const [anonymousAnalyticsConsent, setAnonymousAnalyticsConsent] = useState<boolean | null>(() => (
    consentCookie?.version === COOKIE_CONSENT_VERSION
      ? consentCookie.consents.analytics
      : null
  ))
  usePostHogIdentify(anonymousAnalyticsConsent)
  useConsentAwarePageViews(
    `${location.pathname}${location.search}${location.hash}`,
    analyticsPageViews,
  )

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

  const isLandingPage = location.pathname === '/' && !isAuthenticated

  useRevenueCatIdentity()

  const handleConsentChange = (
    consents: CookieConsentSettings,
    source: 'stored' | 'explicit',
  ) => {
    devLog('Cookie consents updated:', consents)
    if (isAuthenticated) return
    setAnonymousAnalyticsConsent(consents.analytics)
    if (source === 'stored') return
    if (consents.analytics) grantProductAnalytics('anonymous')
    else withdrawProductAnalytics('anonymous')
  }

  return (
    <div className={`w-full${isLandingPage ? ' landing-page' : ''}`}>
      {!isLandingPage && (
        <Suspense fallback={<HeaderPlaceholder />}>
          <Header />
        </Suspense>
      )}
      {/* null fallbacks, deliberately: unlike the header these banners are conditional strips
          that frequently render nothing at all (dismissed, or under capacity), so reserving a
          box would CREATE the layout shift the header placeholder exists to prevent. */}
      {isAuthenticated && !isLandingPage && (
        <Suspense fallback={null}>
          <SubscriptionStatusBanner />
        </Suspense>
      )}
      {isAuthenticated && !isLandingPage && (
        <Suspense fallback={null}>
          <HouseholdOverCapacityBanner />
        </Suspense>
      )}

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
              path={POSTHOG_ENGAGEMENT_ROUTES.dashboard}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.accounts}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.reconnectAccount}
              element={
                <ProtectedRoute>
                  <PremiumRoute>
                    <Suspense fallback={<PageSkeleton />}>
                      <ReconnectPage />
                    </Suspense>
                  </PremiumRoute>
                </ProtectedRoute>
              }
            />
            <Route
              path={POSTHOG_ENGAGEMENT_ROUTES.account}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.transactions}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.review}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.insights}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.planning}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.budget}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.autoCategorization}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.boosts}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.newBoost}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.boost}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.editBoost}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.profile}
              element={
                <ProtectedRoute>
                  <Suspense fallback={<PageSkeleton />}>
                    <ProfilePage />
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
            <Route path="/features" element={<Suspense fallback={<PageSkeleton />}><FeaturesPage /></Suspense>} />
            <Route path="/faq" element={<Suspense fallback={<PageSkeleton />}><FaqPage /></Suspense>} />
            <Route
              path={POSTHOG_ENGAGEMENT_ROUTES.billingManage}
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
              path={POSTHOG_ENGAGEMENT_ROUTES.billingSuccess}
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
              <Route path="revenuecat" element={<Suspense fallback={<PageSkeleton />}><RevenueCatPage /></Suspense>} />
              <Route path="webhooks" element={<Suspense fallback={<PageSkeleton />}><WebhooksPage /></Suspense>} />
              <Route path="dead-letter" element={<Suspense fallback={<PageSkeleton />}><DeadLetterPage /></Suspense>} />
              {/* Issue #1497 — path matches ADMIN_ROUTES.PLAID_ORPHANED_ITEMS and the endpoint it reads. */}
              <Route path="plaid/orphaned-items" element={<Suspense fallback={<PageSkeleton />}><PlaidOrphanedItemsPage /></Suspense>} />
            </Route>
            {/* Belt and braces for the same bug the ClerkProvider comment in main.tsx describes.
                signInUrl/signUpUrl stop Clerk GENERATING these paths; these routes make them
                harmless if they arrive anyway — a stale bookmark, an emailed link, a Clerk flow
                we have not configured, or a future SDK default. Landing on the page that hosts
                the sign-in modal is always better than a 404 for a URL whose whole purpose is
                signing in. */}
            <Route path="/sign-in" element={<Navigate to="/" replace />} />
            <Route path="/sign-up" element={<Navigate to="/" replace />} />
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </main>
      )}

      <AppFooter />

      {!isAuthenticated && <CookieConsent onConsentChange={handleConsentChange} />}
      {isAuthenticated && (
        <Suspense fallback={null}>
          <AuthenticatedPostHogExperiences routeEligible={isPostHogEngagementRoute(location.pathname)} />
        </Suspense>
      )}
      <Toaster position="bottom-right" richColors theme={resolvedTheme} />
    </div>
  )
}

export default App
