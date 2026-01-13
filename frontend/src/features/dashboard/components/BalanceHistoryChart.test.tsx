import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { BalanceHistory } from '../types'

// Issue #575: Use centralized @/shared/charts mock to prevent pollution
import '@/test/mocks/shared-charts'
import {
  MockPlot as mockPlot,
  mockUseDarkMode,
  mockGetBaseLayout,
  mockFormatCurrency,
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
    mockPlot.mockClear()
    mockUseDarkMode.mockClear()
    mockGetBaseLayout.mockClear()
    mockFormatCurrency.mockClear()
  })

  describe('Rendering', () => {
    it('should render chart with balance history data', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should not render a title (WidgetWrapper provides it)', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      expect(screen.queryByRole('heading')).not.toBeInTheDocument();
    });

    it('should show empty state when no data available', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />);
      expect(
        screen.getByText('No balance history available')
      ).toBeInTheDocument();
    });
  });

  describe('Plotly Chart Configuration', () => {
    it('should render with responsive config and resize handler', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toBeInTheDocument();
      expect(chart.getAttribute('data-responsive')).toBe('true');
      expect(chart.getAttribute('data-use-resize-handler')).toBe('true');
      expect(chart).toHaveStyle({ width: '100%', height: '100%' });
    });
  });

  describe('showTypeBreakdown Prop', () => {
    it('should default to total balance (single trace)', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-trace-count')).toBe('1');
    });

    it('should display type breakdown with multiple traces when enabled', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      );
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-trace-count')).toBe('3');
      const traceNames = JSON.parse(
        chart.getAttribute('data-trace-names') || '[]'
      );
      expect(traceNames).toContain('Depository');
      expect(traceNames).toContain('Credit');
      expect(traceNames).toContain('Investment');
    });
  });

  describe('Edge Cases', () => {
    it('should handle single snapshot', () => {
      const singleSnapshot: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-01',
        granularity: 'daily',
        snapshots: [{ date: '2025-11-01', balance: 10000.0 }],
      };

      render(<BalanceHistoryChart history={singleSnapshot} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should handle missing by_type data', () => {
      render(<BalanceHistoryChart history={mockHistoryWithoutByType} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should handle mixed data (some snapshots with by_type, some without)', () => {
      const mixedHistory: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-03',
        granularity: 'daily',
        snapshots: [
          {
            date: '2025-11-01',
            balance: 10000.0,
            by_type: { depository: 7000.0 },
          },
          {
            date: '2025-11-02',
            balance: 10500.0,
          },
          {
            date: '2025-11-03',
            balance: 9800.0,
            by_type: { depository: 6800.0 },
          },
        ],
      };

      render(
        <BalanceHistoryChart history={mixedHistory} showTypeBreakdown={true} />
      );
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should handle large balance amounts', () => {
      const largeBalances: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 1000000.0 },
          { date: '2025-11-02', balance: 2500000.5 },
        ],
      };

      render(<BalanceHistoryChart history={largeBalances} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should handle negative balances', () => {
      const negativeBalances: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: -5000.0 },
          { date: '2025-11-02', balance: -3000.0 },
        ],
      };

      render(<BalanceHistoryChart history={negativeBalances} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should handle zero balance', () => {
      const zeroBalance: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 0.0 },
          { date: '2025-11-02', balance: 100.0 },
        ],
      };

      render(<BalanceHistoryChart history={zeroBalance} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });
  });
  describe('Styling', () => {
    it('should apply h-full height propagation class', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} />
      );
      expect(container.firstChild).toHaveClass('h-full');
    });

    it('should use responsive height and min-height', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toHaveStyle({ height: '100%', minHeight: '200px' });
    });

    it('should not include padding/bg/shadow (delegated to WidgetWrapper)', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} />
      );
      const outerDiv = container.firstChild as HTMLElement;
      expect(outerDiv).not.toHaveClass('bg-white', 'shadow', 'p-6', 'rounded-lg');
    });
  });

  describe('Empty State', () => {
    it('should display centered empty message and no chart', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockEmptyHistory} />
      );
      expect(screen.getByText('No balance history available')).toBeInTheDocument();
      expect(screen.queryByTestId('plotly-chart')).not.toBeInTheDocument();
      expect(
        container.querySelector('.flex.items-center.justify-center')
      ).toBeInTheDocument();
    });
  });
});
