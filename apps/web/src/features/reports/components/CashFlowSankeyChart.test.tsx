import { describe, it, expect, beforeEach } from 'bun:test'
import { render as rtlRender, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import React from 'react'

import '@/test/mocks/shared-charts'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { createLocationHarness } from '@/test/utils/locationProbe'
import { CashFlowSankeyChart } from './CashFlowSankeyChart'
import type { CategoryHierarchyNode, CategoryHierarchyResponse } from '@boostedbudget/api-client/models'

// CashFlowSankeyChart calls useNavigate() unconditionally, so every render needs a real Router
// ancestor. A location probe rendered alongside the component lets tests assert on REAL navigation
// rather than on a mocked function — the URL is the contract the transactions page consumes.
const { LocationProbe, currentUrl, reset: resetLocationProbe } = createLocationHarness()

function render(ui: React.ReactElement) {
  return rtlRender(
    <MemoryRouter>
      <LocationProbe />
      {ui}
    </MemoryRouter>,
  )
}

function node(
  category: string,
  display: string,
  total: number,
  directTotal = total,
  children: CategoryHierarchyNode[] = [],
): CategoryHierarchyNode {
  return { category, display, total, direct_total: directTotal, percentage: 0, transaction_count: 1, children }
}

function hierarchy(levels: CategoryHierarchyNode[], direction: string): CategoryHierarchyResponse {
  return {
    levels,
    total: levels.reduce((s, l) => s + l.total, 0),
    direction,
    start_date: '2024-01-01',
    end_date: '2024-01-31',
  }
}

const mockIncomeData = hierarchy([node('INCOME_WAGES', 'Wages', 5000)], 'income')
const mockExpenseData = hierarchy(
  [
    node('FOOD_AND_DRINK', 'Food And Drink', 1200, 400, [
      node('FOOD_AND_DRINK_FAST_FOOD', 'Fast Food', 500),
      node('custom:abc', 'Eating Out', 300),
    ]),
    node('TRANSPORTATION', 'Transportation', 800),
  ],
  'expense',
)

let focused: string[] = []

const defaultProps = {
  incomeData: mockIncomeData,
  expenseData: mockExpenseData,
  loading: false,
  privacyMode: false,
  startDate: '2024-01-01',
  endDate: '2024-01-31',
  viewMode: { kind: 'expanded' } as const,
  onFocus: (key: string) => { focused.push(key) },
}

beforeEach(() => {
  resetLocationProbe()
  focused = []
  mockTrackEvent.mockClear()
})

describe('CashFlowSankeyChart', () => {
  it('renders a loading skeleton', () => {
    render(<CashFlowSankeyChart {...defaultProps} loading />)
    expect(screen.getByLabelText('Loading cash flow chart')).toBeTruthy()
  })

  it('renders an empty state rather than a phantom chart', () => {
    render(<CashFlowSankeyChart {...defaultProps} incomeData={null} expenseData={null} />)
    expect(screen.getByText('No cash flow data for this period.')).toBeTruthy()
  })

  it('exposes every flow in the screen-reader table', () => {
    render(<CashFlowSankeyChart {...defaultProps} />)
    expect(screen.getByText(/Wages → Total Income/)).toBeTruthy()
    expect(screen.getByText(/Food And Drink → Fast Food/)).toBeTruthy()
    expect(screen.getByText(/Food And Drink → Eating Out/)).toBeTruthy()
  })

  it('never interpolates a category name into an attribute', () => {
    render(<CashFlowSankeyChart {...defaultProps} />)
    // data-ph-mask covers TEXT, not attribute values — session replay records those verbatim — so
    // a user-authored custom category name must reach the DOM as text only.
    for (const el of Array.from(document.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.value).not.toContain('Eating Out')
      }
    }
  })

  describe('the action model', () => {
    it('a PFC detailed leaf navigates by pfc_detaileds only, never widened to its primary', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      fireEvent.click(screen.getByText(/Food And Drink → Fast Food/))

      expect(currentUrl()).toContain('pfc_detaileds=FOOD_AND_DRINK_FAST_FOOD')
      expect(currentUrl()).not.toContain('pfc_primaries=')
      expect(currentUrl()).toContain('amount_type=expense')
    })

    it('a custom leaf navigates by custom_category_ids with the token stripped', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      fireEvent.click(screen.getByText(/Food And Drink → Eating Out/))

      expect(currentUrl()).toContain('custom_category_ids=abc')
      expect(currentUrl()).not.toContain('custom%3Aabc')
    })

    it('an income node navigates with amount_type=income', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      fireEvent.click(screen.getByText(/Wages → Total Income/))
      expect(currentUrl()).toContain('amount_type=income')
    })

    it('a parent FOCUSES instead of navigating', () => {
      render(<CashFlowSankeyChart {...defaultProps} viewMode={{ kind: 'parents' }} />)
      fireEvent.click(screen.getByText(/Total Income → Food And Drink/))

      expect(focused).toEqual(['FOOD_AND_DRINK'])
      expect(currentUrl()).not.toContain('/transactions')
    })

    it('focusing a category is tracked, carrying the LEVEL and nothing identifying', () => {
      render(<CashFlowSankeyChart {...defaultProps} viewMode={{ kind: 'parents' }} />)
      fireEvent.click(screen.getByText(/Total Income → Food And Drink/))

      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.REPORT_SANKEY_CATEGORY_FOCUSED,
        { level: 'l1' },
      )
      // Event properties are stored verbatim and are NOT covered by data-ph-mask, so a category's
      // name, id or amount must never appear in them.
      const call = mockTrackEvent.mock.calls[0] as unknown as [string, Record<string, unknown>]
      const props = JSON.stringify(call?.[1] ?? {})
      expect(props).not.toContain('Food And Drink')
      expect(props).not.toContain('FOOD_AND_DRINK')
      expect(props).not.toContain('custom:')
    })

    it('every navigate action carries exactly ONE filter value', () => {
      // This is why no URL-length cap is needed: parents focus, and a leaf resolves to a single
      // custom id, detailed enum or primary. If a future change accumulated descendants' ids into
      // one action, the query string would grow unbounded and session replay records it verbatim.
      render(<CashFlowSankeyChart {...defaultProps} />)
      for (const label of [/Food And Drink → Fast Food/, /Food And Drink → Eating Out/]) {
        resetLocationProbe()
        fireEvent.click(screen.getByText(label))
        const params = new URLSearchParams(currentUrl().split('?')[1] ?? '')
        const values = ['pfc_primaries', 'pfc_detaileds', 'custom_category_ids']
          .flatMap(k => (params.get(k) ?? '').split(',').filter(Boolean))
        expect(values).toHaveLength(1)
      }
    })

    it('a synthetic node is inert — it neither navigates nor acts on its parent', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      // A "(direct)" bucket has no expressible transactions filter. Falling back to the source
      // would make this row act on Food And Drink, duplicating a row that already exists and
      // acting on something this row's label does not name.
      const directRow = screen.getByText(/Food And Drink → Food And Drink \(direct\)/)
      expect(directRow.tagName).toBe('SPAN')
      expect(focused).toEqual([])
    })
  })

  it('masks the chart canvas and the sr-only table', () => {
    render(<CashFlowSankeyChart {...defaultProps} />)
    expect(document.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    expect(document.querySelector('table')?.hasAttribute('data-ph-mask')).toBe(true)
  })
})
