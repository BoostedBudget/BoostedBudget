/**
 * BudgetDetailPage Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { BudgetDetailPage } from './BudgetDetailPage';
import * as budgetsApi from '../api/budgetsApi';
import * as tagsApi from '../../tags/api/tagsApi';
import type { Budget, BudgetCategory } from '../types';
import type { UserTag } from '../../tags/types';

// Mock the API modules (still needed for category operations)
vi.mock('../api/budgetsApi');
vi.mock('../../tags/api/tagsApi');

// Mock the budget hooks to avoid React Query provider issues
vi.mock('../hooks/useBudgetDetail');
vi.mock('../hooks/useAvailableCategories');

// Import the mocked hooks
import { useBudgetDetail } from '../hooks/useBudgetDetail';
import { useAvailableCategories } from '../hooks/useAvailableCategories';

// Mock the useTags hook to avoid React Query provider issues
vi.mock('../../tags/hooks/useTags', () => ({
  useTags: vi.fn().mockReturnValue({
    tags: [],
    loading: false,
    error: null,
    createTag: vi.fn(),
    updateTag: vi.fn(),
    deleteTag: vi.fn(),
    refetch: vi.fn(),
  }),
}));

// Mock BudgetCategoryManager to simplify testing
vi.mock('./category-management/BudgetCategoryManager', () => ({
  BudgetCategoryManager: ({
    categories,
    onAddCategory,
    onUpdateCategory,
    onDeleteCategory,
  }: {
    categories: BudgetCategory[];
    onAddCategory: () => Promise<BudgetCategory>;
    onUpdateCategory: (id: string) => Promise<BudgetCategory>;
    onDeleteCategory: (id: string) => Promise<void>;
  }) => (
    <div data-testid="budget-category-manager">
      <span data-testid="category-count">{categories.length} categories</span>
      <button onClick={() => onAddCategory()} data-testid="add-category-btn">
        Add Category
      </button>
      <button
        onClick={() => onUpdateCategory('cat-1')}
        data-testid="update-category-btn"
      >
        Update Category
      </button>
      <button
        onClick={() => onDeleteCategory('cat-1')}
        data-testid="delete-category-btn"
      >
        Delete Category
      </button>
    </div>
  ),
}));

// Helper to create QueryClient wrapper
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

/**
 * Helper to render with router context
 */
function renderWithRouter(budgetId = 'budget-1') {
  const Wrapper = createWrapper();
  return render(
    <Wrapper>
      <MemoryRouter initialEntries={[`/budgets/${budgetId}`]}>
        <Routes>
          <Route path="/budgets/:budgetId" element={<BudgetDetailPage />} />
          <Route path="/budgets" element={<div data-testid="budgets-page">Budgets Page</div>} />
        </Routes>
      </MemoryRouter>
    </Wrapper>
  );
}

describe('BudgetDetailPage', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2024-01-01T00:00:00Z',
    end_date: '2024-01-31T23:59:59Z',
    amount: 3000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 5000,
    income_sources: [],
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 250,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 150,
        tags: ['Essential'],
        match_logic: 'and',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
    ],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  };

  const mockTags: UserTag[] = [
    {
      id: 'tag-1',
      user_id: 'user-1',
      name: 'Essential',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    },
  ];

  const mockAvailableCategories = [
    'Food and Drink > Restaurants',
    'Food and Drink > Groceries',
    'Transportation > Gas',
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    // Configure default mocks for hooks
    vi.mocked(useBudgetDetail).mockReturnValue({
      budget: mockBudget,
      metrics: null,
      loading: false,
      error: null,
      refetch: vi.fn(),
    });
    vi.mocked(useAvailableCategories).mockReturnValue({
      categories: mockAvailableCategories.map(c => c.split(' > ')),
      loading: false,
      error: null,
      refetch: vi.fn(),
    });
    // Keep API mocks for category operations
    vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue(mockAvailableCategories);
    vi.mocked(tagsApi.fetchTags).mockResolvedValue(mockTags);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Loading State', () => {
    it('should show loading spinner while fetching budget', async () => {
      vi.mocked(useBudgetDetail).mockReturnValue({
        budget: null,
        metrics: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      });

      renderWithRouter();

      expect(screen.getByText(/loading/i)).toBeInTheDocument();
    });
  });

  describe('Error State', () => {
    it('should show error message on fetch failure', async () => {
      vi.mocked(useBudgetDetail).mockReturnValue({
        budget: null,
        metrics: null,
        loading: false,
        error: new Error('Network error'),
        refetch: vi.fn(),
      });

      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText(/error/i)).toBeInTheDocument();
      });
      expect(screen.getByText(/network error/i)).toBeInTheDocument();
    });

    it('should provide retry button on error', async () => {
      const mockRefetch = vi.fn();
      vi.mocked(useBudgetDetail)
        .mockReturnValueOnce({
          budget: null,
          metrics: null,
          loading: false,
          error: new Error('Network error'),
          refetch: mockRefetch,
        })
        .mockReturnValue({
          budget: mockBudget,
          metrics: null,
          loading: false,
          error: null,
          refetch: mockRefetch,
        });

      const { rerender } = renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText(/error/i)).toBeInTheDocument();
      });

      const retryButton = screen.getByRole('button', { name: /retry/i });
      await userEvent.click(retryButton);

      // Trigger a re-render to simulate the refetch
      rerender(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/budgets/budget-1']}>
            <Routes>
              <Route path="/budgets/:budgetId" element={<BudgetDetailPage />} />
              <Route path="/budgets" element={<div data-testid="budgets-page">Budgets Page</div>} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      );

      await waitFor(() => {
        expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
      });
    });
  });

  describe('Budget Display', () => {
    it('should render budget name', async () => {
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
      });
    });

    it('should render budget period label in meta section', async () => {
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText('Period')).toBeInTheDocument();
      });
    });

    it('should render back navigation link', async () => {
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByRole('link', { name: /back/i })).toBeInTheDocument();
      });
    });

    it('should navigate back to budgets page when back link clicked', async () => {
      const user = userEvent.setup();
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText('Monthly Budget')).toBeInTheDocument();
      });

      const backLink = screen.getByRole('link', { name: /back/i });
      await user.click(backLink);

      await waitFor(() => {
        expect(screen.getByTestId('budgets-page')).toBeInTheDocument();
      });
    });

    it('should render BudgetCategoryManager with categories', async () => {
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByTestId('budget-category-manager')).toBeInTheDocument();
      });
      expect(screen.getByTestId('category-count')).toHaveTextContent('2 categories');
    });
  });

  describe('Category CRUD Operations', () => {
    it('should add category and refetch budget', async () => {
      const user = userEvent.setup();
      const mockRefetch = vi.fn().mockResolvedValue({});
      const newCategory: BudgetCategory = {
        id: 'cat-3',
        budget_id: 'budget-1',
        category: ['Shopping', 'Clothing'],
        allocated_amount: 200,
        spent_amount: 0,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      };

      vi.mocked(budgetsApi.addBudgetCategory).mockResolvedValue(newCategory);
      vi.mocked(useBudgetDetail).mockReturnValue({
        budget: mockBudget,
        metrics: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      });

      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByTestId('add-category-btn')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('add-category-btn'));

      await waitFor(() => {
        expect(mockRefetch).toHaveBeenCalled();
      });
    });

    it('should update category and refetch budget', async () => {
      const user = userEvent.setup();
      const mockRefetch = vi.fn().mockResolvedValue({});
      const updatedCategory = {
        ...mockBudget.categories![0],
        allocated_amount: 600,
      };

      vi.mocked(budgetsApi.updateBudgetCategory).mockResolvedValue(updatedCategory);
      vi.mocked(useBudgetDetail).mockReturnValue({
        budget: mockBudget,
        metrics: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      });

      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByTestId('update-category-btn')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('update-category-btn'));

      await waitFor(() => {
        expect(mockRefetch).toHaveBeenCalled();
      });
    });

    it('should delete category and refetch budget', async () => {
      const user = userEvent.setup();
      const mockRefetch = vi.fn().mockResolvedValue({});

      vi.mocked(budgetsApi.deleteBudgetCategory).mockResolvedValue();
      vi.mocked(useBudgetDetail).mockReturnValue({
        budget: mockBudget,
        metrics: null,
        loading: false,
        error: null,
        refetch: mockRefetch,
      });

      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByTestId('delete-category-btn')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('delete-category-btn'));

      await waitFor(() => {
        expect(mockRefetch).toHaveBeenCalled();
      });
    });
  });

  describe('Budget Income', () => {
    it('should show income amount when set', async () => {
      renderWithRouter();

      await waitFor(() => {
        expect(screen.getByText(/\$5,000\.00/)).toBeInTheDocument();
      });
    });
  });
});
