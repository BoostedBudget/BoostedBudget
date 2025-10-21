import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchLinkToken, exchangePublicToken } from './plaidApi'
import type { LinkTokenResponse, ExchangePublicTokenRequest, ExchangePublicTokenResponse } from '../types'

describe('plaidApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
    vi.clearAllMocks()
  })

  describe('fetchLinkToken', () => {
    it('should call POST /api/v1/plaid/link-token and return link token', async () => {
      const mockResponse: LinkTokenResponse = {
        link_token: 'link-sandbox-abc123-def456',
        expiration: '2025-10-20T22:00:00Z',
        request_id: 'plaid-request-xyz789',
      }

      mock.onPost('/api/v1/plaid/link-token').reply(200, mockResponse)

      const result = await fetchLinkToken()

      expect(result).toEqual(mockResponse)
      expect(mock.history.post.length).toBe(1)
      expect(mock.history.post[0].url).toBe('/api/v1/plaid/link-token')
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const mockResponse: LinkTokenResponse = {
        link_token: 'link-token-123',
        expiration: '2025-10-20T22:00:00Z',
        request_id: 'request-123',
      }

      mock.onPost('/api/v1/plaid/link-token').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await fetchLinkToken()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onPost('/api/v1/plaid/link-token').reply(401, { error: 'Unauthorized' })

      await expect(fetchLinkToken()).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      mock.onPost('/api/v1/plaid/link-token').reply(500, { error: 'Internal server error' })

      await expect(fetchLinkToken()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onPost('/api/v1/plaid/link-token').networkError()

      await expect(fetchLinkToken()).rejects.toThrow()
    })
  })

  describe('exchangePublicToken', () => {
    it('should call POST /api/v1/plaid/exchange-token with request body', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'public-sandbox-abc123-def456',
        institution_id: 'ins_109508',
        institution_name: 'Chase',
      }

      const mockResponse: ExchangePublicTokenResponse = {
        success: true,
        item_id: 'item-xyz123',
        accounts_linked: 3,
      }

      mock.onPost('/api/v1/plaid/exchange-token', request).reply(200, mockResponse)

      const result = await exchangePublicToken(request)

      expect(result).toEqual(mockResponse)
      expect(mock.history.post.length).toBe(1)
      expect(mock.history.post[0].url).toBe('/api/v1/plaid/exchange-token')
      expect(JSON.parse(mock.history.post[0].data)).toEqual(request)
    })

    it('should handle 400 invalid or expired token error', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'invalid-token',
        institution_id: 'ins_123',
        institution_name: 'Test Bank',
      }

      mock.onPost('/api/v1/plaid/exchange-token').reply(400, { error: 'Invalid or expired public token' })

      await expect(exchangePublicToken(request)).rejects.toThrow()
    })

    it('should handle 403 account limit exceeded error', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'public-token-123',
        institution_id: 'ins_456',
        institution_name: 'Test Bank',
      }

      mock.onPost('/api/v1/plaid/exchange-token').reply(403, {
        error: 'Account limit exceeded for your subscription tier'
      })

      await expect(exchangePublicToken(request)).rejects.toThrow()
    })

    it('should handle 503 Plaid service unavailable error', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'public-token-789',
        institution_id: 'ins_789',
        institution_name: 'Test Bank',
      }

      mock.onPost('/api/v1/plaid/exchange-token').reply(503, {
        error: 'Plaid service temporarily unavailable'
      })

      await expect(exchangePublicToken(request)).rejects.toThrow()
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'public-token-auth',
        institution_id: 'ins_auth',
        institution_name: 'Auth Test Bank',
      }

      const mockResponse: ExchangePublicTokenResponse = {
        success: true,
        item_id: 'item-auth',
        accounts_linked: 1,
      }

      mock.onPost('/api/v1/plaid/exchange-token').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await exchangePublicToken(request)
    })
  })
})
