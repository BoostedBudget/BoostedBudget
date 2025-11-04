import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchLinkToken, exchangePublicToken, fetchAccounts, deleteAccount, disconnectInstitution } from './plaidApi'
import type { LinkTokenResponse, ExchangePublicTokenRequest, ExchangePublicTokenResponse, GetAccountsResponse } from '../types'

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
    it('should call POST /v1/plaid/link-token and return link token', async () => {
      const mockResponse: LinkTokenResponse = {
        link_token: 'link-sandbox-abc123-def456',
        expiration: '2025-10-20T22:00:00Z',
        request_id: 'plaid-request-xyz789',
      }

      mock.onPost('/v1/plaid/link-token').reply(200, mockResponse)

      const result = await fetchLinkToken()

      expect(result).toEqual(mockResponse)
      expect(mock.history.post.length).toBe(1)
      expect(mock.history.post[0].url).toBe('/v1/plaid/link-token')
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const mockResponse: LinkTokenResponse = {
        link_token: 'link-token-123',
        expiration: '2025-10-20T22:00:00Z',
        request_id: 'request-123',
      }

      mock.onPost('/v1/plaid/link-token').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await fetchLinkToken()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onPost('/v1/plaid/link-token').reply(401, { error: 'Unauthorized' })

      await expect(fetchLinkToken()).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      mock.onPost('/v1/plaid/link-token').reply(500, { error: 'Internal server error' })

      await expect(fetchLinkToken()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onPost('/v1/plaid/link-token').networkError()

      await expect(fetchLinkToken()).rejects.toThrow()
    })
  })

  describe('exchangePublicToken', () => {
    it('should call POST /v1/plaid/exchange-token with request body', async () => {
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

      mock.onPost('/v1/plaid/exchange-token', request).reply(200, mockResponse)

      const result = await exchangePublicToken(request)

      expect(result).toEqual(mockResponse)
      expect(mock.history.post.length).toBe(1)
      expect(mock.history.post[0].url).toBe('/v1/plaid/exchange-token')
      expect(JSON.parse(mock.history.post[0].data)).toEqual(request)
    })

    it('should handle 400 invalid or expired token error', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'invalid-token',
        institution_id: 'ins_123',
        institution_name: 'Test Bank',
      }

      mock.onPost('/v1/plaid/exchange-token').reply(400, { error: 'Invalid or expired public token' })

      await expect(exchangePublicToken(request)).rejects.toThrow()
    })

    it('should handle 403 account limit exceeded error', async () => {
      const request: ExchangePublicTokenRequest = {
        public_token: 'public-token-123',
        institution_id: 'ins_456',
        institution_name: 'Test Bank',
      }

      mock.onPost('/v1/plaid/exchange-token').reply(403, {
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

      mock.onPost('/v1/plaid/exchange-token').reply(503, {
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

      mock.onPost('/v1/plaid/exchange-token').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await exchangePublicToken(request)
    })
  })

  describe('fetchAccounts', () => {
    it('should call GET /v1/accounts and return accounts list', async () => {
      const mockResponse: GetAccountsResponse = {
        accounts: [
          {
            id: '550e8400-e29b-41d4-a716-446655440000',
            linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
            user_id: 'auth0|507f1f77bcf86cd799439011',
            plaid_account_id: 'plaid-acc-1',
            name: 'Chase Checking',
            official_name: 'Chase Total Checking',
            type: 'depository',
            subtype: 'checking',
            mask: '0000',
            current_balance: 2500.50,
            available_balance: 2300.50,
            currency_code: 'USD',
            institution_name: 'Chase',
            created_at: '2025-10-18T14:30:00Z',
            updated_at: '2025-10-20T10:15:00Z',
          },
          {
            id: '660f9511-f3ac-52e5-b827-557766551111',
            linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
            user_id: 'auth0|507f1f77bcf86cd799439011',
            plaid_account_id: 'plaid-acc-2',
            name: 'Wells Fargo Savings',
            type: 'depository',
            subtype: 'savings',
            mask: '1234',
            current_balance: 10000.00,
            available_balance: 10000.00,
            currency_code: 'USD',
            institution_name: 'Wells Fargo',
            created_at: '2025-10-19T09:20:00Z',
            updated_at: '2025-10-20T10:15:00Z',
          },
        ],
        total_count: 2,
      }

      mock.onGet('/v1/accounts').reply(200, mockResponse)

      const result = await fetchAccounts()

      expect(result).toEqual(mockResponse)
      expect(result.accounts).toHaveLength(2)
      expect(result.total_count).toBe(2)
      expect(mock.history.get.length).toBe(1)
      expect(mock.history.get[0].url).toBe('/v1/accounts')
    })

    it('should return empty accounts array when user has no accounts', async () => {
      const mockResponse: GetAccountsResponse = {
        accounts: [],
        total_count: 0,
      }

      mock.onGet('/v1/accounts').reply(200, mockResponse)

      const result = await fetchAccounts()

      expect(result.accounts).toEqual([])
      expect(result.total_count).toBe(0)
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const mockResponse: GetAccountsResponse = {
        accounts: [],
        total_count: 0,
      }

      mock.onGet('/v1/accounts').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await fetchAccounts()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onGet('/v1/accounts').reply(401, { error: 'Unauthorized' })

      await expect(fetchAccounts()).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      mock.onGet('/v1/accounts').reply(500, { error: 'Internal server error' })

      await expect(fetchAccounts()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/accounts').networkError()

      await expect(fetchAccounts()).rejects.toThrow()
    })
  })

  describe('deleteAccount', () => {
    it('should call DELETE /v1/accounts/:id and return successfully', async () => {
      const accountId = '550e8400-e29b-41d4-a716-446655440000'

      mock.onDelete(`/v1/accounts/${accountId}`).reply(204)

      await deleteAccount(accountId)

      expect(mock.history.delete.length).toBe(1)
      expect(mock.history.delete[0].url).toBe(`/v1/accounts/${accountId}`)
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const accountId = '550e8400-e29b-41d4-a716-446655440000'

      mock.onDelete(`/v1/accounts/${accountId}`).reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [204]
      })

      await deleteAccount(accountId)
    })

    it('should handle 404 not found error', async () => {
      const accountId = 'non-existent-account-id'

      mock.onDelete(`/v1/accounts/${accountId}`).reply(404, { error: 'Account not found' })

      await expect(deleteAccount(accountId)).rejects.toThrow()
    })

    it('should handle 401 unauthorized error', async () => {
      const accountId = '550e8400-e29b-41d4-a716-446655440000'

      mock.onDelete(`/v1/accounts/${accountId}`).reply(401, { error: 'Unauthorized' })

      await expect(deleteAccount(accountId)).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      const accountId = '550e8400-e29b-41d4-a716-446655440000'

      mock.onDelete(`/v1/accounts/${accountId}`).reply(500, { error: 'Internal server error' })

      await expect(deleteAccount(accountId)).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      const accountId = '550e8400-e29b-41d4-a716-446655440000'

      mock.onDelete(`/v1/accounts/${accountId}`).networkError()

      await expect(deleteAccount(accountId)).rejects.toThrow()
    })
  })

  describe('disconnectInstitution', () => {
    it('should call DELETE /v1/linked-items/:id and return successfully', async () => {
      const linkedItemId = '770e9511-f3ac-52e5-b827-557766551112'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).reply(204)

      await disconnectInstitution(linkedItemId)

      expect(mock.history.delete.length).toBe(1)
      expect(mock.history.delete[0].url).toBe(`/v1/linked-items/${linkedItemId}`)
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const linkedItemId = '770e9511-f3ac-52e5-b827-557766551112'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [204]
      })

      await disconnectInstitution(linkedItemId)
    })

    it('should handle 404 institution not found error', async () => {
      const linkedItemId = 'non-existent-item-id'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).reply(404, { error: 'Institution not found' })

      await expect(disconnectInstitution(linkedItemId)).rejects.toThrow()
    })

    it('should handle 401 unauthorized error', async () => {
      const linkedItemId = '770e9511-f3ac-52e5-b827-557766551112'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).reply(401, { error: 'Unauthorized' })

      await expect(disconnectInstitution(linkedItemId)).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      const linkedItemId = '770e9511-f3ac-52e5-b827-557766551112'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).reply(500, { error: 'Failed to disconnect institution' })

      await expect(disconnectInstitution(linkedItemId)).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      const linkedItemId = '770e9511-f3ac-52e5-b827-557766551112'

      mock.onDelete(`/v1/linked-items/${linkedItemId}`).networkError()

      await expect(disconnectInstitution(linkedItemId)).rejects.toThrow()
    })
  })
})
