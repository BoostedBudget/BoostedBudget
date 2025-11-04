import { apiClient } from '../../../api/axiosConfig'
import type { PrivacySettings, UpdatePrivacySettingsRequest } from '../types'

/**
 * Fetch the authenticated user's privacy settings
 * GET /api/v1/user/privacy
 * Auto-creates GDPR-compliant defaults on first access
 */
export async function fetchPrivacySettings(): Promise<PrivacySettings> {
  const response = await apiClient.get<PrivacySettings>('/v1/user/privacy')
  return response.data
}

/**
 * Update the authenticated user's privacy settings
 * PUT /api/v1/user/privacy
 * Enforces GDPR validation (necessary consent cannot be disabled)
 */
export async function updatePrivacySettings(data: UpdatePrivacySettingsRequest): Promise<PrivacySettings> {
  const response = await apiClient.put<PrivacySettings>('/v1/user/privacy', data)
  return response.data
}
