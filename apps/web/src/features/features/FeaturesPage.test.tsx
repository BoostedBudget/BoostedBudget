/**
 * FeaturesPage Component Tests (Issue #1666)
 *
 * The public /features page. Created because the landing page's `/#features`
 * anchor is unreachable for a signed-in user — App.tsx computes
 * `isLandingPage = pathname === '/' && !isAuthenticated`, so `/` redirects to
 * /dashboard and the hash dies. This page is the real destination.
 *
 * Mirrors the McpPage test pattern: exactly one h1, a real link target, and a
 * `trackEvent` assertion for the mount-time view event plus the CTA click.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { ThemeProvider } from '@/shared/theme'
import App from '@/App'
import {
  createMockClerk,
  createMockClerkAuth,
  createMockClerkUser,
  mockClerkUser,
  mockUseAuth,
  mockUseClerk,
  mockUseUser,
} from '@/test/mocks/clerk'
import { mockUseGetPrivacySettings, mockUseUpdatePrivacySettings } from '@/test/mocks/privacy-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS, isValidAnalyticsEventPayload } from '@/shared/analytics'
import { MARKETING_FEATURES } from '@/features/landing/data/marketingContent'
import { FeaturesPage } from './FeaturesPage'

function renderPage() {
  return render(
    <MemoryRouter>
      <FeaturesPage />
    </MemoryRouter>,
  )
}

describe('FeaturesPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUseClerk.mockClear()
    mockUseClerk.mockReturnValue(createMockClerk())
    mockUseAuth.mockClear()
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
  })

  it('renders exactly one h1', () => {
    renderPage()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('renders the non-agentic feature titles and descriptions from the shared source', () => {
    renderPage()
    for (const feature of MARKETING_FEATURES.filter((entry) => entry.key !== 'agent')) {
      expect(screen.getByRole('heading', { level: 3, name: feature.title })).toBeInTheDocument()
      expect(screen.getByText(feature.description)).toBeInTheDocument()
    }
  })

  it('links to /mcp', () => {
    renderPage()
    const mcpLinks = screen.getAllByRole('link').filter((link) => link.getAttribute('href') === '/mcp')
    expect(mcpLinks.length).toBeGreaterThan(0)
  })

  it('fires features_page_viewed exactly once on mount', () => {
    renderPage()
    const viewedCalls = mockTrackEvent.mock.calls.filter(
      (call) => (call as unknown[])[0] === ANALYTICS_EVENTS.FEATURES_PAGE_VIEWED,
    )
    expect(viewedCalls).toHaveLength(1)
  })

  it('fires features_cta_clicked when the primary sign-up CTA is clicked', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /start free/i }))
    const ctaCalls = mockTrackEvent.mock.calls.filter(
      (call) => (call as unknown[])[0] === ANALYTICS_EVENTS.FEATURES_CTA_CLICKED,
    )
    expect(ctaCalls).toHaveLength(1)
  })

  it('opens Clerk sign-up when the primary CTA is clicked', () => {
    const clerkMock = createMockClerk()
    mockUseClerk.mockReturnValue(clerkMock)

    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /start free/i }))
    expect(clerkMock.openSignUp).toHaveBeenCalledTimes(1)
  })

  it('sends a signed-in visitor to /dashboard rather than opening sign-up', () => {
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
    const clerkMock = createMockClerk()
    mockUseClerk.mockReturnValue(clerkMock)

    render(
      <MemoryRouter>
        <FeaturesPage />
        <LocationProbe />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: /start free/i }))

    expect(screen.getByTestId('location-probe')).toHaveTextContent('/dashboard')
    expect(clerkMock.openSignUp).not.toHaveBeenCalled()
    // The event fires in BOTH branches — only the destination differs.
    const ctaCalls = mockTrackEvent.mock.calls.filter(
      (call) => (call as unknown[])[0] === ANALYTICS_EVENTS.FEATURES_CTA_CLICKED,
    )
    expect(ctaCalls).toHaveLength(1)
  })

  it('exposes both events as valid catalogued zero-property analytics contracts', () => {
    // Guards against a silently-dropped event: the catalog validator must accept the
    // empty payload the page actually sends.
    expect(isValidAnalyticsEventPayload(ANALYTICS_EVENTS.FEATURES_PAGE_VIEWED, {})).toBe(true)
    expect(isValidAnalyticsEventPayload(ANALYTICS_EVENTS.FEATURES_CTA_CLICKED, {})).toBe(true)
  })
})

/**
 * The defect this page exists to fix, asserted at the routing level for a
 * SIGNED-IN user. Reproduces the plan's "QA — failure": the landing anchor is
 * unreachable signed-in because App.tsx sends `/` to /dashboard, so `/features`
 * must resolve to the real page rather than fall through to that redirect.
 *
 * Rendered against the real `App` with Clerk mocked signed-in. (A browser-level
 * run of the same check is impossible in a worktree without Clerk credentials —
 * `AuthReadyGate` gates every route on Clerk loading, so the whole app is stuck
 * on `auth-loading` there. This is the strongest in-env equivalent.)
 */
function LocationProbe() {
  return <div data-testid="location-probe">{useLocation().pathname}</div>
}

function appWrapper(initialEntries: string[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return ({ children }: { children: ReactNode }) => (
    <ThemeProvider defaultTheme="system">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>
  )
}

describe('FeaturesPage routing for a signed-in user (#1666 defect)', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUseAuth.mockClear()
    mockUseUser.mockClear()
    mockUseClerk.mockClear()

    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
    mockUseUser.mockReturnValue(
      createMockClerkUser({ isSignedIn: true, user: { ...mockClerkUser, id: 'user_features' } }),
    )
    mockUseClerk.mockReturnValue(createMockClerk())
    mockUseGetPrivacySettings.mockReturnValue({
      data: { gdpr_analytics_consent: true },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as never)
    mockUseUpdatePrivacySettings.mockReturnValue({
      mutate: mock(() => {}),
      mutateAsync: mock(() => Promise.resolve()),
      isPending: false,
      isError: false,
      isSuccess: false,
      error: null,
      data: null,
      reset: mock(() => {}),
    } as never)
  })

  it('renders /features and does not redirect the signed-in user to /dashboard', async () => {
    render(
      <>
        <App />
        <LocationProbe />
      </>,
      { wrapper: appWrapper(['/features']) },
    )

    await waitFor(
      () => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
          /everything you need to run your finances/i,
        )
      },
      { timeout: 10000 },
    )

    // The regression guard: the URL stays on /features, so the `/` → /dashboard
    // Navigate never fires for the page the footer links to.
    expect(screen.getByTestId('location-probe')).toHaveTextContent('/features')
  })
})
