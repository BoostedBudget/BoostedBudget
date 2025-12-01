import { apiClient } from '../../../api/axiosConfig'
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  ToggleRuleRequest,
} from '../types'

/**
 * Fetch all auto-categorization rules for the authenticated user
 * GET /api/v1/auto-categorization
 */
export async function fetchAutoCategorizationRules(): Promise<CategorizationRule[]> {
  const response = await apiClient.get<CategorizationRule[]>('/v1/auto-categorization')
  return response.data
}

/**
 * Get a specific auto-categorization rule by ID
 * GET /api/v1/auto-categorization/:id
 */
export async function fetchAutoCategorizationRuleById(ruleId: string): Promise<CategorizationRule> {
  const response = await apiClient.get<CategorizationRule>(`/v1/auto-categorization/${ruleId}`)
  return response.data
}

/**
 * Create a new auto-categorization rule
 * POST /api/v1/auto-categorization
 */
export async function createAutoCategorizationRule(request: CreateRuleRequest): Promise<CategorizationRule> {
  const response = await apiClient.post<CategorizationRule>('/v1/auto-categorization', request)
  return response.data
}

/**
 * Update an existing auto-categorization rule
 * PUT /api/v1/auto-categorization/:id
 */
export async function updateAutoCategorizationRule(
  ruleId: string,
  request: UpdateRuleRequest
): Promise<CategorizationRule> {
  const response = await apiClient.put<CategorizationRule>(`/v1/auto-categorization/${ruleId}`, request)
  return response.data
}

/**
 * Delete an auto-categorization rule
 * DELETE /api/v1/auto-categorization/:id
 */
export async function deleteAutoCategorizationRule(ruleId: string): Promise<void> {
  await apiClient.delete(`/v1/auto-categorization/${ruleId}`)
}

/**
 * Toggle auto-categorization rule enabled/disabled status
 * POST /api/v1/auto-categorization/:id/toggle
 */
export async function toggleAutoCategorizationRule(
  ruleId: string,
  enabled: boolean
): Promise<CategorizationRule> {
  const request: ToggleRuleRequest = { enabled }
  const response = await apiClient.post<CategorizationRule>(`/v1/auto-categorization/${ruleId}/toggle`, request)
  return response.data
}
