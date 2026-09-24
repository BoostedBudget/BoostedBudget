/**
 * McpBand Component Tests
 *
 * Issue #1411: full-width landing band introducing the MCP server and linking
 * to the public /mcp page (built by another task).
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { McpBand } from './McpBand'

function renderMcpBand() {
  return render(
    <BrowserRouter>
      <McpBand />
    </BrowserRouter>
  )
}

describe('McpBand', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
  })

  it('renders the exact verbatim h2 heading and no h1', () => {
    renderMcpBand()
    expect(
      screen.getByRole('heading', { level: 2, name: 'Connect It to Your Agent' })
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 1 })).not.toBeInTheDocument()
  })

  it('renders the exact verbatim body copy', () => {
    renderMcpBand()
    expect(
      screen.getByText(
        'BoostedBudget runs a remote MCP server. Point Claude, Cursor, VS Code, or any MCP-compatible client at one URL, sign in once, and your agent can read across your accounts, transactions, budgets, and portfolio — changing things only when you confirm each one.'
      )
    ).toBeInTheDocument()
  })

  it('does not make the unverifiable "thousands" claim', () => {
    const { container } = renderMcpBand()
    expect(container.textContent).not.toMatch(/thousands/i)
  })

  it('h2 id is the literal "mcp-band-heading" and matches the section aria-labelledby', () => {
    const { container } = renderMcpBand()
    const section = container.querySelector('section')
    const heading = screen.getByRole('heading', { level: 2, name: 'Connect It to Your Agent' })
    expect(section).not.toBeNull()
    expect(heading.id).toBe('mcp-band-heading')
    expect(section?.getAttribute('aria-labelledby')).toBe('mcp-band-heading')
  })

  it('renders a link to /mcp with accessible name "Explore the MCP server"', () => {
    renderMcpBand()
    const link = screen.getByRole('link', { name: 'Explore the MCP server' })
    expect(link).toHaveAttribute('href', '/mcp')
  })

  it('fires MCP_BAND_CTA_CLICKED analytics event when the /mcp link is clicked', () => {
    renderMcpBand()
    const link = screen.getByRole('link', { name: 'Explore the MCP server' })
    fireEvent.click(link)
    expect(mockTrackEvent).toHaveBeenCalledWith('mcp_band_cta_clicked')
  })

  it('does not render a "Start Free Today" button', () => {
    renderMcpBand()
    expect(screen.queryByRole('button', { name: /start free today/i })).not.toBeInTheDocument()
  })

  it('renders the illustrative transcript figure with the required synthetic-data labels', () => {
    const { container } = renderMcpBand()
    const figure = container.querySelector('figure')
    expect(figure).not.toBeNull()
    expect(figure).toHaveAttribute('data-illustrative', 'true')

    expect(screen.getByText('Sample data')).toBeInTheDocument()
    expect(
      screen.getByText('Illustrative transcript. Real tool calls, synthetic figures.')
    ).toBeInTheDocument()
  })

  it('renders "You" and "Agent" speaker labels as text', () => {
    renderMcpBand()
    expect(screen.getByText('You')).toBeInTheDocument()
    expect(screen.getAllByText('Agent').length).toBeGreaterThanOrEqual(1)
  })

  it('does not apply data-ph-mask to the synthetic transcript figures', () => {
    const { container } = renderMcpBand()
    expect(container.querySelectorAll('[data-ph-mask]').length).toBe(0)
  })

  it('makes the transcript scroll region keyboard-focusable so overflow at narrow widths stays reachable', () => {
    renderMcpBand()
    const region = screen.getByRole('region', { name: 'Illustrative agent transcript' })
    expect(region).toHaveAttribute('tabIndex', '0')
    expect(region).toHaveClass('overflow-x-auto')
  })
})
