import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { RulePreviewPanel } from './RulePreviewPanel'
import {
  mockPreviewRuleMutateAsync,
  mockUsePreviewRule,
} from '@/test/mocks/auto-categorization-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import type { RuleCondition } from '../types'

const CONDITIONS: RuleCondition[] = [
  { type: 'expression', field: 'name', operator: 'contains', value: 'jack liu' },
]

const MATCH = {
  transaction_id: 'tx-1',
  date: '2026-09-01T00:00:00Z',
  name: 'SENTINEL_DESC_QRS',
  merchant_name: 'SENTINEL_MERCHANT_XYZ',
  amount: 1000,
  current_category: 'SENTINEL_CATEGORY_RSTU',
}

function renderPanel(props?: Partial<Parameters<typeof RulePreviewPanel>[0]>) {
  return render(
    <RulePreviewPanel name="Zelle to Jack" conditions={CONDITIONS} conditionLogic="AND" {...props} />
  )
}

describe('RulePreviewPanel (#1625)', () => {
  beforeEach(() => {
    mockPreviewRuleMutateAsync.mockClear()
    mockTrackEvent.mockClear()
    mockUsePreviewRule.mockReturnValue({
      mutateAsync: mockPreviewRuleMutateAsync,
      isPending: false,
      error: null,
    } as never)
  })

  it('reports how many of the scanned transactions would match', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200,
      match_count: 1,
      matches: [MATCH],
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => {
      expect(screen.getByText(/of the last/i)).toBeInTheDocument()
    })
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  // ⚠ Every match cell is user financial data. Session replay must keep the SHAPE of the result
  // without keeping its contents, so each one has to sit inside data-ph-mask.
  it('masks every cell carrying transaction data', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200,
      match_count: 1,
      matches: [MATCH],
    } as never)

    const { container } = renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => expect(screen.getByText('SENTINEL_MERCHANT_XYZ')).toBeInTheDocument())

    for (const sentinel of ['SENTINEL_MERCHANT_XYZ', 'SENTINEL_CATEGORY_RSTU']) {
      const el = screen.getByText(sentinel)
      expect(el.closest('[data-ph-mask]')).not.toBeNull()
    }
    // The amount is rendered formatted, so it is found by its cell rather than the raw number.
    const cells = container.querySelectorAll('tbody td')
    for (const cell of cells) {
      expect(cell.hasAttribute('data-ph-mask')).toBe(true)
    }
  })

  // #1625 follow-up: the request SHAPE was previously checked by neither the compiler (the
  // payload was cast `as never`) nor any test — only the response was asserted. A preview that
  // silently sends the wrong conditions would still render a plausible result.
  it('sends the conditions currently on screen, not a stale or empty set', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200, match_count: 0, matches: [],
    } as never)

    renderPanel({ name: '  Zelle to Jack  ', conditionLogic: 'OR' })
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => expect(mockPreviewRuleMutateAsync).toHaveBeenCalled())
    const call = mockPreviewRuleMutateAsync.mock.calls[0] as unknown as [{ data: Record<string, unknown> }]
    const sent = call[0].data

    expect(sent.conditions).toEqual(CONDITIONS)
    expect(sent.condition_logic).toBe('OR')
    expect(sent.name).toBe('Zelle to Jack')
  })

  it('names an untitled rule rather than sending an empty one', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 0, match_count: 0, matches: [],
    } as never)

    renderPanel({ name: '   ' })
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => expect(mockPreviewRuleMutateAsync).toHaveBeenCalled())
    const call = mockPreviewRuleMutateAsync.mock.calls[0] as unknown as [{ data: Record<string, unknown> }]
    // Validate() rejects an empty name, so preview would 422 on a rule the user has not named yet.
    expect(call[0].data.name).toBe('Untitled rule')
  })

  it('surfaces a validation error instead of leaving the user to discover it on save', async () => {
    mockPreviewRuleMutateAsync.mockRejectedValueOnce({
      response: { data: { detail: 'invalid condition: unknown field "amount_typo"' } },
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/unknown field/i)
    })
    // The message quotes back what the user sent, so the element carrying it must be masked.
    expect(screen.getByRole('alert').closest('[data-ph-mask]')).not.toBeNull()
  })

  it('masks a validation error that quotes the user\'s own value back at them', async () => {
    // A malformed date is echoed verbatim by the domain validator; an unknown-field message
    // names what was sent. Either way the error line is user content, not a fixed string.
    mockPreviewRuleMutateAsync.mockRejectedValueOnce({
      response: { data: { detail: 'invalid value shape: "SENTINEL_MERCHANT_XYZ" is not a YYYY-MM-DD date' } },
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('SENTINEL_MERCHANT_XYZ')
    expect(alert.closest('[data-ph-mask]')).not.toBeNull()
  })

  it('says so plainly when nothing matched, without implying the rule is broken', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200,
      match_count: 0,
      matches: [],
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => {
      expect(screen.getByText(/nothing matched/i)).toBeInTheDocument()
    })
    expect(screen.getByText(/apply to future transactions/i)).toBeInTheDocument()
  })

  it('cannot be run with no conditions — that rule would match everything', () => {
    renderPanel({ conditions: [] })
    expect(screen.getByRole('button', { name: /preview matches/i })).toBeDisabled()
  })

  // Counts and the fixed vocabulary only — never a condition value or a merchant.
  it('tracks the preview without leaking any user content', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200,
      match_count: 1,
      matches: [MATCH],
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => expect(mockTrackEvent).toHaveBeenCalled())
    const call = mockTrackEvent.mock.calls[0] as unknown as [string, Record<string, unknown>]
    const props = call[1]
    const serialized = JSON.stringify(props)
    expect(serialized).not.toContain('jack liu')
    expect(serialized).not.toContain('SENTINEL_MERCHANT_XYZ')
    expect(props.match_count).toBe(1)
  })
})
