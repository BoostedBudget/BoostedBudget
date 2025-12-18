import { apiClient } from '../../../api/axiosConfig';
import type {
  Budget,
  BudgetCategory,
  SavingsGoal,
  CreateBudgetRequest,
  UpdateBudgetRequest,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
  CreateSavingsGoalRequest,
  UpdateSavingsGoalRequest,
  CategoryTransactionsResponse,
} from '../types';
import type { MatchLogic } from '@/shared/types/matching';

// Helper to convert YYYY-MM-DD date strings to RFC3339 format for Go backend
// If already in RFC3339 format (contains 'T'), returns as-is
function toRFC3339(date: string | undefined): string | undefined {
  if (!date) return undefined;
  // If already in RFC3339 format, return as-is
  if (date.includes('T')) return date;
  // Otherwise, convert YYYY-MM-DD to RFC3339
  return `${date}T00:00:00Z`;
}

// Budget CRUD operations
export async function fetchBudgets(): Promise<Budget[]> {
  const response = await apiClient.get<Budget[]>('/v1/budgets');
  return response.data;
}

export async function fetchCurrentBudget(): Promise<Budget> {
  const response = await apiClient.get<Budget>('/v1/budgets/current');
  return response.data;
}

export async function fetchBudget(id: string): Promise<Budget> {
  const response = await apiClient.get<Budget>(`/v1/budgets/${id}`);
  return response.data;
}

export async function createBudget(req: CreateBudgetRequest): Promise<Budget> {
  const payload = {
    ...req,
    start_date: toRFC3339(req.start_date),
    end_date: toRFC3339(req.end_date),
  };
  const response = await apiClient.post<Budget>('/v1/budgets', payload);
  return response.data;
}

export async function updateBudget(id: string, req: UpdateBudgetRequest): Promise<Budget> {
  const payload = {
    ...req,
    start_date: req.start_date ? toRFC3339(req.start_date) : undefined,
    end_date: req.end_date ? toRFC3339(req.end_date) : undefined,
  };
  const response = await apiClient.put<Budget>(`/v1/budgets/${id}`, payload);
  return response.data;
}

export async function deleteBudget(id: string): Promise<void> {
  await apiClient.delete(`/v1/budgets/${id}`);
}

export async function fetchAvailableCategories(): Promise<string[]> {
  const response = await apiClient.get<string[]>('/v1/budgets/categories/available');
  return response.data;
}

// Budget Category operations
export async function addBudgetCategory(
  budgetId: string,
  req: CreateBudgetCategoryRequest
): Promise<BudgetCategory> {
  const response = await apiClient.post<BudgetCategory>(
    `/v1/budgets/${budgetId}/categories`,
    req
  );
  return response.data;
}

export async function updateBudgetCategory(
  budgetId: string,
  categoryId: string,
  req: UpdateBudgetCategoryRequest
): Promise<BudgetCategory> {
  const response = await apiClient.put<BudgetCategory>(
    `/v1/budgets/${budgetId}/categories/${categoryId}`,
    req
  );
  return response.data;
}

export async function deleteBudgetCategory(
  budgetId: string,
  categoryId: string
): Promise<void> {
  await apiClient.delete(`/v1/budgets/${budgetId}/categories/${categoryId}`);
}

// Savings Goal CRUD operations
export async function fetchSavingsGoals(): Promise<SavingsGoal[]> {
  const response = await apiClient.get<SavingsGoal[]>('/v1/savings-goals');
  return response.data;
}

export async function fetchActiveSavingsGoals(): Promise<SavingsGoal[]> {
  const response = await apiClient.get<SavingsGoal[]>('/v1/savings-goals/active');
  return response.data;
}

export async function fetchSavingsGoal(id: string): Promise<SavingsGoal> {
  const response = await apiClient.get<SavingsGoal>(`/v1/savings-goals/${id}`);
  return response.data;
}

export async function createSavingsGoal(req: CreateSavingsGoalRequest): Promise<SavingsGoal> {
  const payload = {
    ...req,
    deadline: toRFC3339(req.deadline),
  };
  const response = await apiClient.post<SavingsGoal>('/v1/savings-goals', payload);
  return response.data;
}

export async function updateSavingsGoal(
  id: string,
  req: UpdateSavingsGoalRequest
): Promise<SavingsGoal> {
  const payload = {
    ...req,
    deadline: req.deadline ? toRFC3339(req.deadline) : undefined,
  };
  const response = await apiClient.put<SavingsGoal>(`/v1/savings-goals/${id}`, payload);
  return response.data;
}

export async function deleteSavingsGoal(id: string): Promise<void> {
  await apiClient.delete(`/v1/savings-goals/${id}`);
}

// =============================================================================
// Issue #497: Category Preview API
// =============================================================================

/**
 * Request type for category preview endpoint
 */
export interface CategoryPreviewRequest {
  categories: string[][];
  tags: string[];
  match_logic: MatchLogic;
  start_date: string;
  end_date: string;
}

/**
 * Response type for category preview endpoint
 */
export interface CategoryPreviewResponse {
  transaction_count: number;
  total_amount: number;
}

/**
 * Preview how many transactions match a category configuration.
 * Returns count and total amount of matching transactions.
 *
 * @param req - The preview request with categories, tags, match logic, and date range
 * @returns Count and total amount of matching transactions
 */
export async function previewCategoryMatches(
  req: CategoryPreviewRequest
): Promise<CategoryPreviewResponse> {
  const payload = {
    ...req,
    start_date: toRFC3339(req.start_date),
    end_date: toRFC3339(req.end_date),
  };
  const response = await apiClient.post<CategoryPreviewResponse>(
    '/v1/budgets/categories/preview',
    payload
  );
  return response.data;
}

// =============================================================================
// Issue #495: Envelope Budgeting - Sync and Transaction APIs
// =============================================================================

/**
 * Response type for GET /v1/transactions
 * Using a local minimal interface to avoid circular dependency with transactions feature
 */
export interface BudgetTransactionsResponse {
  transactions: unknown[]; // We don't need full typing for the budget feature
  total_count: number;
  has_more: boolean;
}

/**
 * Trigger a manual sync of budget category spending amounts.
 * This recalculates spent_amount for all categories in the budget
 * based on current transactions within the budget period.
 *
 * @param budgetId - The budget ID to sync
 * @returns The updated budget with recalculated spending
 */
export async function syncBudget(budgetId: string): Promise<Budget> {
  const response = await apiClient.post<Budget>(`/v1/budgets/${budgetId}/sync`);
  return response.data;
}

/**
 * Fetch transactions relevant to a budget's date range.
 * This uses the existing transactions API with budget-specific filtering.
 *
 * @param budgetId - The budget ID (for future filtering by budget-specific categories)
 * @param params - Optional query parameters
 * @returns Transactions within the budget's date range
 */
export async function fetchBudgetTransactions(
  _budgetId: string,
  params?: {
    startDate?: string;
    endDate?: string;
    limit?: number;
    offset?: number;
  }
): Promise<BudgetTransactionsResponse> {
  const queryParams: Record<string, string | number> = {};
  if (params?.startDate) queryParams.start_date = params.startDate;
  if (params?.endDate) queryParams.end_date = params.endDate;
  if (params?.limit) queryParams.limit = params.limit;
  if (params?.offset) queryParams.offset = params.offset;

  const response = await apiClient.get<BudgetTransactionsResponse>('/v1/transactions', {
    params: queryParams,
  });
  return response.data;
}

// =============================================================================
// Issue #499: Transaction-Budget Linking UI - Category Transactions API
// =============================================================================

/**
 * Fetch transactions matching a specific budget category.
 * Returns paginated transactions with total count and amount.
 *
 * @param budgetId - The budget ID
 * @param categoryId - The category ID within the budget
 * @param params - Optional pagination parameters
 * @returns Paginated transactions matching the category
 */
export async function fetchCategoryTransactions(
  budgetId: string,
  categoryId: string,
  params?: {
    limit?: number;
    offset?: number;
  }
): Promise<CategoryTransactionsResponse> {
  const queryParams: Record<string, number> = {};
  if (params?.limit) queryParams.limit = params.limit;
  if (params?.offset) queryParams.offset = params.offset;

  const response = await apiClient.get<CategoryTransactionsResponse>(
    `/v1/budgets/${budgetId}/categories/${categoryId}/transactions`,
    { params: queryParams }
  );
  return response.data;
}
