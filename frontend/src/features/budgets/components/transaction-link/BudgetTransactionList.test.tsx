/**
 * BudgetTransactionList Component Tests
 * Issue #499: Transaction-Budget Linking UI
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BrowserRouter } from 'react-router';
import { BudgetTransactionList } from './BudgetTransactionList';
import * as useBudgetTransactionsModule from '../../hooks/useBudgetTransactions';
import type { Transaction } from '../../../transactions/types';

// Mock the hook
vi.mock('../../hooks/useBudgetTransactions', () => ({
  useBudgetTransactions: vi.fn(),
}));

const mockUseBudgetTransactions = vi.mocked(useBudgetTransactionsModule.useBudgetTransactions);

// Test data
const mockTransaction: Transaction = {
  id: 'tx-1',
  user_id: 'user-1',
  account_id: 'account-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: -50.0,
  date: '2024-01-15T00:00:00Z',
  name: 'Coffee Shop',
  merchant_name: 'Starbucks',
  category: '["Food and Drink", "Coffee Shop"]',
  pending: false,
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
};

const mockTransaction2: Transaction = {
  ...mockTransaction,
  id: 'tx-2',
  amount: -25.0,
  name: 'Lunch',
  merchant_name: 'Chipotle',
};

// Helper to wrap component with router
const renderWithRouter = (component: React.ReactElement) => {
  return render(<BrowserRouter>{component}</BrowserRouter>);
};

describe('BudgetTransactionList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('loading state', () => {
    it('should show loading spinner when loading', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [],
        totalCount: 0,
        totalAmount: 0,
        loading: true,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByRole('status')).toBeInTheDocument();
      expect(screen.getByText(/loading/i)).toBeInTheDocument();
    });
  });

  describe('empty state', () => {
    it('should show empty message when no transactions', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [],
        totalCount: 0,
        totalAmount: 0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByText(/no transactions/i)).toBeInTheDocument();
    });
  });

  describe('error state', () => {
    it('should show error message when error occurs', () => {
      const mockRefetch = vi.fn();
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [],
        totalCount: 0,
        totalAmount: 0,
        loading: false,
        loadingMore: false,
        error: new Error('Failed to load'),
        hasMore: false,
        loadMore: vi.fn(),
        refetch: mockRefetch,
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByText(/failed to load/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
    });

    it('should call refetch when retry button is clicked', () => {
      const mockRefetch = vi.fn();
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [],
        totalCount: 0,
        totalAmount: 0,
        loading: false,
        loadingMore: false,
        error: new Error('Failed to load'),
        hasMore: false,
        loadMore: vi.fn(),
        refetch: mockRefetch,
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      fireEvent.click(screen.getByRole('button', { name: /retry/i }));
      expect(mockRefetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('transaction list', () => {
    it('should render transaction list with correct count and total', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction, mockTransaction2],
        totalCount: 2,
        totalAmount: 75.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      // Header should show count
      expect(screen.getByText(/transactions \(2\)/i)).toBeInTheDocument();
      // Header should show total amount
      expect(screen.getByText(/\$75\.00/)).toBeInTheDocument();
      // Transactions should be rendered (TransactionCard displays merchant_name)
      expect(screen.getByText('Starbucks')).toBeInTheDocument();
      expect(screen.getByText('Chipotle')).toBeInTheDocument();
    });

    it('should render transactions in a list with proper role', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 1,
        totalAmount: 50.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByRole('list')).toBeInTheDocument();
    });
  });

  describe('pagination', () => {
    it('should show Load More button when hasMore is true', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 10,
        totalAmount: 500.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: true,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByRole('button', { name: /load more/i })).toBeInTheDocument();
    });

    it('should not show Load More button when hasMore is false', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 1,
        totalAmount: 50.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.queryByRole('button', { name: /load more/i })).not.toBeInTheDocument();
    });

    it('should call loadMore when Load More button is clicked', () => {
      const mockLoadMore = vi.fn();
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 10,
        totalAmount: 500.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: true,
        loadMore: mockLoadMore,
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      fireEvent.click(screen.getByRole('button', { name: /load more/i }));
      expect(mockLoadMore).toHaveBeenCalledTimes(1);
    });

    it('should show loading indicator on Load More button when loadingMore', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 10,
        totalAmount: 500.0,
        loading: false,
        loadingMore: true,
        error: null,
        hasMore: true,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      const loadMoreButton = screen.getByRole('button', { name: /loading/i });
      expect(loadMoreButton).toBeInTheDocument();
      expect(loadMoreButton).toBeDisabled();
    });
  });

  describe('hook integration', () => {
    it('should pass budgetId and categoryId to hook', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [],
        totalCount: 0,
        totalAmount: 0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="my-budget" categoryId="my-category" />
      );

      expect(mockUseBudgetTransactions).toHaveBeenCalledWith('my-budget', 'my-category');
    });
  });

  describe('accessibility', () => {
    it('should have accessible heading', () => {
      mockUseBudgetTransactions.mockReturnValue({
        transactions: [mockTransaction],
        totalCount: 1,
        totalAmount: 50.0,
        loading: false,
        loadingMore: false,
        error: null,
        hasMore: false,
        loadMore: vi.fn(),
        refetch: vi.fn(),
      });

      renderWithRouter(
        <BudgetTransactionList budgetId="budget-1" categoryId="category-1" />
      );

      expect(screen.getByRole('heading', { level: 3 })).toBeInTheDocument();
    });
  });
});
