import { apiClient } from '../../../api/axiosConfig'
import type { SyncStats } from '../types'

/**
 * Fetch monthly sync statistics for the authenticated user
 * GET /api/v1/sync-stats
 *
 * Returns information about:
 * - How many accounts have been synced this month
 * - Monthly sync limit based on subscription tier
 * - Whether user can sync more accounts
 * - When the limit resets (first day of next month)
 *
 * Issue #242: Monthly sync limits
 *
 * @returns Sync statistics data
 * @throws Error if request fails (401 unauthorized, 500 server error)
 */
export async function fetchSyncStats(): Promise<SyncStats> {
  const response = await apiClient.get<SyncStats>('/v1/sync-stats')
  return response.data
}
