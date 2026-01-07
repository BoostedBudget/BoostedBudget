/**
 * Tests the NetWorthWidget component including loading,
 * error, success states, and sparkline toggle functionality.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// Mock dependencies before importing the component
vi.mock('../context/DashboardContext', () => ({
  useDashboardContext: vi.fn(),
}));

vi.mock('../hooks/useNetWorthData', () => ({
  useNetWorthData: vi.fn(),
}));

vi.mock('./registry', () => ({
  registerWidget: vi.fn(),
}));

import { NetWorthWidget } from './NetWorthWidget';
import { useDashboardContext } from '../context/DashboardContext';
import { useNetWorthData } from '../hooks/useNetWorthData';
import type { NetWorthSummary, NetWorthHistory } from '../types';

describe('NetWorthWidget', () => {
  const mockFilters = {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  };

  const mockSummary: NetWorthSummary = {
    as_of_date: '2024-01-31',
    net_worth: 50000,
    total_assets: 75000,
    total_liabilities: 25000,
    by_category: {
      assets: {
        category: 'assets',
        total: 75000,
        account_count: 3,
        by_type: {},
      },
      liabilities: {
        category: 'liabilities',
        total: 25000,
        account_count: 2,
        by_type: {},
      },
    },
    period_comparison: {
      previous_net_worth: 45000,
      previous_assets: 70000,
      previous_liabilities: 25000,
      net_worth_change: 5000,
      net_worth_change_percent: 11.11,
    },
  };

  const mockHistory: NetWorthHistory = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    granularity: 'daily',
    snapshots: [
      {
        date: '2024-01-01',
        net_worth: 45000,
        total_assets: 70000,
        total_liabilities: 25000,
      },
      {
        date: '2024-01-31',
        net_worth: 50000,
        total_assets: 75000,
        total_liabilities: 25000,
      },
    ],
  };

  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock for context
    vi.mocked(useDashboardContext).mockReturnValue({
      filters: mockFilters,
      setFilters: vi.fn(),
      timeframe: 'month',
      setTimeframe: vi.fn(),
      isEditing: false,
      setIsEditing: vi.fn(),
    });
  });

  describe('Loading State', () => {
    it('should show loading spinner when loading', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: null,
        history: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      // Check for spinner animation class
      const spinner = document.querySelector('.animate-spin');
      expect(spinner).toBeInTheDocument();
    });
  });

  describe('Error State', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: null,
        history: null,
        loading: false,
        error: new Error('Network error'),
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      expect(screen.getByText('Network error')).toBeInTheDocument();
    });

    it('should show error text in red', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: null,
        history: null,
        loading: false,
        error: new Error('API Error'),
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      const errorText = screen.getByText('API Error');
      expect(errorText).toHaveClass('text-red-500');
    });
  });

  describe('Empty State', () => {
    it('should show no data message when summary is null', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: null,
        history: null,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      expect(screen.getByText('No data available')).toBeInTheDocument();
    });
  });

  describe('Success State', () => {
    it('should render net worth value', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      // Check net worth is displayed (formatted)
      expect(screen.getByText(/\$50,000/)).toBeInTheDocument();
    });

    it('should display positive net worth with green color', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      const netWorthElement = screen.getByText(/\$50,000/);
      expect(netWorthElement).toHaveClass('text-green-600');
    });

    it('should display negative net worth with red color', () => {
      const negativeNetWorthSummary: NetWorthSummary = {
        ...mockSummary,
        net_worth: -10000,
        total_assets: 15000,
        total_liabilities: 25000,
      };

      vi.mocked(useNetWorthData).mockReturnValue({
        summary: negativeNetWorthSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      const netWorthElement = screen.getByText(/-\$10,000/);
      expect(netWorthElement).toHaveClass('text-red-600');
    });

    it('should display assets and liabilities breakdown', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      expect(screen.getByText('Assets')).toBeInTheDocument();
      expect(screen.getByText('Liabilities')).toBeInTheDocument();
      expect(screen.getByText(/\$75,000/)).toBeInTheDocument();
      expect(screen.getByText(/\$25,000/)).toBeInTheDocument();
    });

    it('should display period comparison when available', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      // Check for change display
      expect(screen.getByText(/\$5,000/)).toBeInTheDocument();
      expect(screen.getByText(/11\.11%/)).toBeInTheDocument();
    });
  });

  describe('Sparkline Toggle', () => {
    it('should render sparkline toggle button', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      expect(screen.getByText('Show Trend')).toBeInTheDocument();
    });

    it('should hide sparkline by default', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      // Sparkline container should not be present
      expect(screen.queryByTestId('net-worth-sparkline')).not.toBeInTheDocument();
    });

    it('should show sparkline when toggle is clicked', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      const toggleButton = screen.getByText('Show Trend');
      fireEvent.click(toggleButton);

      // Toggle text should change
      expect(screen.getByText('Hide Trend')).toBeInTheDocument();
      // Sparkline should now be visible
      expect(screen.getByTestId('net-worth-sparkline')).toBeInTheDocument();
    });

    it('should have user-friendly tooltip on toggle', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      const toggleButton = screen.getByText('Show Trend');
      expect(toggleButton.closest('button')).toHaveAttribute(
        'title',
        expect.stringContaining('trend')
      );
    });
  });

  describe('Context Integration', () => {
    it('should use filters from dashboard context', () => {
      vi.mocked(useNetWorthData).mockReturnValue({
        summary: mockSummary,
        history: mockHistory,
        loading: false,
        error: null,
        refetch: vi.fn(),
      });

      render(<NetWorthWidget />);

      expect(useNetWorthData).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: '2024-01-01',
          endDate: '2024-01-31',
        })
      );
    });
  });
});
