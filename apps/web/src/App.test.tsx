/**
 * App Component Tests
 * Issue #575: Migrated to bun:test with centralized mocks
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter, MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import App, { HeaderPlaceholder } from './App'
import { ThemeProvider } from './shared/theme'
import Cookies from 'js-cookie'
import { COOKIE_CONSENT_NAME } from './privacy'
import {
  getAnalyticsLifecycleSnapshot,
  getAnalyticsLifecycleState,
  resetAnalyticsLifecycleForTests,
  subscribeToAnalyticsLifecycle,
} from './shared/lib/posthog'
import { mockPosthogCapture } from '@/test/mocks/posthog'

import {
  mockUseAuth,
  mockUseUser,
  mockUseClerk,
  mockClerkUser,
  createMockClerkAuth,
  createMockClerkUser,
  createMockClerk,
} from '@/test/mocks/clerk'
import { mockUseGetPrivacySettings, mockUseUpdatePrivacySettings } from '@/test/mocks/privacy-api'

const mockFetch = mock(() =>
  Promise.resolve({
    json: () =>
      Promise.resolve({
        status: 'healthy',
        version: '1.0.0',
        timestamp: '2025-10-18T00:00:00Z',
        checks: { database: true, redis: true },
      }),
  })
)
globalThis.fetch = mockFetch as unknown as typeof fetch

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })

const createWrapper = () => {
  const queryClient = createTestQueryClient()
  return ({ children }: { children: ReactNode }) => (
    <ThemeProvider defaultTheme="system">
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    </ThemeProvider>
  )
}

const createMemoryWrapper = (initialEntries: string[]) => {
  const queryClient = createTestQueryClient()
  return ({ children }: { children: ReactNode }) => (
    <ThemeProvider defaultTheme="system">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>
  )
}

describe('App', () => {
  beforeEach(() => {
    mockUseAuth.mockClear()
    mockUseUser.mockClear()
    mockUseClerk.mockClear()
    mockUseUpdatePrivacySettings.mockClear()
    mockFetch.mockClear()
    mockPosthogCapture.mockClear()
    resetAnalyticsLifecycleForTests()
    window.ENV = { ...window.ENV, POSTHOG_KEY: 'ph_test' }
    Cookies.remove(COOKIE_CONSENT_NAME)

    // Default privacy settings mock
    mockUseUpdatePrivacySettings.mockReturnValue({
      mutate: mock(() => {}),
      mutateAsync: mock(() => Promise.resolve()),
      isPending: false,
      isError: false,
      isSuccess: false,
      error: null,
      data: null,
      reset: mock(() => {}),
    })
  })

  describe('when user is not authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should render landing page with hero section', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(/take control/i)
      })
    })

    it('should render start free today CTA', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        // Multiple CTAs on landing page
        const ctaButtons = screen.getAllByRole('button', { name: /start free today/i })
        expect(ctaButtons.length).toBeGreaterThanOrEqual(1)
      })
    })

    it('should not render user menu', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByRole('button', { name: /user menu/i })).not.toBeInTheDocument()
      })
    })

    it('reconsiders a first-visit anonymous grant after sign-in and logout in the same SPA', async () => {
      mockUseGetPrivacySettings.mockReturnValue({
        data: { gdpr_analytics_consent: false },
        isLoading: false,
        isFetching: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      } as never)
      const Wrapper = createMemoryWrapper(['/'])
      const analyticsPageViews = {
        capture: mockPosthogCapture,
        setRoute: mock(() => {}),
        getSnapshot: getAnalyticsLifecycleSnapshot,
        subscribe: subscribeToAnalyticsLifecycle,
      }
      const { rerender } = render(<App analyticsPageViews={analyticsPageViews} />, { wrapper: Wrapper })

      await userEvent.click(await screen.findByRole('button', { name: /accept all cookies/i }))
      expect(getAnalyticsLifecycleState()).toBe('granted')
      expect(mockPosthogCapture).toHaveBeenCalledWith('/')

      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({
        isSignedIn: true,
        user: { ...mockClerkUser, id: 'user-a' },
      }))
      rerender(<App analyticsPageViews={analyticsPageViews} />)
      await waitFor(() => expect(getAnalyticsLifecycleState()).toBe('denied'))

      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, user: null }))
      rerender(<App analyticsPageViews={analyticsPageViews} />)

      await waitFor(() => expect(getAnalyticsLifecycleState()).toBe('granted'))
      expect(mockPosthogCapture.mock.calls.map(call => call[0])).toEqual(['/', '/'])
      rerender(<App analyticsPageViews={analyticsPageViews} />)
      expect(mockPosthogCapture.mock.calls.map(call => call[0])).toEqual(['/', '/'])
    })
  })

  it('captures the current route once when signed-in privacy resolves granted', async () => {
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
    mockUseUser.mockReturnValue(createMockClerkUser({
      isSignedIn: true,
      user: { ...mockClerkUser, id: 'user-a' },
    }))
    mockUseGetPrivacySettings.mockReturnValue({
      data: undefined,
      isLoading: true,
      isFetching: true,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as never)
    const Wrapper = createMemoryWrapper(['/legal/cookie-policy'])
    const analyticsPageViews = {
      capture: mockPosthogCapture,
      setRoute: mock(() => {}),
      getSnapshot: getAnalyticsLifecycleSnapshot,
      subscribe: subscribeToAnalyticsLifecycle,
    }
    const { rerender } = render(<App analyticsPageViews={analyticsPageViews} />, { wrapper: Wrapper })
    expect(mockPosthogCapture).not.toHaveBeenCalled()

    mockUseGetPrivacySettings.mockReturnValue({
      data: { gdpr_analytics_consent: true },
      isLoading: false,
      isFetching: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as never)
    rerender(<App analyticsPageViews={analyticsPageViews} />)
    await waitFor(() => expect(mockPosthogCapture).toHaveBeenCalledTimes(1))

    mockUseGetPrivacySettings.mockReturnValue({
      data: { gdpr_analytics_consent: true },
      isLoading: false,
      isFetching: true,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as never)
    rerender(<App analyticsPageViews={analyticsPageViews} />)

    mockUseGetPrivacySettings.mockReturnValue({
      data: { gdpr_analytics_consent: true },
      isLoading: false,
      isFetching: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as never)
    rerender(<App analyticsPageViews={analyticsPageViews} />)
    await waitFor(() => expect(getAnalyticsLifecycleState()).toBe('granted'))

    rerender(<App analyticsPageViews={analyticsPageViews} />)
    expect(mockPosthogCapture).toHaveBeenCalledTimes(1)
  })

  describe('when user is authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
      mockUseUser.mockReturnValue(
        createMockClerkUser({
          isSignedIn: true,
          user: {
            ...mockClerkUser,
            fullName: 'Test User',
            primaryEmailAddress: { emailAddress: 'test@example.com' },
          },
        })
      )
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    // #1601: Header is React.lazy() (see App.tsx), so its arrival is a genuine async
    // boundary — a dynamic import resolving, not a synchronous render — and the user
    // menu button lives inside it. Before Header became lazy this waitFor's default
    // 1000ms budget was never really exercised; under CI load a lazy chunk can take
    // longer than that to resolve, so this and every other Header-dependent assertion
    // below use the file's established { timeout: 10000 } instead of the bare default.
    it('should render user menu button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
    })

    // #1601 review finding 6: Header became React.lazy(), and nothing exercised the BOUNDARY —
    // Header.test.tsx imports the component directly, and e2e/navigation.auth.spec.ts's bodies
    // are empty stubs. The risk of lazy chrome is not that it fails to appear (the user-menu
    // assertions here would catch that) but that its arrival SHIFTS the layout, which no existing
    // assertion could see.
    //
    // The suspended frame itself is NOT assertable: RTL's render() wraps in act(), which flushes
    // the microtask resolving the lazy chunk, so the placeholder is gone before any query can see
    // it. A first draft of this test asserted on it and failed for exactly that reason. What IS
    // deterministic — and what actually matters — is that the placeholder reserves the same box
    // the real header occupies.
    it('the lazy Header resolves, and its placeholder reserves the same box (no layout shift)', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      // The lazy boundary genuinely resolves: without this the header never arrives and every
      // authenticated route renders headless. Explicit timeout: this is an async import
      // resolving, not a synchronous render.
      await waitFor(() => expect(screen.getByRole('banner')).toBeInTheDocument(), {
        timeout: 10000,
      })
      const headerClass = screen.getByRole('banner').className

      // Render the stand-in on its own and compare geometry. Asserted on BOTH sides rather than
      // hardcoded once, so changing Header's height without changing the placeholder fails here
      // instead of shipping a visible jump on every authenticated route.
      const { container: phContainer } = render(<HeaderPlaceholder />)
      const placeholder = phContainer.querySelector('[data-testid="header-placeholder"]')
      expect(placeholder).not.toBeNull()
      const phClass = placeholder!.className

      for (const geometry of ['h-16', 'sticky', 'top-0', 'border-b']) {
        expect(headerClass).toContain(geometry)
        expect(phClass).toContain(geometry)
      }

      // aria-hidden: an empty box announced as a banner with no navigation is worse than silence.
      expect(placeholder!.getAttribute('aria-hidden')).toBe('true')
    })

    it('should not render login button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByText(/log in/i)).not.toBeInTheDocument()
      })
    })

    it('should display user name in dropdown when opened', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(
        () => {
          expect(screen.getByText('Test User')).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
    }, 60000)

    it('should display user email in dropdown when opened', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(
        () => {
          expect(screen.getByText('test@example.com')).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
    }, 60000)
  })

  // #1598: Clerk's default sign-in redirect target had no route, so a Clerk-initiated redirect
  // (an expired session, most commonly) fell through to path="*" and showed the user
  // "404 Page Not Found" instead of a way to sign back in. This app has no sign-in PAGE on
  // purpose — LandingHeader opens Clerk's MODAL — so "/" is the correct destination, and it is
  // already where ProtectedRoute sends unauthenticated users.
  //
  // Asserted on the RENDERED OUTCOME rather than on ClerkProvider's props: the props are one of
  // two mechanisms (the /sign-in and /sign-up routes in App.tsx are the other), and what actually
  // matters is that neither of these URLs can ever render the 404 page.
  describe('sign-in / sign-up URLs never render the 404 page', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    for (const path of ['/sign-in', '/sign-up']) {
      it(`${path} does not render NotFoundPage`, async () => {
        const Wrapper = createMemoryWrapper([path])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(screen.queryByText('Page Not Found')).not.toBeInTheDocument()
        })
        // And the 404's numeral is absent too — NotFoundPage renders both, so checking one alone
        // could pass on a partial render.
        expect(screen.queryByText('404')).not.toBeInTheDocument()
      })
    }
  })

  describe('landing page sections', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should display features section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /why choose boostedbudget/i })).toBeInTheDocument()
      })
    })

    it('should display how it works section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /how it works/i })).toBeInTheDocument()
      })
    })

    it('should display FAQ section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /frequently asked questions/i })).toBeInTheDocument()
      })
    })
  })

  describe('transactions navigation', () => {
    describe('when user is authenticated', () => {
      beforeEach(() => {
        mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
        mockUseUser.mockReturnValue(
          createMockClerkUser({
            isSignedIn: true,
            user: {
              ...mockClerkUser,
              fullName: 'Test User',
              primaryEmailAddress: { emailAddress: 'test@example.com' },
            },
          })
        )
        mockUseClerk.mockReturnValue(createMockClerk())
      })

      it('should display transactions link in navigation', async () => {
        const Wrapper = createWrapper()
        render(<App />, { wrapper: Wrapper })

        // The transactions link lives inside the lazy-loaded Header — explicit timeout,
        // same reasoning as the Header assertions above.
        await waitFor(
          () => {
            const transactionsLink = screen.getByRole('link', { name: /transactions/i })
            expect(transactionsLink).toBeInTheDocument()
            expect(transactionsLink).toHaveAttribute('href', '/transactions')
          },
          { timeout: 10000 }
        )
      })
    })

    describe('when user is not authenticated', () => {
      beforeEach(() => {
        mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
        mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
        mockUseClerk.mockReturnValue(createMockClerk())
      })

      it('should not display transactions link in navigation', async () => {
        const Wrapper = createWrapper()
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          const transactionsLink = screen.queryByRole('link', { name: /transactions/i })
          expect(transactionsLink).not.toBeInTheDocument()
        })
      })
    })
  })

  describe('hash fragment navigation (issue #1411 B1 fix)', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('scrolls to the #features section when the URL has a matching hash', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      try {
        const Wrapper = createMemoryWrapper(['/#features'])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth' })
        })
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
      }
    })

    it('scrolls to the #faq section when the URL has a matching hash', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      try {
        const Wrapper = createMemoryWrapper(['/#faq'])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth' })
        })
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
      }
    })

    // Codex review finding (post-B1 fix pass): /mcp is lazy-loaded (`lazy(() =>
    // import('./features/mcp')...)` above). On a cold load of `/mcp#capabilities`, this
    // effect can run while only <PageSkeleton /> has rendered — getElementById finds
    // nothing and, without a retry, never scrolls at all. This pins the retry behavior:
    // the target only exists once the lazy chunk resolves and Suspense swaps it in.
    it('scrolls to a target inside a lazy-loaded route once it renders (cold load of /mcp#capabilities)', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      try {
        const Wrapper = createMemoryWrapper(['/mcp#capabilities'])
        render(<App />, { wrapper: Wrapper })

        // McpPage is lazy — its "Capabilities" section (id="capabilities") does not
        // exist in the DOM until the dynamic import resolves and Suspense swaps in the
        // real content in place of <PageSkeleton />. This route also sits behind the
        // lazy Header (non-landing shell), compounding the wait — explicit timeout.
        await waitFor(
          () => {
            expect(
              screen.getByRole('heading', { level: 2, name: "What it can and can't do" })
            ).toBeInTheDocument()
          },
          { timeout: 10000 }
        )

        await waitFor(() => {
          expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth' })
        })
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
      }
    })

    it('does not scroll when the URL has no hash', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      try {
        const Wrapper = createMemoryWrapper(['/'])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
        })
        expect(scrollIntoViewMock).not.toHaveBeenCalled()
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
      }
    })

    it('does not scroll when the hash does not match any element id', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      try {
        const Wrapper = createMemoryWrapper(['/#does-not-exist'])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
        })
        expect(scrollIntoViewMock).not.toHaveBeenCalled()
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
      }
    })

    it('respects prefers-reduced-motion by scrolling instantly instead of smoothly', async () => {
      const scrollIntoViewMock = mock(() => {})
      const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
      HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

      const originalMatchMedia = window.matchMedia
      window.matchMedia = mock((query: string) => ({
        matches: query.includes('prefers-reduced-motion'),
        media: query,
        onchange: null,
        addListener: mock(() => {}),
        removeListener: mock(() => {}),
        addEventListener: mock(() => {}),
        removeEventListener: mock(() => {}),
        dispatchEvent: mock(() => true),
      })) as unknown as typeof window.matchMedia

      try {
        const Wrapper = createMemoryWrapper(['/#features'])
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto' })
        })
      } finally {
        HTMLElement.prototype.scrollIntoView = originalScrollIntoView
        window.matchMedia = originalMatchMedia
      }
    })
  })

  describe('MCP page routing (issue #1411)', () => {
    beforeEach(() => {
      // Signed-out, but Clerk finished loading — AuthReadyGate-equivalent state.
      // /mcp must render without authentication.
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('renders the MCP page at /mcp without authentication, inside the app shell', async () => {
      const Wrapper = createMemoryWrapper(['/mcp'])
      const { container } = render(<App />, { wrapper: Wrapper })

      // McpPage and the shared Header (non-landing shell) are both lazy — explicit
      // timeout, same reasoning as the other Header/lazy-route assertions in this file.
      await waitFor(
        () => {
          expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
            /your finances, in the agent you already use/i
          )
        },
        { timeout: 10000 }
      )

      // Prove this is the non-landing app shell, not just "some banner/footer exist"
      // (LandingHeader also has role="banner", and AppFooter renders unconditionally
      // in both branches — see App.tsx). The `landing-page` class is only applied when
      // isLandingPage is true, and the shared <main> wrapper only exists in the
      // non-landing branch — so asserting the h1 lives inside it, and that the root
      // never got the landing-page class, pins down which branch actually rendered.
      expect(container.querySelector('.landing-page')).not.toBeInTheDocument()
      const main = screen.getByRole('main')
      expect(within(main).getByRole('heading', { level: 1 })).toHaveTextContent(
        /your finances, in the agent you already use/i
      )

      // Shared Header + AppFooter render alongside that shared <main> (App.tsx
      // non-landing branch), confirming the app shell — not the landing shell.
      expect(screen.getByRole('banner')).toBeInTheDocument()
      expect(screen.getByRole('contentinfo')).toBeInTheDocument()
    })
  })
})
