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

  it('tooltip formatter escapes malicious series names (XSS regression — #1169)', () => {
    render(<BalanceHistoryChart history={mockHistory} />)
    const option = mockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option.tooltip!.formatter!([{ seriesName: '<img src=x onerror=alert(1)>', value: 100, dataIndex: 0 }])
    expect(formatted).not.toMatch(/<(img|script|svg)/i)
    expect(formatted).toContain('&lt;img')
  })
});
