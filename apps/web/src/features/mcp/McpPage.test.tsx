/**
 * McpPage Component Tests (Issue #1411)
 *
 * The public /mcp page. Assembles McpHero, two McpTranscript instances,
 * McpTocRail, and McpSetupTabs, plus the remaining plain-copy sections.
 * Per task-P8-brief.md "Tests to write first".
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, within } from '@testing-library/react'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpPage } from './McpPage'
import capabilitySummary from './data/capabilitySummary.json'

// "Two tools instead of N" is generated (§6) — N is allowedOperations, not hand-typed. See
// Codex review finding #4: the heading must not hard-code a count that can drift from the manifest.
const HOW_IT_WORKS_HEADING = `Two tools instead of ${capabilitySummary.allowedOperations}`

const SECTION_HEADINGS = [
  { id: 'overview', heading: 'What the MCP server is', tocLabel: 'Overview' },
  { id: 'capabilities', heading: "What it can and can't do", tocLabel: 'Capabilities' },
  { id: 'how-it-works', heading: HOW_IT_WORKS_HEADING, tocLabel: 'How it works' },
  { id: 'connect', heading: 'Connect your client', tocLabel: 'Connect' },
  { id: 'write-safety', heading: 'Every write waits for you', tocLabel: 'Write safety' },
  { id: 'example-prompts', heading: 'Example prompts', tocLabel: 'Example prompts' },
  { id: 'recipes', heading: 'Saved recipes', tocLabel: 'Recipes' },
  { id: 'data-and-access', heading: 'Data handling and access control', tocLabel: 'Data and access' },
  { id: 'limits', heading: 'Limits and known gaps', tocLabel: 'Limits' },
  { id: 'troubleshooting', heading: "If something doesn't connect", tocLabel: 'Troubleshooting' },
]

// Corrected per Codex review finding #1: the brief's original access-matrix draft omitted the
// household-coverage exception in backend/internal/huma/middleware/read_only.go:143-155 — a
// lapsed user who is a covered member of an active household plan keeps write access.
const ACCESS_MATRIX_TEXT =
  "You need a BoostedBudget account. During your 60-day trial and while your subscription is active, your agent can read your data and make changes you confirm. If your subscription lapses, reading keeps working and changes are blocked — unless you're a covered member of an active household plan, in which case your agent can keep making confirmed changes too."

function normalize(text: string | null): string {
  return (text ?? '').replace(/\s+/g, ' ').trim()
}

describe('McpPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    document.title = 'Original Title'
    const meta = document.createElement('meta')
    meta.setAttribute('name', 'description')
    meta.setAttribute('content', 'Original description')
    document.head.appendChild(meta)
    const link = document.createElement('link')
    link.setAttribute('rel', 'canonical')
    link.setAttribute('href', 'https://boostedbudget.com/')
    document.head.appendChild(link)
  })

  afterEach(() => {
    document.querySelectorAll('meta[name="description"]').forEach((n) => n.remove())
    document.querySelectorAll('link[rel="canonical"]').forEach((n) => n.remove())
    window.history.replaceState(null, '', '/mcp')
  })

  it('renders exactly one h1', () => {
    render(<McpPage />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('does not render a <main> element (App.tsx already provides one)', () => {
    render(<McpPage />);
    expect(document.querySelector('main')).toBeNull()
  })

  it('sets document.title, meta description, and canonical link on mount', () => {
    render(<McpPage />)
    expect(document.title).toBe('MCP Server — BoostedBudget')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute(
      'content',
      expect.stringContaining('MCP')
    )
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute(
      'href',
      'https://boostedbudget.com/mcp'
    )
  })

  // #1411 B2 fix: Cursor's remote-MCP auto-auth support is unverified (see the Cursor tab
  // and Limits section below) — the meta description must not promise it unqualified.
  // `stringContaining('MCP')` above would still pass for the old Cursor-containing
  // description, so this asserts its absence directly.
  it('does not name Cursor in the meta description (#1411 B2 fix)', () => {
    render(<McpPage />)
    const description = document.querySelector('meta[name="description"]')?.getAttribute('content')
    expect(description).not.toBeNull()
    expect(description).not.toContain('Cursor')
  })

  it('restores document.title, meta description, and canonical link on unmount', () => {
    const { unmount } = render(<McpPage />)
    unmount()
    expect(document.title).toBe('Original Title')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute(
      'content',
      'Original description'
    )
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute(
      'href',
      'https://boostedbudget.com/'
    )
  })

  it('fires MCP_PAGE_VIEWED exactly once on mount', () => {
    render(<McpPage />)
    const viewedCalls = mockTrackEvent.mock.calls.filter(
      (call) => call[0] === ANALYTICS_EVENTS.MCP_PAGE_VIEWED
    )
    expect(viewedCalls).toHaveLength(1)
  })

  describe.each(SECTION_HEADINGS)('section "$heading"', ({ id, heading }) => {
    it(`has an h2 with id="${id}" wired to its section's aria-labelledby`, () => {
      render(<McpPage />)
      const h2 = screen.getByRole('heading', { level: 2, name: heading })
      expect(h2).toHaveAttribute('id', id)
      const section = h2.closest('section')
      expect(section).not.toBeNull()
      expect(section).toHaveAttribute('aria-labelledby', id)
    })
  })

  it('renders the TOC with one anchor per section, matching ids and labels', () => {
    render(<McpPage />)
    const nav = screen.getByRole('navigation', { name: 'On this page' })
    const links = within(nav).getAllByRole('link')
    expect(links).toHaveLength(SECTION_HEADINGS.length)
    links.forEach((link, i) => {
      expect(link).toHaveAttribute('href', `#${SECTION_HEADINGS[i].id}`)
      expect(link).toHaveTextContent(SECTION_HEADINGS[i].tocLabel)
    })
  })

  // Codex review finding #6: a #connect-<slug> hash correctly selected the right setup tab,
  // but nothing scrolled the page there — a support link landed the user at the page top.
  it('scrolls the Connect section into view on mount when the URL has a #connect-<slug> hash', () => {
    window.history.replaceState(null, '', '/mcp#connect-cursor')
    const scrollIntoViewMock = mock(() => {})
    const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

    try {
      render(<McpPage />)
      // The Connect section's h2 carries id="connect" — confirm it exists and that
      // scrollIntoView fired exactly once (targeting that element, per the source).
      expect(screen.getByRole('heading', { level: 2, name: 'Connect your client' })).toHaveAttribute(
        'id',
        'connect'
      )
      expect(scrollIntoViewMock).toHaveBeenCalledTimes(1)
    } finally {
      HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    }
  })

  it('does not scroll on mount when there is no #connect-<slug> hash', () => {
    const scrollIntoViewMock = mock(() => {})
    const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollIntoViewMock

    try {
      render(<McpPage />)
      expect(scrollIntoViewMock).not.toHaveBeenCalled()
    } finally {
      HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    }
  })

  it('renders the access-matrix paragraph verbatim', () => {
    render(<McpPage />)
    const match = screen.getAllByText((_, node) => normalize(node?.textContent) === ACCESS_MATRIX_TEXT)
    expect(match.length).toBeGreaterThan(0)
  })

  it('never writes "requires a paid plan" anywhere on the page', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/requires a paid plan/i)
  })

  it('renders two illustrative transcripts, each with the exact base figcaption text', () => {
    render(<McpPage />)
    const figures = document.querySelectorAll('figure[data-illustrative="true"]')
    expect(figures).toHaveLength(2)
    figures.forEach((figure) => {
      const figcaption = figure.querySelector('figcaption')
      expect(figcaption?.textContent).toContain(
        'Illustrative transcript. The tool names, operation IDs, and confirmation flow are exactly what the server does. The account names and dollar figures are synthetic sample data, not customer data.'
      )
    })
  })

  it('renders the household-invite refusal transcript verbatim in §2', () => {
    render(<McpPage />)
    expect(
      screen.getByText('My partner sent me a household invite. Accept it.')
    ).toBeInTheDocument()
    expect(
      screen.getByText(/Accepting a household invite is consent to share financial data/)
    ).toBeInTheDocument()
  })

  it('renders the "not configured" state for the setup tabs by default (no MCP URL in test env)', () => {
    render(<McpPage />)
    expect(screen.getByText(/not configured in this environment/i)).toBeInTheDocument()
  })

  it('renders the generated allowed-operations count from capabilitySummary.json, not a hand-typed literal', () => {
    render(<McpPage />)
    // capabilitySummary.json is generated data; assert only that the displayed number is a
    // positive integer followed by "allowed operations" rather than pinning a specific value
    // (that would re-introduce exactly the drift risk §6 exists to prevent).
    const chip = screen.getByText(/^\d+ allowed operations$/)
    expect(chip).toBeInTheDocument()
  })

  it('does not claim the agent writes and runs code against your finances', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/writes and runs code/i)
  })

  it('does not claim tokens are bound to your OAuth grant', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/bound to your OAuth grant/i)
  })

  it('does not claim recipes are read-only', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/recipes are read-only/i)
  })

  it('does not claim disconnect is available from account settings or revocation is instant', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/disconnect from account settings/i)
    expect(document.body.textContent).not.toMatch(/revoked instantly/i)
  })

  it('does not claim recipes can be set up to run automatically', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/set up recipes to run automatically/i)
  })

  it('does not use unfalsifiable security adjectives', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/bank-grade|military-grade|zero-trust/i)
  })

  it('does not claim the agent can move money', () => {
    render(<McpPage />)
    expect(document.body.textContent).not.toMatch(/let your agent move money/i)
  })

  it('states the rate limits are per session, not per account', () => {
    render(<McpPage />)
    expect(screen.getByText(/60 requests\/minute/)).toBeInTheDocument()
    expect(document.body.textContent).toMatch(/per session, not per account/i)
  })
})
