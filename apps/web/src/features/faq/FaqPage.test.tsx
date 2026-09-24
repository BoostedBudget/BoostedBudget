/**
 * FaqPage Tests (Issue #1666)
 *
 * The public /faq page. Created because the landing page's `#faq` anchor is
 * unreachable for a signed-in user — App.tsx computes
 * `isLandingPage = pathname === '/' && !isAuthenticated`, so `/` redirects to
 * /dashboard and the hash dies. This page is the real destination.
 *
 * Mirrors the FAQSection and McpPage test patterns: exactly one h1, a JSON-LD
 * FAQPage block whose question count equals the number of RENDERED questions
 * (asserted, not eyeballed), the accessible grid-row disclosure, and a
 * `trackEvent` assertion for the mount view event plus the closing CTA click.
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
import { FaqPage } from './FaqPage'

const AGENTIC_QUESTIONS = [
  'Can my AI agent help me manage my budget?',
  'Can an AI agent move my money?',
  'Is it safe to let an AI agent see my finances?',
]

function renderPage() {
  return render(
    <MemoryRouter>
      <FaqPage />
    </MemoryRouter>,
  )
}

function jsonLd() {
  const script = document.querySelector('script[type="application/ld+json"]')
  if (!script) throw new Error('no FAQPage JSON-LD script')
  return JSON.parse(script.textContent ?? '') as {
    '@context': string
    '@type': string
    mainEntity: Array<{
      '@type': string
      name: string
      acceptedAnswer: { '@type': string; text: string }
    }>
  }
}

/** The rendered question buttons — the ones that expose `aria-expanded`. */
function questionButtons(): HTMLButtonElement[] {
  return screen
    .getAllByRole('button')
    .filter((button) => button.hasAttribute('aria-expanded')) as HTMLButtonElement[]
}

/** The rendered answer text for one question, anchored to its own disclosure region. */
function answerText(question: string): string {
  const heading = screen
    .getAllByRole('heading', { level: 3 })
    .find((h) => h.textContent === question)
  if (!heading) throw new Error(`no FAQ question "${question}"`)
  const answerId = heading.closest('button')?.getAttribute('aria-controls')
  if (!answerId) throw new Error(`FAQ question "${question}" has no aria-controls`)
  return (document.getElementById(answerId)?.textContent ?? '').replace(/\s+/g, ' ').trim()
}

describe('FaqPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUseClerk.mockClear()
    mockUseClerk.mockReturnValue(createMockClerk())
  })

  it('renders exactly one h1', () => {
    renderPage()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('emits a single FAQPage JSON-LD block', () => {
    renderPage()
    expect(document.querySelectorAll('script[type="application/ld+json"]')).toHaveLength(1)
    const schema = jsonLd()
    expect(schema['@context']).toBe('https://schema.org')
    expect(schema['@type']).toBe('FAQPage')
  })

  it('renders as many questions as the JSON-LD mainEntity array has entries, at least 10', () => {
    renderPage()
    const rendered = questionButtons()
    const schemaQuestions = jsonLd().mainEntity
    // The load-bearing assertion: a schema that lists questions the page does
    // not render (or vice versa) is a silent SEO/content drift.
    expect(schemaQuestions.length).toBe(rendered.length)
    expect(schemaQuestions.length).toBeGreaterThanOrEqual(10)
  })

  it('keeps every JSON-LD question and every rendered question in the same set', () => {
    renderPage()
    const renderedQuestions = screen
      .getAllByRole('heading', { level: 3 })
      .map((h) => (h.textContent ?? '').trim())
    const schemaQuestions = jsonLd().mainEntity.map((entry) => entry.name)
    expect(new Set(schemaQuestions)).toEqual(new Set(renderedQuestions))
  })

  it('renders every question as a real button with aria-expanded and an id-linked answer', () => {
    renderPage()
    const buttons = questionButtons()
    expect(buttons.length).toBeGreaterThanOrEqual(10)
    for (const button of buttons) {
      expect(button.tagName).toBe('BUTTON')
      expect(button).toHaveAttribute('aria-expanded', 'false')
      const answerId = button.getAttribute('aria-controls')
      expect(answerId).toBeTruthy()
      expect(document.getElementById(answerId as string)).not.toBeNull()
    }
  })

  it('discloses an answer on click and reports the expanded state to assistive technology', () => {
    renderPage()
    const first = questionButtons()[0]
    const answerId = first.getAttribute('aria-controls') as string

    fireEvent.click(first)
    expect(first).toHaveAttribute('aria-expanded', 'true')
    expect(document.getElementById(answerId)?.className).toContain('grid-rows-[1fr]')

    fireEvent.click(first)
    expect(first).toHaveAttribute('aria-expanded', 'false')
  })

  it('collapses answers by grid row rather than a fixed max-height cap', () => {
    renderPage()
    const first = questionButtons()[0]
    const answerId = first.getAttribute('aria-controls') as string
    const region = document.getElementById(answerId) as HTMLElement
    expect(region.className).toContain('grid-rows-[0fr]')
    // A `max-h-*` collapse clips a long answer silently at a fixed cap; the
    // grid row animates to the content's height and cannot truncate.
    expect(region.className).not.toMatch(/max-h-\d/)
  })

  it('fires faq_page_viewed exactly once on mount', () => {
    renderPage()
    const viewedCalls = mockTrackEvent.mock.calls.filter(
      (call) => (call as unknown[])[0] === ANALYTICS_EVENTS.FAQ_PAGE_VIEWED,
    )
    expect(viewedCalls).toHaveLength(1)
  })

  it('fires faq_cta_clicked when the closing sign-up CTA is clicked', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /start free/i }))
    const ctaCalls = mockTrackEvent.mock.calls.filter(
      (call) => (call as unknown[])[0] === ANALYTICS_EVENTS.FAQ_CTA_CLICKED,
    )
    expect(ctaCalls).toHaveLength(1)
  })

  it('opens Clerk sign-up when the closing CTA is clicked', () => {
    const clerkMock = createMockClerk()
    mockUseClerk.mockReturnValue(clerkMock)

    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /start free/i }))
    expect(clerkMock.openSignUp).toHaveBeenCalledTimes(1)
  })

  it('exposes both events as valid catalogued zero-property analytics contracts', () => {
    expect(isValidAnalyticsEventPayload(ANALYTICS_EVENTS.FAQ_PAGE_VIEWED, {})).toBe(true)
    expect(isValidAnalyticsEventPayload(ANALYTICS_EVENTS.FAQ_CTA_CLICKED, {})).toBe(true)
  })

  it('includes the landing seven plus the three agentic questions', () => {
    renderPage()
    expect(screen.getByText('Is my bank data secure?')).toBeInTheDocument()
    expect(screen.getByText('How does BoostedBudget connect to my bank?')).toBeInTheDocument()
    expect(screen.getByText('Can I cancel anytime?')).toBeInTheDocument()
    expect(screen.getByText('What happens to my data if I cancel?')).toBeInTheDocument()
    expect(screen.getByText('Is there a free trial?')).toBeInTheDocument()
    expect(screen.getByText('Do you sell my financial data?')).toBeInTheDocument()
    expect(screen.getByText('Can my family share one subscription?')).toBeInTheDocument()
    for (const question of AGENTIC_QUESTIONS) {
      expect(screen.getByText(question)).toBeInTheDocument()
    }
  })

  describe('agentic answers stay within claims discipline', () => {
    it('states confirmation, no money movement, server-side credentials, and revocation', () => {
      renderPage()
      const combined = AGENTIC_QUESTIONS.map(answerText).join(' ')

      // Each required claim, one per acceptance criterion.
      expect(combined).toMatch(/confirmation/i)
      expect(combined).toMatch(/never moves money/i)
      expect(combined).toMatch(/injected server-side/i)
      expect(combined).toMatch(/never visible to the agent/i)
      expect(combined).toMatch(/revoke/i)
    })

    it('never promises an action the product cannot perform', () => {
      renderPage()
      for (const question of AGENTIC_QUESTIONS) {
        const answer = answerText(question)
        expect(answer).not.toMatch(/will cancel/i)
        expect(answer).not.toMatch(/transfers your money/i)
        expect(answer).not.toMatch(/accepts the invite/i)
      }
    })

    it('links readers to the MCP page', () => {
      renderPage()
      const mcpLinks = screen
        .getAllByRole('link')
        .filter((link) => link.getAttribute('href') === '/mcp')
      expect(mcpLinks.length).toBeGreaterThan(0)
    })
  })
})

/**
 * The defect this page exists to fix, asserted at the routing level for a
 * SIGNED-IN user: the landing `#faq` anchor is unreachable signed-in because
 * App.tsx sends `/` to /dashboard, so `/faq` must resolve to the real page.
 *
 * Rendered against the real `App` with Clerk mocked signed-in (the strongest
 * in-env equivalent — the app is Clerk-gated and this worktree has no Clerk key
 * to drive a real browser).
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

describe('FaqPage routing for a signed-in user (#1666 defect)', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUseAuth.mockClear()
    mockUseUser.mockClear()
    mockUseClerk.mockClear()

    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
    mockUseUser.mockReturnValue(
      createMockClerkUser({ isSignedIn: true, user: { ...mockClerkUser, id: 'user_faq' } }),
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

  it('renders /faq and does not redirect the signed-in user to /dashboard', async () => {
    render(
      <>
        <App />
        <LocationProbe />
      </>,
      { wrapper: appWrapper(['/faq']) },
    )

    await waitFor(
      () => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(/frequently asked questions/i)
      },
      { timeout: 10000 },
    )

    expect(screen.getByTestId('location-probe')).toHaveTextContent('/faq')
  })
})