import type { Page } from '@playwright/test'
import { registerRevenueCatFixture } from '../../src/test/support/revenuecat-e2e-fixture'
import { apiClient } from './api/client'

// Simulated confirmed access for application E2E only. Provider lifecycle tests
// use the RevenueCat integration suite and the real sandbox acceptance checklist.
export async function prepareRevenueCatFixture(page: Page, expectedEmail: string): Promise<void> {
  const allowed = [process.env.TEST_CLERK_EMAIL, process.env.TEST_CLERK_EMAIL_2].filter(Boolean)
  if (!allowed.includes(expectedEmail)) throw new Error('Identity is not configured for this E2E run')
  const api = await apiClient(page)
  const response = await api.get('/api/v1/user/profile')
  if (!response.ok()) throw new Error('Cannot prepare fixture before authenticated registration')
  const profile = await response.json()
  if (profile.email?.toLowerCase() !== expectedEmail.toLowerCase()) throw new Error('E2E fixture identity mismatch')
  registerRevenueCatFixture(profile.id)
  const authorityResponse = await api.get('/api/v1/subscription/authority')
  if (!authorityResponse.ok()) throw new Error('Simulated RevenueCat authority is unavailable')
  const authority = await authorityResponse.json()
  if (!authority.access_resolved || authority.own?.tier !== 'premium' || authority.own?.source !== 'comp') {
    throw new Error('Simulated RevenueCat projection did not authorize the configured test user')
  }
}
