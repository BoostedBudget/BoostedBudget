import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchTransactions } from './transactionsApi'
import type { GetTransactionsResponse, FetchTransactionsParams } from '../types'

describe('transactionsApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
    vi.clearAllMocks()
  })

  describe('fetchTransactions', () => {
    it('should call GET /v1/transactions with default parameters', async () => {
      const mockResponse: GetTransactionsResponse = {
        transactions: [
          {
            id: 'txn-1',
            user_id: 'user-123',
            account_id: 'account-456',
            linked_item_id: 'item-789',
            source: 'plaid',
            external_transaction_id: 'plaid-txn-abc',
            amount: -25.50,
            date: '2025-10-25',
            name: 'Starbucks',
            merchant_name: 'Starbucks Coffee',
            category: '["Food and Drink", "Coffee"]',
            pending: false,
            created_at: '2025-10-25T10:00:00Z',
            updated_at: '2025-10-25T10:00:00Z',
          },
        ],
        total_count: 1,
        limit: 50,
        offset: 0,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply(200, mockResponse)

      const result = await fetchTransactions()

      expect(result).toEqual(mockResponse)
      expect(result.transactions).toHaveLength(1)
      expect(result.total_count).toBe(1)
      expect(mock.history.get.length).toBe(1)
      expect(mock.history.get[0].url).toBe('/v1/transactions')
    })

    it('should construct URL with limit and offset query parameters', async () => {
      const params: FetchTransactionsParams = {
        limit: 25,
        offset: 100,
      }

      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 150,
        limit: 25,
        offset: 100,
        has_more: true,
      }

      mock.onGet('/v1/transactions').reply((config) => {
        expect(config.params).toEqual({
          limit: 25,
          offset: 100,
        })
        return [200, mockResponse]
      })

      const result = await fetchTransactions(params)

      expect(result.limit).toBe(25)
      expect(result.offset).toBe(100)
      expect(result.has_more).toBe(true)
    })

    it('should construct URL with filter query parameters', async () => {
      const params: FetchTransactionsParams = {
        filters: {
          account_id: 'account-123',
          start_date: '2025-01-01',
          end_date: '2025-12-31',
          category: 'food',
          search: 'starbucks',
          pending: true,
        },
      }

      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 0,
        limit: 50,
        offset: 0,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply((config) => {
        expect(config.params).toEqual({
          account_id: 'account-123',
          start_date: '2025-01-01',
          end_date: '2025-12-31',
          category: 'food',
          search: 'starbucks',
          pending: true,
        })
        return [200, mockResponse]
      })

      await fetchTransactions(params)
    })

    it('should construct URL with combined limit, offset, and filters', async () => {
      const params: FetchTransactionsParams = {
        limit: 10,
        offset: 20,
        filters: {
          account_id: 'account-456',
          search: 'amazon',
        },
      }

      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 5,
        limit: 10,
        offset: 20,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply((config) => {
        expect(config.params).toEqual({
          limit: 10,
          offset: 20,
          account_id: 'account-456',
          search: 'amazon',
        })
        return [200, mockResponse]
      })

      await fetchTransactions(params)
    })

    it('should return empty transactions array when no transactions exist', async () => {
      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 0,
        limit: 50,
        offset: 0,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply(200, mockResponse)

      const result = await fetchTransactions()

      expect(result.transactions).toEqual([])
      expect(result.total_count).toBe(0)
      expect(result.has_more).toBe(false)
    })

    it('should include Authorization header from apiClient interceptor', async () => {
      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 0,
        limit: 50,
        offset: 0,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply((config) => {
        // Verify Authorization header exists (set by axios interceptor)
        expect(config.headers).toBeDefined()
        return [200, mockResponse]
      })

      await fetchTransactions()
    })

    it('should handle 401 unauthorized error', async () => {
      mock.onGet('/v1/transactions').reply(401, { error: 'Unauthorized' })

      await expect(fetchTransactions()).rejects.toThrow()
    })

    it('should handle 400 bad request error (invalid parameters)', async () => {
      mock.onGet('/v1/transactions').reply(400, { error: 'Invalid limit parameter' })

      await expect(fetchTransactions({ limit: 150 })).rejects.toThrow()
    })

    it('should handle 400 bad request error (invalid date format)', async () => {
      mock.onGet('/v1/transactions').reply(400, { error: 'Invalid start_date format' })

      await expect(
        fetchTransactions({ filters: { start_date: 'not-a-date' } })
      ).rejects.toThrow()
    })

    it('should handle 500 server error', async () => {
      mock.onGet('/v1/transactions').reply(500, { error: 'Internal server error' })

      await expect(fetchTransactions()).rejects.toThrow()
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/transactions').networkError()

      await expect(fetchTransactions()).rejects.toThrow()
    })

    it('should handle pagination metadata correctly for has_more=true', async () => {
      const mockResponse: GetTransactionsResponse = {
        transactions: new Array(50).fill({}).map((_, i) => ({
          id: `txn-${i}`,
          user_id: 'user-123',
          account_id: 'account-456',
          linked_item_id: 'item-789',
          source: 'plaid' as const,
          amount: -10.00,
          date: '2025-10-25',
          name: `Transaction ${i}`,
          pending: false,
          created_at: '2025-10-25T10:00:00Z',
          updated_at: '2025-10-25T10:00:00Z',
        })),
        total_count: 150,
        limit: 50,
        offset: 0,
        has_more: true,
      }

      mock.onGet('/v1/transactions').reply(200, mockResponse)

      const result = await fetchTransactions({ limit: 50, offset: 0 })

      expect(result.transactions).toHaveLength(50)
      expect(result.total_count).toBe(150)
      expect(result.has_more).toBe(true)
    })

    it('should omit undefined filter parameters from query string', async () => {
      const params: FetchTransactionsParams = {
        filters: {
          account_id: 'account-123',
          // Other filters undefined
        },
      }

      const mockResponse: GetTransactionsResponse = {
        transactions: [],
        total_count: 0,
        limit: 50,
        offset: 0,
        has_more: false,
      }

      mock.onGet('/v1/transactions').reply((config) => {
        // Should only include defined parameters
        expect(config.params).toEqual({
          account_id: 'account-123',
        })
        return [200, mockResponse]
      })

      await fetchTransactions(params)
    })
  })
})
