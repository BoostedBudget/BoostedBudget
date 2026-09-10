import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { BalanceHistory } from '../types'

// Issue #575: Use centralized @/shared/charts mock to prevent pollution
import '@/test/mocks/shared-charts'
import {
  MockEChartsChart as mockEChartsChart,
  mockUseDarkMode,
} from '@/test/mocks/shared-charts'

import { BalanceHistoryChart } from './BalanceHistoryChart'

describe('BalanceHistoryChart', () => {
  const mockHistory: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-05',
    granularity: 'daily',
    snapshots: [
      {
        date: '2025-11-01',
        balance: 10000.0,
        by_type: {
          depository: 7000.0,
          credit: -2000.0,
          investment: 5000.0,
        },
      },
      {
        date: '2025-11-02',
        balance: 10500.5,
        by_type: {
          depository: 7200.0,
          credit: -1900.0,
          investment: 5200.5,
        },
      },
      {
        date: '2025-11-03',
        balance: 9800.0,
        by_type: {
          depository: 6800.0,
          credit: -2200.0,
          investment: 5200.0,
        },
      },
      {
        date: '2025-11-04',
        balance: 11000.0,
        by_type: {
          depository: 7500.0,
          credit: -1800.0,
          investment: 5300.0,
        },
      },
      {
        date: '2025-11-05',
        balance: 11200.25,
        by_type: {
          depository: 7600.0,
          credit: -1700.0,
          investment: 5300.25,
        },
      },
    ],
  };

  const mockEmptyHistory: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-05',
    granularity: 'daily',
    snapshots: [],
  };

  const mockHistoryWithoutByType: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-03',
    granularity: 'daily',
    snapshots: [
      { date: '2025-11-01', balance: 10000.0 },
      { date: '2025-11-02', balance: 10500.5 },
      { date: '2025-11-03', balance: 9800.0 },
    ],
  };

  beforeEach(() => {
    mockEChartsChart.mockClear()
    mockUseDarkMode.mockClear()
  })

  describe('Rendering', () => {
    it('should render chart with balance history data', () => {
      render(<BalanceHistoryChart history={mockHistory} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should not render a title (WidgetWrapper provides it)', () => {
      render(<BalanceHistoryChart history={mockHistory} />)
      expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    })

    it('should render chart even with empty snapshots (parent handles empty state)', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })
  })

  describe('ECharts Chart Configuration', () => {
    it('should render with SVG renderer', () => {
      render(<BalanceHistoryChart history={mockHistory} />)
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.dataset.renderer).toBe('svg')
      expect(chart).toHaveStyle({ width: '100%', height: '100%' })
    })
  })

  describe('showTypeBreakdown Prop', () => {
    it('should default to total balance (single series)', () => {
      render(<BalanceHistoryChart history={mockHistory} />)
      expect(screen.getByTestId('echarts-chart').dataset.seriesCount).toBe('1')
    })

    it('should display type breakdown with multiple series when enabled', () => {
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)
      const chart = screen.getByTestId('echarts-chart')
      expect(chart.dataset.seriesCount).toBe('3')
      const seriesNames = JSON.parse(chart.dataset.seriesNames ?? '[]') as string[]
      expect(seriesNames).toEqual(expect.arrayContaining(['Depository', 'Credit', 'Investment']))
    })
  })

  describe('Edge Cases', () => {
    it('should handle single snapshot', () => {
      render(<BalanceHistoryChart history={{
        start_date: '2025-11-01',
        end_date: '2025-11-01',
        granularity: 'daily',
        snapshots: [{ date: '2025-11-01', balance: 10000.0 }],
      }} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should handle missing by_type data', () => {
      render(<BalanceHistoryChart history={mockHistoryWithoutByType} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should handle mixed data (some snapshots with by_type, some without)', () => {
      render(<BalanceHistoryChart history={{
        start_date: '2025-11-01',
        end_date: '2025-11-03',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 10000.0, by_type: { depository: 7000.0 } },
          { date: '2025-11-02', balance: 10500.0 },
          { date: '2025-11-03', balance: 9800.0, by_type: { depository: 6800.0 } },
        ],
      }} showTypeBreakdown={true} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should handle large balance amounts', () => {
      render(<BalanceHistoryChart history={{
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 1000000.0 },
          { date: '2025-11-02', balance: 2500000.5 },
        ],
      }} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should handle negative balances', () => {
      render(<BalanceHistoryChart history={{
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: -5000.0 },
          { date: '2025-11-02', balance: -3000.0 },
        ],
      }} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })

    it('should handle zero balance', () => {
      render(<BalanceHistoryChart history={{
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 0.0 },
          { date: '2025-11-02', balance: 100.0 },
        ],
      }} />)
      expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
    })
  })

  describe('Styling', () => {
    it('should apply h-full and responsive dimensions without wrapper styling', () => {
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)
      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('h-full')
      const chart = screen.getByTestId('echarts-chart')
      expect(chart).toHaveStyle({ height: '100%', minHeight: '200px' })
      expect(outerDiv).not.toHaveClass('bg-white', 'shadow', 'p-6', 'rounded-lg')
    })
  })

  describe('Dark Mode', () => {
    it('reflects dark mode state in isDark prop', () => {
      mockUseDarkMode.mockReturnValue(true)
      const { unmount } = render(<BalanceHistoryChart history={mockHistory} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
      unmount()

      mockUseDarkMode.mockReturnValue(false)
      render(<BalanceHistoryChart history={mockHistory} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })

  // ─── #1626 Phase 3, 1d/1e ──────────────────────────────────────────────────────────────
  describe('missing account types are gaps, not zeroes', () => {
    const mixed: BalanceHistory = {
      start_date: '2025-11-01',
      end_date: '2025-11-03',
      granularity: 'daily',
      snapshots: [
        { date: '2025-11-01', balance: 10000, by_type: { depository: 7000, credit: -2000 } },
        { date: '2025-11-02', balance: 10500, by_type: { depository: 7500 } },
        { date: '2025-11-03', balance: 9800, by_type: { depository: 6800, credit: -1800 } },
      ],
    }

    const seriesOf = (name: string) => {
      const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        series?: Array<{
          name: string
          data: Array<number | null>
          smooth?: boolean
          smoothMonotone?: string
          connectNulls?: boolean
        }>
      }
      const found = (option.series ?? []).find((sr) => sr.name === name)
      if (!found) throw new Error(`no series named ${name}`)
      return found
    }

    it('renders a day with no entry for that type as null, not 0', () => {
      render(<BalanceHistoryChart history={mixed} showTypeBreakdown={true} />)
      // A credit balance of exactly 0 and "this account type had no balance recorded" are
      // different facts. Rendering the second as the first is the same anti-pattern as the
      // zero-filled future buckets this phase removed, at smaller scale.
      expect(seriesOf('Credit').data).toEqual([-2000, null, -1800])
      expect(seriesOf('Depository').data).toEqual([7000, 7500, 6800])
    })

    it('leaves a gap in the line rather than connecting across it', () => {
      render(<BalanceHistoryChart history={mixed} showTypeBreakdown={true} />)
      expect(seriesOf('Credit').connectNulls).not.toBe(true)
    })

    it('omits the gap from the tooltip instead of printing a zero', () => {
      render(<BalanceHistoryChart history={mixed} showTypeBreakdown={true} />)
      const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        tooltip?: { formatter?: (p: unknown) => string }
      }
      const formatted = option.tooltip!.formatter!([
        { seriesName: 'Depository', value: 7500, dataIndex: 1 },
        { seriesName: 'Credit', value: null, dataIndex: 1 },
      ])
      expect(formatted).toContain('Depository')
      expect(formatted).not.toContain('Credit')
      expect(formatted).not.toContain('NaN')
    })
  })

  describe('line smoothing', () => {
    const seriesList = () => {
      const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        series?: Array<{ smooth?: boolean; smoothMonotone?: string }>
      }
      return option.series ?? []
    }

    // smoothMonotone is a MODIFIER of smooth, not a replacement: with smooth false it does
    // nothing at all, so both have to be present. Smoothing is what turned a one-step plunge
    // into an exponential-decay curve and made this bug look like a slow bleed rather than a
    // cliff; monotone-x stops the curve overshooting between points without flattening a
    // genuine step.
    it('keeps smoothing on and constrains it to monotone-x (total balance)', () => {
      render(<BalanceHistoryChart history={mockHistory} />)
      for (const sr of seriesList()) {
        expect(sr.smooth).toBe(true)
        expect(sr.smoothMonotone).toBe('x')
      }
    })

    it('keeps smoothing on and constrains it to monotone-x (type breakdown)', () => {
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)
      expect(seriesList()).toHaveLength(3)
      for (const sr of seriesList()) {
        expect(sr.smooth).toBe(true)
        expect(sr.smoothMonotone).toBe('x')
      }
    })
  })

  it('tooltip formatter escapes malicious series names (XSS regression — #1169)', () => {
    render(<BalanceHistoryChart history={mockHistory} />)
    const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option.tooltip!.formatter!([{ seriesName: '<img src=x onerror=alert(1)>', value: 100, dataIndex: 0 }])
    expect(formatted).not.toMatch(/<(img|script|svg)/i)
    expect(formatted).toContain('&lt;img')
  })

  // Bug C regression — Issue #1173: midnight-UTC RFC3339 dates must not shift back one
  // day in negative-offset timezones. Old code used format(parseISO()), new code uses
  // formatHistoryLabel() which anchors to noon UTC.
  describe('TZ-safe date labels (Bug C — Issue #1173)', () => {
    it('x-axis labels use UTC calendar date for midnight-UTC RFC3339 snapshots', () => {
      const aprilHistory: BalanceHistory = {
        start_date: '2024-04-28T00:00:00Z',
        end_date: '2024-04-29T00:00:00Z',
        granularity: 'daily',
        snapshots: [
          { date: '2024-04-28T00:00:00Z', balance: 4000 },
          { date: '2024-04-29T00:00:00Z', balance: 4100 },
        ],
      }
      render(<BalanceHistoryChart history={aprilHistory} />)
      const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        xAxis?: { data?: string[] }
      }
      const labels = option.xAxis?.data ?? []
      expect(labels).toContain('Apr 28')
      expect(labels).toContain('Apr 29')
      expect(labels).not.toContain('Apr 27') // Old code would have shifted back one day
    })

    it('tooltip header uses UTC calendar date for midnight-UTC RFC3339 snapshots', () => {
      const aprilHistory: BalanceHistory = {
        start_date: '2024-04-29T00:00:00Z',
        end_date: '2024-04-29T00:00:00Z',
        granularity: 'daily',
        snapshots: [
          { date: '2024-04-29T00:00:00Z', balance: 4100 },
          { date: '2024-04-30T00:00:00Z', balance: 4200 },
        ],
      }
      render(<BalanceHistoryChart history={aprilHistory} />)
      const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
        tooltip?: { formatter?: (p: unknown) => string }
      }
      const formatted = option.tooltip!.formatter!([{ seriesName: 'Total Balance', value: 4100, dataIndex: 0 }])
      expect(formatted).toContain('April 29, 2024')
      expect(formatted).not.toContain('April 28, 2024') // Old code would have shifted back one day
    })
  })
});
