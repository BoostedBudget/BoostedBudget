/**
 * Subscription API Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the fetchSubscriptionTiers API function following existing patterns.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fetchSubscriptionTiers } from './subscriptionApi'
import { apiClient } from '../../../api/axiosConfig'
import type { SubscriptionTiersResponse } from '../types'

// Mock axios client
vi.mock('../../../api/axiosConfig', () => ({
  apiClient: {
    get: vi.fn(),
  },
}))

describe('fetchSubscriptionTiers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('should call GET /api/v1/subscription/tiers', async () => {
    // Arrange
    const mockResponse: SubscriptionTiersResponse = {
      tiers: {
        free: {
          display_name: 'Free',
          description: 'Basic plan',
          price_monthly: 0,
          price_yearly: 0,
          limits: {
            accounts: 3,
            rules: 1,
            executions_per_day: 1,
            min_execution_interval_minutes: 1440,
          },
          features: {
            ads_enabled: true,
            ai_insights: false,
          },
        },
        premium_lite: {
          display_name: 'Premium Lite',
          description: 'Lite plan',
          price_monthly: 4.99,
          price_yearly: 49.99,
          limits: {
            accounts: 15,
            rules: 5,
            executions_per_day: 2,
            min_execution_interval_minutes: 720,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium: {
          display_name: 'Premium',
          description: 'Premium plan',
          price_monthly: 9.99,
          price_yearly: 99.99,
          limits: {
            accounts: 30,
            rules: 15,
            executions_per_day: 6,
            min_execution_interval_minutes: 240,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium_plus: {
          display_name: 'Premium+',
          description: 'Premium plus plan',
          price_monthly: 19.99,
          price_yearly: 199.99,
          limits: {
            accounts: -1,
            rules: -1,
            executions_per_day: 144,
            min_execution_interval_minutes: 10,
          },
          features: {
            ads_enabled: false,
            ai_insights: true,
            api_access: true,
            priority_support: true,
          },
        },
      },
      default_tier: 'free',
    }

    vi.mocked(apiClient.get).mockResolvedValue({ data: mockResponse })

    // Act
    await fetchSubscriptionTiers()

    // Assert
    expect(apiClient.get).toHaveBeenCalledWith('/api/v1/subscription/tiers')
    expect(apiClient.get).toHaveBeenCalledTimes(1)
  })

  it('should return tiers and default_tier from response', async () => {
    // Arrange
    const mockResponse: SubscriptionTiersResponse = {
      tiers: {
        free: {
          display_name: 'Free',
          description: 'Basic plan',
          price_monthly: 0,
          price_yearly: 0,
          limits: {
            accounts: 3,
            rules: 1,
            executions_per_day: 1,
            min_execution_interval_minutes: 1440,
          },
          features: {
            ads_enabled: true,
            ai_insights: false,
          },
        },
        premium_lite: {
          display_name: 'Premium Lite',
          description: 'Lite plan',
          price_monthly: 4.99,
          price_yearly: 49.99,
          limits: {
            accounts: 15,
            rules: 5,
            executions_per_day: 2,
            min_execution_interval_minutes: 720,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium: {
          display_name: 'Premium',
          description: 'Premium plan',
          price_monthly: 9.99,
          price_yearly: 99.99,
          limits: {
            accounts: 30,
            rules: 15,
            executions_per_day: 6,
            min_execution_interval_minutes: 240,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium_plus: {
          display_name: 'Premium+',
          description: 'Premium plus plan',
          price_monthly: 19.99,
          price_yearly: 199.99,
          limits: {
            accounts: -1,
            rules: -1,
            executions_per_day: 144,
            min_execution_interval_minutes: 10,
          },
          features: {
            ads_enabled: false,
            ai_insights: true,
            api_access: true,
            priority_support: true,
          },
        },
      },
      default_tier: 'free',
    }

    vi.mocked(apiClient.get).mockResolvedValue({ data: mockResponse })

    // Act
    const result = await fetchSubscriptionTiers()

    // Assert
    expect(result).toEqual(mockResponse)
    expect(result.tiers).toBeDefined()
    expect(result.default_tier).toBe('free')
    expect(Object.keys(result.tiers)).toHaveLength(4)
  })

  it('should parse response data correctly', async () => {
    // Arrange
    const mockResponse: SubscriptionTiersResponse = {
      tiers: {
        free: {
          display_name: 'Free',
          description: 'Full budgeting with basic automation',
          price_monthly: 0,
          price_yearly: 0,
          limits: {
            accounts: 3,
            rules: 1,
            executions_per_day: 1,
            min_execution_interval_minutes: 1440,
          },
          features: {
            ads_enabled: true,
            ai_insights: false,
          },
        },
        premium_lite: {
          display_name: 'Premium Lite',
          description: 'Lite plan',
          price_monthly: 4.99,
          price_yearly: 49.99,
          limits: {
            accounts: 15,
            rules: 5,
            executions_per_day: 2,
            min_execution_interval_minutes: 720,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium: {
          display_name: 'Premium',
          description: 'Premium plan',
          price_monthly: 9.99,
          price_yearly: 99.99,
          limits: {
            accounts: 30,
            rules: 15,
            executions_per_day: 6,
            min_execution_interval_minutes: 240,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium_plus: {
          display_name: 'Premium+',
          description: 'Premium plus plan',
          price_monthly: 19.99,
          price_yearly: 199.99,
          limits: {
            accounts: -1,
            rules: -1,
            executions_per_day: 144,
            min_execution_interval_minutes: 10,
          },
          features: {
            ads_enabled: false,
            ai_insights: true,
            api_access: true,
            priority_support: true,
          },
        },
      },
      default_tier: 'free',
    }

    vi.mocked(apiClient.get).mockResolvedValue({ data: mockResponse })

    // Act
    const result = await fetchSubscriptionTiers()

    // Assert - Verify structure
    expect(result.tiers.free).toBeDefined()
    expect(result.tiers.free.display_name).toBe('Free')
    expect(result.tiers.free.limits.accounts).toBe(3)
    expect(result.tiers.free.limits.rules).toBe(1)
    expect(result.tiers.free.features.ads_enabled).toBe(true)

    expect(result.tiers.premium_plus).toBeDefined()
    expect(result.tiers.premium_plus.limits.accounts).toBe(-1) // unlimited
    expect(result.tiers.premium_plus.features.ai_insights).toBe(true)
  })

  it('should handle network errors gracefully', async () => {
    // Arrange
    const networkError = new Error('Network Error')
    vi.mocked(apiClient.get).mockRejectedValue(networkError)

    // Act & Assert
    await expect(fetchSubscriptionTiers()).rejects.toThrow('Network Error')
  })

  it('should handle 500 server errors', async () => {
    // Arrange
    const serverError = {
      response: {
        status: 500,
        data: { error: 'Internal server error' },
      },
    }
    vi.mocked(apiClient.get).mockRejectedValue(serverError)

    // Act & Assert
    await expect(fetchSubscriptionTiers()).rejects.toEqual(serverError)
  })

  it('should NOT include Authorization header (public endpoint)', async () => {
    // Arrange
    const mockResponse: SubscriptionTiersResponse = {
      tiers: {
        free: {
          display_name: 'Free',
          description: 'Basic plan',
          price_monthly: 0,
          price_yearly: 0,
          limits: {
            accounts: 3,
            rules: 1,
            executions_per_day: 1,
            min_execution_interval_minutes: 1440,
          },
          features: {
            ads_enabled: true,
            ai_insights: false,
          },
        },
        premium_lite: {
          display_name: 'Premium Lite',
          description: 'Lite plan',
          price_monthly: 4.99,
          price_yearly: 49.99,
          limits: {
            accounts: 15,
            rules: 5,
            executions_per_day: 2,
            min_execution_interval_minutes: 720,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium: {
          display_name: 'Premium',
          description: 'Premium plan',
          price_monthly: 9.99,
          price_yearly: 99.99,
          limits: {
            accounts: 30,
            rules: 15,
            executions_per_day: 6,
            min_execution_interval_minutes: 240,
          },
          features: {
            ads_enabled: false,
            ai_insights: false,
          },
        },
        premium_plus: {
          display_name: 'Premium+',
          description: 'Premium plus plan',
          price_monthly: 19.99,
          price_yearly: 199.99,
          limits: {
            accounts: -1,
            rules: -1,
            executions_per_day: 144,
            min_execution_interval_minutes: 10,
          },
          features: {
            ads_enabled: false,
            ai_insights: true,
            api_access: true,
            priority_support: true,
          },
        },
      },
      default_tier: 'free',
    }

    vi.mocked(apiClient.get).mockResolvedValue({ data: mockResponse })

    // Act
    await fetchSubscriptionTiers()

    // Assert - Verify the call was made without passing a config with Authorization
    // The apiClient.get should be called with just the URL, no config object
    expect(apiClient.get).toHaveBeenCalledWith('/api/v1/subscription/tiers')

    // Verify it was NOT called with a second parameter (config object)
    const calls = vi.mocked(apiClient.get).mock.calls
    expect(calls[0]).toHaveLength(1) // Only URL parameter, no config
  })
})
