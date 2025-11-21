import { apiClient } from '../../../api/axiosConfig'
import type { CustomCategory, CreateCategoryRequest, UpdateCategoryRequest } from '../types'

/**
 * Fetch all custom categories for the authenticated user
 * GET /api/v1/categories
 */
export async function fetchCategories(): Promise<CustomCategory[]> {
  const response = await apiClient.get<CustomCategory[]>('/v1/categories')
  return response.data
}

/**
 * Get a specific category by ID
 * GET /api/v1/categories/:id
 */
export async function fetchCategoryById(categoryId: string): Promise<CustomCategory> {
  const response = await apiClient.get<CustomCategory>(`/v1/categories/${categoryId}`)
  return response.data
}

/**
 * Create a new custom category
 * POST /api/v1/categories
 */
export async function createCategory(request: CreateCategoryRequest): Promise<CustomCategory> {
  const response = await apiClient.post<CustomCategory>('/v1/categories', request)
  return response.data
}

/**
 * Update an existing category
 * PUT /api/v1/categories/:id
 */
export async function updateCategory(categoryId: string, request: UpdateCategoryRequest): Promise<CustomCategory> {
  const response = await apiClient.put<CustomCategory>(`/v1/categories/${categoryId}`, request)
  return response.data
}

/**
 * Delete a category
 * DELETE /api/v1/categories/:id
 */
export async function deleteCategory(categoryId: string): Promise<void> {
  await apiClient.delete(`/v1/categories/${categoryId}`)
}
