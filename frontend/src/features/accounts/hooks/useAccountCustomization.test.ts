/**
 * useAccountCustomization Hook Tests
 *
 * Tests for account customization operations (Issue #307)
 * Following TDD approach: RED tests first, then implementation
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAccountCustomization } from './useAccountCustomization'
import * as plaidApi from '../api/plaidApi'
import type { Account, UpdateAccountRequest, UpdateAccountResponse, ReorderAccountsResponse } from '../types'

// Mock the API module
vi.mock('../api/plaidApi', () => ({
  updateAccount: vi.fn(),
  reorderAccounts: vi.fn(),
}))

describe('useAccountCustomization', () => {
  const mockAccount: Account = {
    id: 'acc-123',
    linked_item_id: 'item-456',
    user_id: 'user-789',
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
    nickname: 'My Primary',
    custom_color: '#3B82F6',
    display_order: 0,
    is_hidden: false,
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('initial state', () => {
    it('should have correct initial state', () => {
      const { result } = renderHook(() => useAccountCustomization())

      expect(result.current.isUpdating).toBe(false)
      expect(result.current.updateError).toBeNull()
      expect(result.current.isReordering).toBe(false)
      expect(result.current.reorderError).toBeNull()
    })
  })

  describe('updateAccount', () => {
    it('should update account successfully', async () => {
      const updateData: UpdateAccountRequest = {
        nickname: 'Updated Name',
        custom_color: '#FF0000',
      }

      const mockResponse: UpdateAccountResponse = {
        account: { ...mockAccount, nickname: 'Updated Name', custom_color: '#FF0000' },
      }

      vi.mocked(plaidApi.updateAccount).mockResolvedValueOnce(mockResponse)

      const { result } = renderHook(() => useAccountCustomization())

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount('acc-123', updateData)
      })

      expect(plaidApi.updateAccount).toHaveBeenCalledWith('acc-123', updateData)
      expect(returnedAccount).toEqual(mockResponse.account)
      expect(result.current.updateError).toBeNull()
    })

    it('should set isUpdating to true during update', async () => {
      const updateData: UpdateAccountRequest = { nickname: 'Test' }

      let resolvePromise: (value: UpdateAccountResponse) => void
      const pendingPromise = new Promise<UpdateAccountResponse>((resolve) => {
        resolvePromise = resolve
      })
      vi.mocked(plaidApi.updateAccount).mockReturnValueOnce(pendingPromise)

      const { result } = renderHook(() => useAccountCustomization())

      // Start the update
      act(() => {
        result.current.updateAccount('acc-123', updateData)
      })

      // Check loading state is true
      expect(result.current.isUpdating).toBe(true)

      // Resolve the promise
      await act(async () => {
        resolvePromise!({ account: mockAccount })
      })

      // Check loading state is false
      expect(result.current.isUpdating).toBe(false)
    })

    it('should set updateError on failure', async () => {
      const updateData: UpdateAccountRequest = { nickname: 'Test' }
      const error = new Error('Network error')

      vi.mocked(plaidApi.updateAccount).mockRejectedValueOnce(error)

      const { result } = renderHook(() => useAccountCustomization())

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount('acc-123', updateData)
      })

      expect(returnedAccount).toBeNull()
      expect(result.current.updateError).toEqual(error)
    })

    it('should handle non-Error rejection', async () => {
      const updateData: UpdateAccountRequest = { nickname: 'Test' }

      vi.mocked(plaidApi.updateAccount).mockRejectedValueOnce('string error')

      const { result } = renderHook(() => useAccountCustomization())

      await act(async () => {
        await result.current.updateAccount('acc-123', updateData)
      })

      expect(result.current.updateError?.message).toBe('Failed to update account')
    })
  })

  describe('reorderAccounts', () => {
    it('should reorder accounts successfully', async () => {
      const accountOrders = [
        { account_id: 'acc-1', display_order: 0 },
        { account_id: 'acc-2', display_order: 1 },
        { account_id: 'acc-3', display_order: 2 },
      ]

      const mockResponse: ReorderAccountsResponse = { success: true }
      vi.mocked(plaidApi.reorderAccounts).mockResolvedValueOnce(mockResponse)

      const { result } = renderHook(() => useAccountCustomization())

      let success = false
      await act(async () => {
        success = await result.current.reorderAccounts(accountOrders)
      })

      expect(plaidApi.reorderAccounts).toHaveBeenCalledWith(accountOrders)
      expect(success).toBe(true)
      expect(result.current.reorderError).toBeNull()
    })

    it('should set isReordering to true during reorder', async () => {
      const accountOrders = [{ account_id: 'acc-1', display_order: 0 }]

      let resolvePromise: (value: ReorderAccountsResponse) => void
      const pendingPromise = new Promise<ReorderAccountsResponse>((resolve) => {
        resolvePromise = resolve
      })
      vi.mocked(plaidApi.reorderAccounts).mockReturnValueOnce(pendingPromise)

      const { result } = renderHook(() => useAccountCustomization())

      // Start the reorder
      act(() => {
        result.current.reorderAccounts(accountOrders)
      })

      // Check loading state is true
      expect(result.current.isReordering).toBe(true)

      // Resolve the promise
      await act(async () => {
        resolvePromise!({ success: true })
      })

      // Check loading state is false
      expect(result.current.isReordering).toBe(false)
    })

    it('should set reorderError on failure', async () => {
      const accountOrders = [{ account_id: 'acc-1', display_order: 0 }]
      const error = new Error('Reorder failed')

      vi.mocked(plaidApi.reorderAccounts).mockRejectedValueOnce(error)

      const { result } = renderHook(() => useAccountCustomization())

      let success = false
      await act(async () => {
        success = await result.current.reorderAccounts(accountOrders)
      })

      expect(success).toBe(false)
      expect(result.current.reorderError).toEqual(error)
    })

    it('should handle non-Error rejection', async () => {
      const accountOrders = [{ account_id: 'acc-1', display_order: 0 }]

      vi.mocked(plaidApi.reorderAccounts).mockRejectedValueOnce('string error')

      const { result } = renderHook(() => useAccountCustomization())

      await act(async () => {
        await result.current.reorderAccounts(accountOrders)
      })

      expect(result.current.reorderError?.message).toBe('Failed to reorder accounts')
    })
  })

  describe('clearErrors', () => {
    it('should clear update error', async () => {
      const updateData: UpdateAccountRequest = { nickname: 'Test' }
      vi.mocked(plaidApi.updateAccount).mockRejectedValueOnce(new Error('Error'))

      const { result } = renderHook(() => useAccountCustomization())

      await act(async () => {
        await result.current.updateAccount('acc-123', updateData)
      })

      expect(result.current.updateError).not.toBeNull()

      act(() => {
        result.current.clearErrors()
      })

      expect(result.current.updateError).toBeNull()
    })

    it('should clear reorder error', async () => {
      const accountOrders = [{ account_id: 'acc-1', display_order: 0 }]
      vi.mocked(plaidApi.reorderAccounts).mockRejectedValueOnce(new Error('Error'))

      const { result } = renderHook(() => useAccountCustomization())

      await act(async () => {
        await result.current.reorderAccounts(accountOrders)
      })

      expect(result.current.reorderError).not.toBeNull()

      act(() => {
        result.current.clearErrors()
      })

      expect(result.current.reorderError).toBeNull()
    })

    it('should clear both errors at once', async () => {
      vi.mocked(plaidApi.updateAccount).mockRejectedValueOnce(new Error('Error'))
      vi.mocked(plaidApi.reorderAccounts).mockRejectedValueOnce(new Error('Error'))

      const { result } = renderHook(() => useAccountCustomization())

      await act(async () => {
        await result.current.updateAccount('acc-123', { nickname: 'Test' })
        await result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      expect(result.current.updateError).not.toBeNull()
      expect(result.current.reorderError).not.toBeNull()

      act(() => {
        result.current.clearErrors()
      })

      expect(result.current.updateError).toBeNull()
      expect(result.current.reorderError).toBeNull()
    })
  })
})
