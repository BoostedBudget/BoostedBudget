import { apiClient } from '../../../api/axiosConfig'
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  ToggleRuleRequest,
} from '../types'

/**
 * Fetch all categorization rules for the authenticated user
 * GET /api/v1/rules
 */
export async function fetchRules(): Promise<CategorizationRule[]> {
  const response = await apiClient.get<CategorizationRule[]>('/v1/rules')
  return response.data
}

/**
 * Get a specific rule by ID
 * GET /api/v1/rules/:id
 */
export async function fetchRuleById(ruleId: string): Promise<CategorizationRule> {
  const response = await apiClient.get<CategorizationRule>(`/v1/rules/${ruleId}`)
  return response.data
}

/**
 * Create a new categorization rule
 * POST /api/v1/rules
 */
export async function createRule(request: CreateRuleRequest): Promise<CategorizationRule> {
  const response = await apiClient.post<CategorizationRule>('/v1/rules', request)
  return response.data
}

/**
 * Update an existing rule
 * PUT /api/v1/rules/:id
 */
export async function updateRule(
  ruleId: string,
  request: UpdateRuleRequest
): Promise<CategorizationRule> {
  const response = await apiClient.put<CategorizationRule>(`/v1/rules/${ruleId}`, request)
  return response.data
}

/**
 * Delete a rule
 * DELETE /api/v1/rules/:id
 */
export async function deleteRule(ruleId: string): Promise<void> {
  await apiClient.delete(`/v1/rules/${ruleId}`)
}

/**
 * Toggle rule enabled/disabled status
 * POST /api/v1/rules/:id/toggle
 */
export async function toggleRule(
  ruleId: string,
  enabled: boolean
): Promise<CategorizationRule> {
  const request: ToggleRuleRequest = { enabled }
  const response = await apiClient.post<CategorizationRule>(`/v1/rules/${ruleId}/toggle`, request)
  return response.data
}
