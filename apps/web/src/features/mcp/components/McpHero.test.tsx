/**
 * McpHero Component Tests (Issue #1411)
 *
 * Hero copy is verbatim per .superpowers/sdd/task-P8-brief.md § "Hero copy — verbatim".
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { fireEvent } from '@testing-library/react'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpHero } from './McpHero'

function renderHero(allowedOperationsCount = 133) {
  return render(<McpHero allowedOperationsCount={allowedOperationsCount} />)
}

describe('McpHero', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
  })

  it('renders exactly one h1 with the verbatim headline', () => {
    renderHero()
    const headings = screen.getAllByRole('heading', { level: 1 })
    expect(headings).toHaveLength(1)
    expect(headings[0]).toHaveTextContent('Your finances, in the agent you already use.')
  })

  it('renders the verbatim subhead', () => {
    renderHero()
    expect(
      screen.getByText(
        /BoostedBudget runs a remote MCP server\. Point Claude or VS Code at one URL, sign in once, and your agent can read across your accounts, transactions, budgets, and portfolio — and change things only after you confirm each change\./
      )
    ).toBeInTheDocument()
  })

  it('renders the primary CTA anchored to #connect', () => {
    renderHero()
    const link = screen.getByRole('link', { name: /connect your client/i })
    expect(link).toHaveAttribute('href', '#connect')
  })

  it('renders the secondary CTA anchored to #capabilities', () => {
    renderHero()
    const link = screen.getByRole('link', { name: /see exactly what it can do/i })
    expect(link).toHaveAttribute('href', '#capabilities')
  })

  it('renders the verbatim under-CTA line', () => {
    renderHero()
    expect(
      screen.getByText('You paste a URL. There is no API key and no token to manage.')
    ).toBeInTheDocument()
  })

  it('renders the trust strip with best-effort audit language matching McpPage.tsx\'s detailed copy (audit-client.ts is a no-op when unconfigured and swallows failures; agent.ts fires audit.record without awaiting it, so a write can commit with no audit entry — round 3 of Codex review on this claim)', () => {
    renderHero()
    expect(
      screen.getByText(
        /The Worker never touches the database\. Your credentials are injected server-side\. Tool calls are recorded to an audit log on a best-effort basis\./
      )
    ).toBeInTheDocument()
    // Reject the unconditional form ("...audit log." with no best-effort qualifier) —
    // that overstates a guarantee the code doesn't provide.
    expect(
      screen.queryByText(
        /Tool calls are recorded to an audit log\.(?! on a best-effort basis)/
      )
    ).not.toBeInTheDocument()
  })

  it('renders the proof chips, including the generated allowed-operations count', () => {
    renderHero(133)
    expect(screen.getByText('133 allowed operations')).toBeInTheDocument()
    expect(screen.getByText('Every write confirmed')).toBeInTheDocument()
    expect(screen.getByText('No API key')).toBeInTheDocument()
  })

  it('renders a different allowed-operations count when the prop changes (proves the number is not hand-typed)', () => {
    renderHero(999)
    expect(screen.getByText('999 allowed operations')).toBeInTheDocument()
    expect(screen.queryByText('133 allowed operations')).not.toBeInTheDocument()
  })

  it('fires MCP_PAGE_CTA_CLICKED when the primary CTA is clicked', () => {
    renderHero()
    fireEvent.click(screen.getByRole('link', { name: /connect your client/i }))
    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.MCP_PAGE_CTA_CLICKED)
  })
})
