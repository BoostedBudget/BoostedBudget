import { describe, it, expect, beforeEach } from 'bun:test'
import { render as rtlRender, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import React from 'react'

import { MockEChartsChart } from '@/test/mocks/shared-charts'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { createLocationHarness } from '@/test/utils/locationProbe'
import { CashFlowSankeyChart } from './CashFlowSankeyChart'
import { buildSankeyData } from './buildSankeyData'
import type { NodeAction } from './buildSankeyData'
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

// Resolves a node's index in the emitted node list — which is what ECharts reports as dataIndex.
// Derived from buildSankeyData rather than hard-coded so a change to node ordering makes the test
// click the right node instead of silently clicking a different one.
function nodeIndexOf(expenses: CategoryHierarchyResponse, id: string): number {
  const built = buildSankeyData(mockIncomeData.levels, expenses.levels, { kind: 'expanded' })
  const idx = built.nodes.findIndex(n => n.id === id)
  if (idx < 0) throw new Error(`no node ${id} in the built Sankey`)
  return idx
}

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
  MockEChartsChart.mockClear()
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

  // #1630: ECharts' sankey series reads a node-data field literally named `depth` as its LAYOUT
  // LAYER (its own docs: "the layer of the current node… starting from 0"), not as arbitrary
  // payload. `buildSankeyData` used to carry a field named `depth` for analytics only, and this
  // component passed the internal node objects straight through to ECharts
  // (`data: sankeyData.nodes as any`) — so ECharts silently adopted it as the render layer,
  // pinning synthetic nodes (depth 0) on the left and scattering income leaves (their real
  // category depth) among the expense layers on the right. A link/topology assertion cannot catch
  // this: the links were always correct, only the LAYOUT was wrong. These assertions are
  // option-level on purpose.
  describe('the ECharts series payload (#1630)', () => {
    it('projects node data down to id/name/itemStyle — no internal field (least of all `depth`) leaks through', () => {
      render(<CashFlowSankeyChart {...defaultProps} viewMode={{ kind: 'parents' }} />)
      const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        series?: Array<{ data?: Array<Record<string, unknown>> }>
      }
      const nodeData = option?.series?.[0]?.data ?? []
      expect(nodeData.length).toBeGreaterThan(0)
      for (const n of nodeData) {
        expect(Object.keys(n).sort()).toEqual(['id', 'itemStyle', 'name'])
        expect(Object.prototype.hasOwnProperty.call(n, 'depth')).toBe(false)
      }
    })

    // ⚠ THE TRAP: because the payload is now projected, ECharts hands the SAME projected object
    // back in click params (id/name/itemStyle only — no action, nodeKind, or categoryDepth).
    // handleClick must resolve the real node from sankeyData.nodes BY ID rather than trusting
    // `p.data`'s other fields, or a click would silently stop navigating and stop reporting a depth.
    it('resolves the clicked node by id from the projected payload, and still navigates with a real depth', () => {
      render(<CashFlowSankeyChart {...defaultProps} viewMode={{ kind: 'parents' }} />)
      const built = buildSankeyData(mockIncomeData.levels ?? [], mockExpenseData.levels ?? [], { kind: 'parents' })
      const parent = built.nodes.find(n => n.action.kind === 'focus')
      if (!parent) throw new Error('expected a focus node in the parents view')
      const action = parent.action as Extract<NodeAction, { kind: 'focus' }>

      const clickHandler = MockEChartsChart.mock.calls.at(-1)?.[0]?.onEvents?.click as
        ((params: unknown) => void) | undefined
      if (!clickHandler) throw new Error('expected onEvents.click to be wired')

      // Simulate what real (unmocked) ECharts hands back: dataType 'node', data === the PROJECTED
      // payload only — exactly what the option memo now builds, never the internal SankeyNode.
      clickHandler({ dataType: 'node', data: { id: parent.id, name: parent.name, itemStyle: parent.itemStyle } })

      expect(focused).toEqual([action.key])
      expect(parent.categoryDepth).toBeGreaterThan(0)
      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.REPORT_SANKEY_CATEGORY_FOCUSED,
        { depth: parent.categoryDepth },
      )
    })
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

    // The depth must come from node.categoryDepth, NOT from splitting the node id on ':'. events.ts
    // says so in as many words, and the sibling navigate event obeys it; this one derived 'l1' from the id.
    // It happens to be right today because only expense nodes focus and their ids encode true
    // depth — but the income fan is flattened to `in:l1:*`, so the moment anything in that fan can
    // focus, the id would report a plausible-looking level that is simply wrong.
    it('focusing a category is tracked with the node DEPTH and nothing identifying', () => {
      render(<CashFlowSankeyChart {...defaultProps} viewMode={{ kind: 'parents' }} />)
      fireEvent.click(screen.getByText(/Total Income → Food And Drink/))

      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.REPORT_SANKEY_CATEGORY_FOCUSED,
        { depth: 1 },
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

    // ── #1626 Phase 2: the Uncategorized node navigates ────────────────────────────────
    describe('the uncategorized node', () => {
      const withUncategorized = hierarchy(
        [node('FOOD_AND_DRINK', 'Food And Drink', 300), node('UNCATEGORIZED', 'Uncategorized', 120)],
        'expense',
      )

      it('navigates by the dedicated flag, never by a category value', () => {
        render(<CashFlowSankeyChart {...defaultProps} expenseData={withUncategorized} />)
        fireEvent.click(screen.getByText(/Total Income → Uncategorized/))

        const params = new URLSearchParams(currentUrl().split('?')[1] ?? '')
        expect(params.get('uncategorized')).toBe('true')
        // Sending the synthesised sentinel as a category value would look like a working filter
        // and match zero rows — the failure this node was inert to avoid.
        expect(params.get('pfc_primaries')).toBeNull()
        expect(params.get('pfc_detaileds')).toBeNull()
        expect(params.get('custom_category_ids')).toBeNull()
      })

      // The Sankey aggregates POSTED spend; the transactions list shows pending rows too. Without
      // this the node's amount and the list's total would differ by exactly the pending rows, and
      // the whole point of Phase 2 is that those two numbers agree.
      it('pins pending=false, so the list totals match the amount the node showed', () => {
        render(<CashFlowSankeyChart {...defaultProps} expenseData={withUncategorized} />)
        fireEvent.click(screen.getByText(/Total Income → Uncategorized/))

        expect(new URLSearchParams(currentUrl().split('?')[1] ?? '').get('pending')).toBe('false')
      })

      // Two independent activation paths reach the same node: ECharts' onClick (which the chart
      // mock surfaces as a hidden echarts-point-N button, carrying dataIndex ONLY) and the sr-only
      // table's own button. A keyboard or screen-reader user landing on a DIFFERENT filtered list
      // than a mouse user is exactly the kind of divergence that survives review because nobody
      // exercises both paths in one test.
      it('produces the same URL from the ECharts click and the screen-reader button', () => {
        const { unmount } = render(<CashFlowSankeyChart {...defaultProps} expenseData={withUncategorized} />)
        fireEvent.click(screen.getByText(/Total Income → Uncategorized/))
        const fromTable = currentUrl()
        expect(fromTable).toContain('uncategorized=true')
        unmount()

        resetLocationProbe()
        render(<CashFlowSankeyChart {...defaultProps} expenseData={withUncategorized} />)
        // Find the node's index the same way ECharts would: its position in the emitted node list.
        const idx = nodeIndexOf(withUncategorized, 'out:l1:UNCATEGORIZED')
        fireEvent.click(screen.getByTestId(`echarts-point-${idx}`))

        expect(currentUrl()).toBe(fromTable)
      })

      it('is tracked with structure only — depth and direction, never a category', () => {
        render(<CashFlowSankeyChart {...defaultProps} expenseData={withUncategorized} />)
        fireEvent.click(screen.getByText(/Total Income → Uncategorized/))

        expect(mockTrackEvent).toHaveBeenCalledWith(
          ANALYTICS_EVENTS.REPORT_SANKEY_NODE_NAVIGATED,
          { depth: 1, direction: 'expense' },
        )
      })
    })

    // The income fan is FLATTENED — every income node's id is `in:l1:*` whatever its real depth —
    // so an event that read depth from the id would report all income navigations as level 1. A
    // plausible wrong number is worse than none, because nothing downstream can tell.
    it('reports an income leaf its REAL depth, not the level its id implies', () => {
      const deepIncome = hierarchy(
        [node('INCOME', 'Income', 5000, 0, [node('INCOME_WAGES', 'Wages', 5000)])],
        'income',
      )
      render(<CashFlowSankeyChart {...defaultProps} incomeData={deepIncome} />)
      fireEvent.click(screen.getByText(/Wages → Total Income/))

      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.REPORT_SANKEY_NODE_NAVIGATED,
        { depth: 2, direction: 'income' },
      )
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
