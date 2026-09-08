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

  it('surfaces a validation error instead of leaving the user to discover it on save', async () => {
    mockPreviewRuleMutateAsync.mockRejectedValueOnce({
      response: { data: { detail: 'invalid condition: unknown field "amount_typo"' } },
    } as never)

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/unknown field/i)
    })
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
