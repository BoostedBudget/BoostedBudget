/**
 * BalanceHistoryChart Component Tests
 *
 * Tests the pure Plotly visualization component for balance history.
 * No title - WidgetWrapper provides it.
 *
 * Issue #418: Fresh implementation from scratch
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BalanceHistoryChart } from './BalanceHistoryChart';
import type { BalanceHistory } from '../types';

// Mock the shared charts module
vi.mock('@/shared/charts', () => ({
  Plot: vi.fn(({ data, config, style, useResizeHandler }) => (
    <div
      data-testid="plotly-chart"
      data-trace-count={data.length}
      data-trace-names={JSON.stringify(data.map((d: { name: string }) => d.name))}
      data-use-resize-handler={useResizeHandler}
      data-responsive={config?.responsive}
      style={style}
    >
      Plotly Chart Mock
    </div>
  )),
  useDarkMode: vi.fn(() => false),
  getBaseLayout: vi.fn(() => ({
    autosize: true,
    margin: { t: 10, r: 10, b: 40, l: 60 },
    xaxis: { gridcolor: '#e5e7eb' },
    yaxis: { gridcolor: '#e5e7eb' },
    legend: { font: {} },
  })),
  DEFAULT_CONFIG: {
    responsive: true,
    displayModeBar: 'hover',
    scrollZoom: false,
    modeBarButtonsToRemove: ['lasso2d', 'select2d'],
    displaylogo: false,
  },
  ACCOUNT_TYPE_COLORS: {
    balance: '#3b82f6',
    depository: '#10b981',
    credit: '#ef4444',
    investment: '#8b5cf6',
    loan: '#f59e0b',
  },
  formatCurrency: vi.fn((value: number) => `$${value.toLocaleString()}`),
}));

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
    vi.clearAllMocks();
  });

  describe('Basic Rendering', () => {
    it('should render chart with balance history data', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      expect(screen.getByTestId('plotly-chart')).toBeInTheDocument();
    });

    it('should NOT render a title (WidgetWrapper provides it)', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      expect(screen.queryByRole('heading')).not.toBeInTheDocument();
    });

    it('should handle empty snapshots array gracefully', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />);
      expect(
        screen.getByText('No balance history available')
      ).toBeInTheDocument();
    });

    it('should show "No balance history available" message when no data', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />);
      const emptyMessage = screen.getByText('No balance history available');
      expect(emptyMessage).toBeInTheDocument();
    });
  });

  describe('Plotly Chart Rendering', () => {
    it('should render Plotly chart component', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toBeInTheDocument();
    });

    it('should pass responsive config to Plot', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-responsive')).toBe('true');
    });

    it('should enable resize handler', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-use-resize-handler')).toBe('true');
    });

    it('should use 100% width and height styling', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toHaveStyle({ width: '100%', height: '100%' });
    });
  });

  describe('Total Balance Mode (showTypeBreakdown=false)', () => {
    it('should display single trace for total balance', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />
      );
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-trace-count')).toBe('1');
    });

    it('should name the trace "Total Balance"', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />
      );
      const chart = screen.getByTestId('plotly-chart');
      const traceNames = JSON.parse(
        chart.getAttribute('data-trace-names') || '[]'
      );
      expect(traceNames).toContain('Total Balance');
    });
  });

  describe('Type Breakdown Mode (showTypeBreakdown=true)', () => {
    it('should display multiple traces when showTypeBreakdown=true', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      );
      const chart = screen.getByTestId('plotly-chart');
      // Should have 3 traces: depository, credit, investment
      expect(chart.getAttribute('data-trace-count')).toBe('3');
    });

    it('should capitalize type names for trace names', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      );
      const chart = screen.getByTestId('plotly-chart');
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

  describe('Prop Variations', () => {
    it('should default showTypeBreakdown to false when not provided', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      // Default is single trace for total balance
      expect(chart.getAttribute('data-trace-count')).toBe('1');
    });

    it('should respect showTypeBreakdown=false prop', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />
      );
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-trace-count')).toBe('1');
    });

    it('should respect showTypeBreakdown=true prop', () => {
      render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      );
      const chart = screen.getByTestId('plotly-chart');
      expect(chart.getAttribute('data-trace-count')).toBe('3');
    });
  });

  describe('Height Chain', () => {
    it('should have h-full on outer container for height propagation', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} />
      );
      const outerDiv = container.firstChild as HTMLElement;
      expect(outerDiv).toHaveClass('h-full');
    });

    it('should NOT have redundant styling (bg-white, shadow, p-6, rounded-lg)', () => {
      // These styles are provided by WidgetWrapper, not the chart component
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} />
      );
      const outerDiv = container.firstChild as HTMLElement;
      expect(outerDiv).not.toHaveClass('bg-white');
      expect(outerDiv).not.toHaveClass('shadow');
      expect(outerDiv).not.toHaveClass('p-6');
      expect(outerDiv).not.toHaveClass('rounded-lg');
    });
  });

  describe('Layout and Styling', () => {
    it('should use responsive height (100%) instead of fixed pixels', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toHaveStyle({ height: '100%' });
    });

    it('should have minHeight of 200px', () => {
      render(<BalanceHistoryChart history={mockHistory} />);
      const chart = screen.getByTestId('plotly-chart');
      expect(chart).toHaveStyle({ minHeight: '200px' });
    });
  });

  describe('Empty State Display', () => {
    it('should show empty state when snapshots.length === 0', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />);
      expect(
        screen.getByText('No balance history available')
      ).toBeInTheDocument();
    });

    it('should center empty state message', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockEmptyHistory} />
      );
      const emptyState = container.querySelector(
        '.flex.items-center.justify-center'
      );
      expect(emptyState).toBeInTheDocument();
    });

    it('should style empty state text as gray with dark mode support', () => {
      const { container } = render(
        <BalanceHistoryChart history={mockEmptyHistory} />
      );
      const emptyText = container.querySelector('.text-gray-500');
      expect(emptyText).toBeInTheDocument();
      expect(emptyText).toHaveTextContent('No balance history available');
    });

    it('should not render Plotly chart when empty', () => {
      render(<BalanceHistoryChart history={mockEmptyHistory} />);
      expect(screen.queryByTestId('plotly-chart')).not.toBeInTheDocument();
    });
  });
});
