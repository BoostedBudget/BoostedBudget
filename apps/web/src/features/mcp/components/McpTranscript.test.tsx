/**
 * McpTranscript Component Tests (Issue #1411)
 *
 * Both transcripts on the /mcp page share this component. Per
 * .superpowers/sdd/task-P8-brief.md § "Transcripts": illustrative-only,
 * `data-illustrative="true"`, a fixed figcaption (with an extra sentence when
 * the transcript shows write-confirmation status markers), real text-node
 * speaker labels (never CSS ::before), and real emoji (✅/⚠️/❌) never used —
 * only →, ⟳, ✕.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen, within } from '@testing-library/react'
import { McpTranscript } from './McpTranscript'

const BASE_FIGCAPTION =
  'Illustrative transcript. The conversation and confirmation flow reflect how MCP works. The account names and dollar figures are synthetic sample data, not customer data.'

const turns = [
  {
    speaker: 'You' as const,
    lines: ['My partner sent me a household invite. Accept it.']
  },
  {
    speaker: 'Agent' as const,
    lines: ['I found a pending household invite.', '→ I can review the invite details with you.']
  },
  {
    speaker: 'Agent' as const,
    lines: ["I can see the invite — it's from the account ending 8815 and it's still pending. I can't accept it."]
  }
]

describe('McpTranscript', () => {
  it('renders inside a figure marked data-illustrative="true"', () => {
    render(<McpTranscript turns={turns} />)
    const figure = document.querySelector('figure')
    expect(figure).toHaveAttribute('data-illustrative', 'true')
  })

  it('renders the "Sample data" chip', () => {
    render(<McpTranscript turns={turns} />)
    expect(screen.getByText('Sample data')).toBeInTheDocument()
  })

  it('renders the exact base figcaption when the transcript shows no status markers', () => {
    render(<McpTranscript turns={turns} />)
    const figure = document.querySelector('figure') as HTMLElement
    expect(within(figure).getByText(BASE_FIGCAPTION)).toBeInTheDocument()
  })

  it('appends the status-markers sentence when showsStatusMarkers is true', () => {
    render(<McpTranscript turns={turns} showsStatusMarkers />)
    const figure = document.querySelector('figure') as HTMLElement
    expect(
      within(figure).getByText(`${BASE_FIGCAPTION} Status markers are simplified for readability.`)
    ).toBeInTheDocument()
  })

  it('renders speaker labels as real text nodes', () => {
    render(<McpTranscript turns={turns} />)
    // getAllByText proves "Agent" is a real, queryable text node (not a CSS ::before,
    // which testing-library/DOM cannot see at all).
    expect(screen.getAllByText('Agent').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('You')).toBeInTheDocument()
  })

  it('renders each line of transcript content', () => {
    render(<McpTranscript turns={turns} />)
    expect(screen.getByText('My partner sent me a household invite. Accept it.')).toBeInTheDocument()
    expect(screen.getByText('I found a pending household invite.')).toBeInTheDocument()
    expect(screen.getByText('→ I can review the invite details with you.')).toBeInTheDocument()
  })

  it('never renders real status emoji (✅/⚠️/❌)', () => {
    render(<McpTranscript turns={turns} showsStatusMarkers />)
    const figure = document.querySelector('figure') as HTMLElement
    expect(figure.textContent).not.toMatch(/✅|⚠️|❌/)
  })

  it('makes the transcript body a horizontally-scrollable, focusable region', () => {
    render(<McpTranscript turns={turns} />)
    const region = screen.getByRole('region', {
      name: /illustrative agent transcript/i
    })
    expect(region).toHaveAttribute('tabIndex', '0')
  })
})
