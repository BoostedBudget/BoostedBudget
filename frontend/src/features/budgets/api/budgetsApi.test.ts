import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import MockAdapter from 'axios-mock-adapter';
import { apiClient } from '../../../api/axiosConfig';
import {
  fetchBudgets,
  fetchCurrentBudget,
  fetchBudget,
  createBudget,
  updateBudget,
  deleteBudget,
  fetchAvailableCategories,
  addBudgetCategory,
  updateBudgetCategory,
  deleteBudgetCategory,
  fetchSavingsGoals,
  fetchActiveSavingsGoals,
  fetchSavingsGoal,
  createSavingsGoal,
  updateSavingsGoal,
  deleteSavingsGoal,
  // Issue #495: Envelope budgeting
  syncBudget,
  fetchBudgetTransactions,
} from './budgetsApi';
import type { Budget, BudgetCategory, SavingsGoal } from '../types';

describe('budgetsApi', () => {
  let mock: MockAdapter;

  const mockBudget: Budget = {
    id: 'budget-123',
    user_id: 'user-456',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    // Issue #495: Envelope budgeting fields
    income_amount: 0,
    income_sources: [],
  };

  const mockBudgetCategory: BudgetCategory = {
    id: 'cat-123',
    budget_id: 'budget-123',
    category: ['Food and Drink', 'Restaurants'],
    allocated_amount: 500,
    spent_amount: 150,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    // Issue #495: Envelope budgeting fields
    tags: [],
    match_logic: 'category_only',
  };

  const mockSavingsGoal: SavingsGoal = {
    id: 'goal-123',
    user_id: 'user-456',
    name: 'Vacation Fund',
    description: 'Save for summer vacation',
    target_amount: 5000,
    current_amount: 1500,
    currency_code: 'USD',
    account_id: 'account-789',
    deadline: '2025-07-01T00:00:00Z',
    is_completed: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  };

  beforeEach(() => {
    mock = new MockAdapter(apiClient);
  });

  afterEach(() => {
    mock.reset();
    vi.clearAllMocks();
  });

  // Budget CRUD Tests
  describe('fetchBudgets', () => {
    it('should call GET /v1/budgets and return budgets array', async () => {
      mock.onGet('/v1/budgets').reply(200, [mockBudget]);

      const result = await fetchBudgets();

      expect(result).toEqual([mockBudget]);
      expect(mock.history.get.length).toBe(1);
      expect(mock.history.get[0].url).toBe('/v1/budgets');
    });

    it('should return empty array when no budgets exist', async () => {
      mock.onGet('/v1/budgets').reply(200, []);

      const result = await fetchBudgets();

      expect(result).toEqual([]);
    });

    it('should handle API errors', async () => {
      mock.onGet('/v1/budgets').reply(500, { message: 'Internal server error' });

      await expect(fetchBudgets()).rejects.toThrow();
    });
  });

  describe('fetchCurrentBudget', () => {
    it('should call GET /v1/budgets/current and return current budget', async () => {
      mock.onGet('/v1/budgets/current').reply(200, mockBudget);

      const result = await fetchCurrentBudget();

      expect(result).toEqual(mockBudget);
      expect(mock.history.get[0].url).toBe('/v1/budgets/current');
    });

    it('should handle 404 when no current budget exists', async () => {
      mock.onGet('/v1/budgets/current').reply(404, { message: 'No current budget found' });

      await expect(fetchCurrentBudget()).rejects.toThrow();
    });
  });

  describe('fetchBudget', () => {
    it('should call GET /v1/budgets/:id and return specific budget', async () => {
      mock.onGet('/v1/budgets/budget-123').reply(200, mockBudget);

      const result = await fetchBudget('budget-123');

      expect(result).toEqual(mockBudget);
      expect(mock.history.get[0].url).toBe('/v1/budgets/budget-123');
    });

    it('should handle 404 when budget not found', async () => {
      mock.onGet('/v1/budgets/nonexistent').reply(404, { message: 'Budget not found' });

      await expect(fetchBudget('nonexistent')).rejects.toThrow();
    });
  });

  describe('createBudget', () => {
    it('should call POST /v1/budgets and return created budget', async () => {
      const createRequest = {
        name: 'Monthly Budget',
        period: 'monthly' as const,
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      };

      mock.onPost('/v1/budgets').reply(201, mockBudget);

      const result = await createBudget(createRequest);

      expect(result).toEqual(mockBudget);
      expect(mock.history.post.length).toBe(1);
      expect(mock.history.post[0].url).toBe('/v1/budgets');
      expect(JSON.parse(mock.history.post[0].data)).toEqual(createRequest);
    });

    it('should handle 409 conflict for duplicate name', async () => {
      const createRequest = {
        name: 'Duplicate Budget',
        period: 'monthly' as const,
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      };

      mock.onPost('/v1/budgets').reply(409, { message: 'A budget with this name already exists' });

      await expect(createBudget(createRequest)).rejects.toThrow();
    });
  });

  describe('updateBudget', () => {
    it('should call PUT /v1/budgets/:id and return updated budget', async () => {
      const updateRequest = { name: 'Updated Budget', amount: 2500 };
      const updatedBudget = { ...mockBudget, ...updateRequest };

      mock.onPut('/v1/budgets/budget-123').reply(200, updatedBudget);

      const result = await updateBudget('budget-123', updateRequest);

      expect(result).toEqual(updatedBudget);
      expect(mock.history.put[0].url).toBe('/v1/budgets/budget-123');
      expect(JSON.parse(mock.history.put[0].data)).toEqual(updateRequest);
    });
  });

  describe('deleteBudget', () => {
    it('should call DELETE /v1/budgets/:id', async () => {
      mock.onDelete('/v1/budgets/budget-123').reply(204);

      await expect(deleteBudget('budget-123')).resolves.toBeUndefined();
      expect(mock.history.delete[0].url).toBe('/v1/budgets/budget-123');
    });

    it('should handle 404 when budget not found', async () => {
      mock.onDelete('/v1/budgets/nonexistent').reply(404);

      await expect(deleteBudget('nonexistent')).rejects.toThrow();
    });
  });

  describe('fetchAvailableCategories', () => {
    it('should call GET /v1/budgets/categories/available and return categories', async () => {
      const categories = ['Food and Drink', 'Transportation', 'Shopping'];
      mock.onGet('/v1/budgets/categories/available').reply(200, categories);

      const result = await fetchAvailableCategories();

      expect(result).toEqual(categories);
    });
  });

  // Budget Category Tests
  describe('addBudgetCategory', () => {
    it('should call POST /v1/budgets/:id/categories', async () => {
      const createRequest = {
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
      };

      mock.onPost('/v1/budgets/budget-123/categories').reply(201, mockBudgetCategory);

      const result = await addBudgetCategory('budget-123', createRequest);

      expect(result).toEqual(mockBudgetCategory);
      expect(mock.history.post[0].url).toBe('/v1/budgets/budget-123/categories');
    });
  });

  describe('updateBudgetCategory', () => {
    it('should call PUT /v1/budgets/:id/categories/:categoryId', async () => {
      const updateRequest = { allocated_amount: 600 };
      const updatedCategory = { ...mockBudgetCategory, allocated_amount: 600 };

      mock.onPut('/v1/budgets/budget-123/categories/cat-123').reply(200, updatedCategory);

      const result = await updateBudgetCategory('budget-123', 'cat-123', updateRequest);

      expect(result).toEqual(updatedCategory);
    });
  });

  describe('deleteBudgetCategory', () => {
    it('should call DELETE /v1/budgets/:id/categories/:categoryId', async () => {
      mock.onDelete('/v1/budgets/budget-123/categories/cat-123').reply(204);

      await expect(deleteBudgetCategory('budget-123', 'cat-123')).resolves.toBeUndefined();
    });
  });

  // Savings Goal Tests
  describe('fetchSavingsGoals', () => {
    it('should call GET /v1/savings-goals and return goals array', async () => {
      mock.onGet('/v1/savings-goals').reply(200, [mockSavingsGoal]);

      const result = await fetchSavingsGoals();

      expect(result).toEqual([mockSavingsGoal]);
      expect(mock.history.get[0].url).toBe('/v1/savings-goals');
    });
  });

  describe('fetchActiveSavingsGoals', () => {
    it('should call GET /v1/savings-goals/active and return active goals', async () => {
      mock.onGet('/v1/savings-goals/active').reply(200, [mockSavingsGoal]);

      const result = await fetchActiveSavingsGoals();

      expect(result).toEqual([mockSavingsGoal]);
      expect(mock.history.get[0].url).toBe('/v1/savings-goals/active');
    });
  });

  describe('fetchSavingsGoal', () => {
    it('should call GET /v1/savings-goals/:id and return specific goal', async () => {
      mock.onGet('/v1/savings-goals/goal-123').reply(200, mockSavingsGoal);

      const result = await fetchSavingsGoal('goal-123');

      expect(result).toEqual(mockSavingsGoal);
    });
  });

  describe('createSavingsGoal', () => {
    it('should call POST /v1/savings-goals and return created goal', async () => {
      const createRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      };

      mock.onPost('/v1/savings-goals').reply(201, mockSavingsGoal);

      const result = await createSavingsGoal(createRequest);

      expect(result).toEqual(mockSavingsGoal);
      expect(mock.history.post[0].url).toBe('/v1/savings-goals');
    });
  });

  describe('updateSavingsGoal', () => {
    it('should call PUT /v1/savings-goals/:id and return updated goal', async () => {
      const updateRequest = { target_amount: 6000 };
      const updatedGoal = { ...mockSavingsGoal, target_amount: 6000 };

      mock.onPut('/v1/savings-goals/goal-123').reply(200, updatedGoal);

      const result = await updateSavingsGoal('goal-123', updateRequest);

      expect(result).toEqual(updatedGoal);
    });
  });

  describe('deleteSavingsGoal', () => {
    it('should call DELETE /v1/savings-goals/:id', async () => {
      mock.onDelete('/v1/savings-goals/goal-123').reply(204);

      await expect(deleteSavingsGoal('goal-123')).resolves.toBeUndefined();
    });
  });

  // =============================================================================
  // Issue #495: Envelope Budgeting - Sync and Transaction APIs
  // =============================================================================

  describe('syncBudget', () => {
    it('should call POST /v1/budgets/:id/sync and return updated budget', async () => {
      const syncedBudget = {
        ...mockBudget,
        categories: [
          {
            ...mockBudgetCategory,
            spent_amount: 350, // Updated spent amount after sync
          },
        ],
      };

      mock.onPost('/v1/budgets/budget-123/sync').reply(200, syncedBudget);

      const result = await syncBudget('budget-123');

      expect(result).toEqual(syncedBudget);
      expect(mock.history.post.length).toBe(1);
      expect(mock.history.post[0].url).toBe('/v1/budgets/budget-123/sync');
    });

    it('should handle 404 when budget not found', async () => {
      mock.onPost('/v1/budgets/nonexistent/sync').reply(404, { message: 'Budget not found' });

      await expect(syncBudget('nonexistent')).rejects.toThrow();
    });

    it('should handle API errors during sync', async () => {
      mock.onPost('/v1/budgets/budget-123/sync').reply(500, { message: 'Internal server error' });

      await expect(syncBudget('budget-123')).rejects.toThrow();
    });
  });

  describe('fetchBudgetTransactions', () => {
    const mockTransactionsResponse = {
      transactions: [
        {
          id: 'tx-1',
          account_id: 'acc-1',
          amount: -25.99,
          date: '2025-01-15T00:00:00Z',
          name: 'Restaurant ABC',
          category: '["Food and Drink", "Restaurants"]',
        },
        {
          id: 'tx-2',
          account_id: 'acc-1',
          amount: -12.50,
          date: '2025-01-16T00:00:00Z',
          name: 'Coffee Shop',
          category: '["Food and Drink", "Coffee Shops"]',
        },
      ],
      total_count: 2,
      has_more: false,
    };

    it('should call GET /v1/transactions with budget date range', async () => {
      mock.onGet('/v1/transactions').reply(200, mockTransactionsResponse);

      const result = await fetchBudgetTransactions('budget-123', {
        startDate: '2025-01-01',
        endDate: '2025-01-31',
      });

      expect(result).toEqual(mockTransactionsResponse);
      expect(mock.history.get.length).toBe(1);
      expect(mock.history.get[0].params).toEqual({
        start_date: '2025-01-01',
        end_date: '2025-01-31',
      });
    });

    it('should include optional limit and offset parameters', async () => {
      mock.onGet('/v1/transactions').reply(200, mockTransactionsResponse);

      await fetchBudgetTransactions('budget-123', {
        startDate: '2025-01-01',
        endDate: '2025-01-31',
        limit: 50,
        offset: 100,
      });

      expect(mock.history.get[0].params).toEqual({
        start_date: '2025-01-01',
        end_date: '2025-01-31',
        limit: 50,
        offset: 100,
      });
    });

    it('should work without optional parameters', async () => {
      mock.onGet('/v1/transactions').reply(200, mockTransactionsResponse);

      const result = await fetchBudgetTransactions('budget-123');

      expect(result).toEqual(mockTransactionsResponse);
      expect(mock.history.get[0].params).toEqual({});
    });

    it('should handle API errors', async () => {
      mock.onGet('/v1/transactions').reply(500, { message: 'Internal server error' });

      await expect(
        fetchBudgetTransactions('budget-123', {
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      ).rejects.toThrow();
    });

    it('should handle empty transactions response', async () => {
      const emptyResponse = {
        transactions: [],
        total_count: 0,
        has_more: false,
      };

      mock.onGet('/v1/transactions').reply(200, emptyResponse);

      const result = await fetchBudgetTransactions('budget-123', {
        startDate: '2025-01-01',
        endDate: '2025-01-31',
      });

      expect(result.transactions).toEqual([]);
      expect(result.total_count).toBe(0);
    });
  });
});
