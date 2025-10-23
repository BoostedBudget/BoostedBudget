/**
 * Subscription API Module
 *
 * Handles fetching subscription tier configuration from the backend API.
 * This is a public endpoint (no authentication required) with CDN caching.
 *
 * TDD GREEN: Minimal implementation to pass tests.
 */

import { apiClient } from '../../../api/axiosConfig'
import type { SubscriptionTiersResponse } from '../types'

/**
 * Fetch all subscription tiers from the backend
 *
 * @returns Promise resolving to subscription tiers configuration
 * @throws Error if the API request fails
 *
 * @example
 * ```typescript
 * try {
 *   const { tiers, default_tier } = await fetchSubscriptionTiers()
 *   console.log('Free tier:', tiers.free)
 * } catch (error) {
 *   console.error('Failed to load tiers:', error)
 * }
 * ```
 */
export async function fetchSubscriptionTiers(): Promise<SubscriptionTiersResponse> {
  const response = await apiClient.get<SubscriptionTiersResponse>('/api/v1/subscription/tiers')
  return response.data
}
