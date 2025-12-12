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
} from '../types';

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
