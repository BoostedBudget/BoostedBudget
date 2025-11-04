import { apiClient } from '../../../api/axiosConfig'
import type { User, UpdateProfileRequest } from '../types'

/**
 * Fetch the authenticated user's profile
 * GET /api/v1/user/profile
 */
export async function fetchProfile(): Promise<User> {
  const response = await apiClient.get<User>('/v1/user/profile')
  return response.data
}

/**
 * Update the authenticated user's profile
 * PUT /api/v1/user/profile
 */
export async function updateProfile(data: UpdateProfileRequest): Promise<User> {
  const response = await apiClient.put<User>('/v1/user/profile', data)
  return response.data
}
