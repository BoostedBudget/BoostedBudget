import { apiClient } from '../../../api/axiosConfig'
import type { RequestDeletionResponse, CancelDeletionResponse } from '../types'

/**
 * Request account deletion
 * POST /api/v1/user/delete
 *
 * Schedules user account for deletion after 30-day grace period.
 * Returns cancellation token for user to cancel deletion.
 *
 * @returns Response with scheduled date and cancellation token
 * @throws Error if request fails
 */
export async function requestDeletion(): Promise<RequestDeletionResponse> {
  const response = await apiClient.post<RequestDeletionResponse>('/v1/user/delete')
  return response.data
}

/**
 * Cancel scheduled account deletion
 * GET /v1/user/delete/cancel/:token
 *
 * Public endpoint (no auth required) - validates JWT token.
 * Cancels the scheduled deletion for the user associated with the token.
 *
 * @param token - JWT cancellation token from deletion request
 * @returns Cancellation confirmation message
 * @throws Error if token is invalid or expired
 */
export async function cancelDeletion(token: string): Promise<CancelDeletionResponse> {
  const response = await apiClient.get<CancelDeletionResponse>(`/v1/user/delete/cancel/${token}`)
  return response.data
}
