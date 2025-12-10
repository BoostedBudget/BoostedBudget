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
  const response = await apiClient.post<Budget>('/v1/budgets', req);
  return response.data;
}

export async function updateBudget(id: string, req: UpdateBudgetRequest): Promise<Budget> {
  const response = await apiClient.put<Budget>(`/v1/budgets/${id}`, req);
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
  const response = await apiClient.post<SavingsGoal>('/v1/savings-goals', req);
  return response.data;
}

export async function updateSavingsGoal(
  id: string,
  req: UpdateSavingsGoalRequest
): Promise<SavingsGoal> {
  const response = await apiClient.put<SavingsGoal>(`/v1/savings-goals/${id}`, req);
  return response.data;
}

export async function deleteSavingsGoal(id: string): Promise<void> {
  await apiClient.delete(`/v1/savings-goals/${id}`);
}
