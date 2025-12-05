/**
 * Dashboard Preferences API
 * Issue #354: Customizable Dashboard Widget System
 *
 * Provides API functions for dashboard layout persistence.
 */

import { apiClient } from '../../../api/axiosConfig';
import type { DashboardLayout, DashboardPreferencesResponse } from '../types';

/**
 * Get user's dashboard preferences
 */
export async function getDashboardPreferences(): Promise<DashboardPreferencesResponse> {
  const response = await apiClient.get<DashboardPreferencesResponse>(
    '/v1/dashboard/preferences'
  );
  return response.data;
}

/**
 * Save user's dashboard preferences
 */
export async function saveDashboardPreferences(
  layout: DashboardLayout
): Promise<DashboardPreferencesResponse> {
  const response = await apiClient.put<DashboardPreferencesResponse>(
    '/v1/dashboard/preferences',
    { layout }
  );
  return response.data;
}

/**
 * Reset dashboard preferences to default
 */
export async function resetDashboardPreferences(): Promise<DashboardPreferencesResponse> {
  const response = await apiClient.post<DashboardPreferencesResponse>(
    '/v1/dashboard/preferences/reset'
  );
  return response.data;
}
