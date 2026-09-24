/**
 * McpPage Component Tests (Issue #1411)
 *
 * The public /mcp page. Assembles McpHero, two McpTranscript instances,
 * McpTocRail, and McpSetupTabs, plus the remaining plain-copy sections.
 * Per task-P8-brief.md "Tests to write first".
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpPage } from './McpPage'
import capabilitySummary from './data/capabilitySummary.json'

const HOW_IT_WORKS_HEADING = 'How it works'

const SECTION_HEADINGS = [
  { id: 'overview', heading: 'What the MCP server is', tocLabel: 'Overview' },
  {
    id: 'capabilities',
    heading: "What it can and can't do",
    tocLabel: 'Capabilities'
  },
  {
    id: 'how-it-works',
    heading: HOW_IT_WORKS_HEADING,
    tocLabel: 'How it works'
  },
  { id: 'connect', heading: 'Connect your client', tocLabel: 'Connect' },
  {
    id: 'write-safety',
    heading: 'Every write waits for you',
    tocLabel: 'Write safety'
  },
  {
    id: 'example-prompts',
    heading: 'Example prompts',
    tocLabel: 'Example prompts'
  },
  { id: 'recipes', heading: 'Saved recipes', tocLabel: 'Recipes' },
  {
    id: 'data-and-access',
    heading: 'Data handling and access control',
    tocLabel: 'Data and access'
  },
  { id: 'limits', heading: 'Limits', tocLabel: 'Limits' },
  {
    id: 'troubleshooting',
    heading: "If something doesn't connect",
    tocLabel: 'Troubleshooting'
  },
  {
    id: 'agent-discovery',
    heading: 'Discovery files for agents',
    tocLabel: 'Agent discovery'
  }
]

// Corrected per Codex review finding #1: the brief's original access-matrix draft omitted the
// household-coverage exception in backend/internal/huma/middleware/read_only.go:143-155 — a
// lapsed user who is a covered member of an active household plan keeps write access.
const ACCESS_MATRIX_TEXT =
  "You need a BoostedBudget account. During your 60-day trial and while your subscription is active, your agent can read your data and make changes you confirm. If your subscription lapses, reading keeps working and changes are blocked — unless you're a covered member of an active household plan, in which case your agent can keep making confirmed changes too."

function normalize(text: string | null): string {
  return (text ?? '').replace(/\s+/g, ' ').trim()
}

function renderPage() {
  return render(
    <MemoryRouter>
      <McpPage />
    </MemoryRouter>
  )
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
    renderPage()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('does not render a <main> element (App.tsx already provides one)', () => {
    renderPage()
    expect(document.querySelector('main')).toBeNull()
  })

  it('sets document.title, meta description, and canonical link on mount', () => {
    renderPage()
    expect(document.title).toBe('MCP Server — BoostedBudget')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute(
      'content',
      expect.stringContaining('MCP')
    )
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', 'https://boostedbudget.com/mcp')
  })

  // #1411 B2 fix, still true after the #1430 genericization of the setup tab: the meta
  // description names specific verified clients (Claude, VS Code) and must not overclaim
  // to unnamed others by mentioning Cursor specifically. `stringContaining('MCP')` above
  // would still pass for the old Cursor-containing description, so this asserts its
  // absence directly.
  it('does not name Cursor in the meta description (#1411 B2 fix)', () => {
    renderPage()
    const description = document.querySelector('meta[name="description"]')?.getAttribute('content')
    expect(description).not.toBeNull()
    expect(description).not.toContain('Cursor')
  })

  it('restores document.title, meta description, and canonical link on unmount', () => {
    const { unmount } = renderPage()
    unmount()
    expect(document.title).toBe('Original Title')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'Original description')
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', 'https://boostedbudget.com/')
  })

  it('fires MCP_PAGE_VIEWED exactly once on mount', () => {
    renderPage()
    const viewedCalls = mockTrackEvent.mock.calls.filter((call) => call[0] === ANALYTICS_EVENTS.MCP_PAGE_VIEWED)
    expect(viewedCalls).toHaveLength(1)
  })

  describe.each(SECTION_HEADINGS)('section "$heading"', ({ id, heading }) => {
    it(`has an h2 with id="${id}" wired to its section's aria-labelledby`, () => {
      renderPage()
      const h2 = screen.getByRole('heading', { level: 2, name: heading })
      expect(h2).toHaveAttribute('id', id)
      const section = h2.closest('section')
      expect(section).not.toBeNull()
      expect(section).toHaveAttribute('aria-labelledby', id)
    })
  })

  it('renders the TOC with one anchor per section, matching ids and labels', () => {
    renderPage()
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
      renderPage()
      // The Connect section's h2 carries id="connect" — confirm it exists and that
      // scrollIntoView fired exactly once (targeting that element, per the source).
      expect(screen.getByRole('heading', { level: 2, name: 'Connect your client' })).toHaveAttribute('id', 'connect')
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
      renderPage()
      expect(scrollIntoViewMock).not.toHaveBeenCalled()
    } finally {
      HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    }
  })

  it('renders the access-matrix paragraph verbatim', () => {
    renderPage()
    const match = screen.getAllByText((_, node) => normalize(node?.textContent) === ACCESS_MATRIX_TEXT)
    expect(match.length).toBeGreaterThan(0)
  })

  it('never writes "requires a paid plan" anywhere on the page', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/requires a paid plan/i)
  })

  it('renders two illustrative transcripts, each with the exact base figcaption text', () => {
    renderPage()
    const figures = document.querySelectorAll('figure[data-illustrative="true"]')
    expect(figures).toHaveLength(2)
    figures.forEach((figure) => {
      const figcaption = figure.querySelector('figcaption')
      expect(figcaption?.textContent).toContain(
        'Illustrative transcript. The conversation and confirmation flow reflect how MCP works. The account names and dollar figures are synthetic sample data, not customer data.'
      )
    })
  })

  it('renders the household-invite refusal transcript verbatim in §2', () => {
    renderPage()
    expect(screen.getByText('My partner sent me a household invite. Accept it.')).toBeInTheDocument()
    expect(screen.getByText(/Accepting a household invite is consent to share financial data/)).toBeInTheDocument()
  })

  it('renders only the four user-facing MCP boundaries', () => {
    renderPage()

    const boundaries = screen.getByRole('list', {
      name: 'Actions that stay in BoostedBudget'
    })
    expect(within(boundaries).getByText('Bank connections')).toBeInTheDocument()
    expect(within(boundaries).getByText('Household membership and sharing')).toBeInTheDocument()
    expect(within(boundaries).getByText('Subscription changes')).toBeInTheDocument()
    expect(within(boundaries).getByText('Account deletion')).toBeInTheDocument()
    expect(within(boundaries).getAllByRole('listitem')).toHaveLength(4)
  })

  it('does not expose internal MCP taxonomy or implementation details', () => {
    renderPage()
    const text = document.body.textContent ?? ''

    expect(text).not.toMatch(/qa|testing endpoint|admin endpoint|webhook|internal service|follow-up issue/i)
    expect(text).not.toMatch(/worker|go backend|database|typed client|code mode|dynamic worker loader/i)
    expect(text).not.toMatch(/operationId|pathParams|recipe\.templates|#1220|#1419/i)
  })

  it('renders the "not configured" state for the setup tabs by default (no MCP URL in test env)', () => {
    renderPage()
    expect(screen.getByText(/not configured in this environment/i)).toBeInTheDocument()
  })

  it('renders the generated capability count from capabilitySummary.json, not a hand-typed literal', () => {
    renderPage()
    // capabilitySummary.json is generated data; assert that the displayed number comes from
    // it rather than pinning a specific value (which would re-introduce drift risk).
    const chip = screen.getByText(`${capabilitySummary.allowedOperations} capabilities`)
    expect(chip).toBeInTheDocument()
  })

  // #1569 task 20 (A5). capabilitySummary.json has carried destructiveWriteOperations since task
  // 16 and NOTHING read it: McpPage destructured allowedOperations, readOperations,
  // writeOperations and publicBoundaries, and stopped. So a public page whose whole subject is
  // "what can an agent do to my money" could not say that four of the capabilities it advertises
  // permanently delete something.
  it('tells the reader how many capabilities permanently delete data', () => {
    renderPage()
    const n = capabilitySummary.destructiveWriteOperations
    expect(n).toBeGreaterThan(0) // a zero would make the sentence below vacuously satisfiable
    // The lookbehind matters: "134 capabilities" also ends in a 4, and a bare `4\\b` would match
    // inside it. The number must be its own token.
    expect(document.body.textContent).toMatch(
      new RegExp(`(?<![0-9])${n}\\b[^.]*permanently delete`, 'i')
    )
  })

  // The arithmetic the page's own sentence depends on. writeOperations deliberately means
  // write-OR-destructive so that allowed === read + write holds; the destructive count is a SUBSET
  // of writeOperations, never a fourth bucket added alongside it. Asserted here because the
  // tempting way to surface the new number is to subtract it out, which would silently make the
  // sentence above the chip wrong.
  it('keeps allowed === read + write, with destructive a subset of write', () => {
    const s = capabilitySummary
    expect(s.readOperations + s.writeOperations).toBe(s.allowedOperations)
    expect(s.destructiveWriteOperations).toBeLessThanOrEqual(s.writeOperations)
  })

  it('does not claim the agent writes and runs code against your finances', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/writes and runs code/i)
  })

  it('does not claim tokens are bound to your OAuth grant', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/bound to your OAuth grant/i)
  })

  it('does not claim recipes are read-only', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/recipes are read-only/i)
  })

  it('does not claim disconnect is available from account settings or revocation is instant', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/disconnect from account settings/i)
    expect(document.body.textContent).not.toMatch(/revoked instantly/i)
  })

  it('does not claim recipes can be set up to run automatically', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/set up recipes to run automatically/i)
  })

  it('does not use unfalsifiable security adjectives', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/bank-grade|military-grade|zero-trust/i)
  })

  it('does not claim the agent can move money', () => {
    renderPage()
    expect(document.body.textContent).not.toMatch(/let your agent move money/i)
  })

  it('states the rate limits are per session, not per account', () => {
    renderPage()
    expect(screen.getByText(/60 requests\/minute/)).toBeInTheDocument()
    expect(document.body.textContent).toMatch(/per session, not per account/i)
  })

  it('describes saved reviews in plain language without internal tool names', () => {
    renderPage()
    expect(screen.getByText(/A recipe is a named, saved financial review/i)).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/recipe\.(save|list|run)/i)
  })

  it('describes account access without exposing implementation details', () => {
    renderPage()
    expect(screen.getByText(/same permissions used in the app/i)).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/worker|go backend|database|typed client/i)
  })

  it('states credentials are injected server-side and never shown to the agent', () => {
    renderPage()
    const text = document.body.textContent ?? ''
    expect(text).toMatch(/credentials are injected server-side.*never visible to the agent/i)
    expect(text).not.toMatch(/the agent never stores them/i)
  })

  it('cross-links to the /features and /faq pages', () => {
    renderPage()
    expect(document.querySelector('a[href="/features"]')).not.toBeNull()
    expect(document.querySelector('a[href="/faq"]')).not.toBeNull()
  })

  it('documents llms.txt / llms-full.txt as agent-readable documentation, not a ranking tactic', () => {
    renderPage()
    const text = document.body.textContent ?? ''
    expect(text).toContain('llms.txt')
    expect(text).toContain('llms-full.txt')
    expect(text).toMatch(/agent-readable documentation/i)
    expect(text).toMatch(/not an AI-search-ranking tactic/i)
  })

  it('makes no AI-search-ranking claim about the discovery files', () => {
    renderPage()
    const text = document.body.textContent ?? ''
    expect(text).not.toMatch(/(improve|boost|raise|increase).{0,40}(search )?ranking/i)
    expect(text).not.toMatch(/rank higher/i)
  })
})
