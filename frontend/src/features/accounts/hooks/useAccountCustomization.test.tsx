/**
 * useAccountCustomization Hook Tests
 *
 * Tests for account customization operations (Issue #307)
 * Issue #171: Updated to test UserAccount endpoint for stable customization storage
 * Issue #575: Migrated from MSW to centralized API mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { useAccountCustomization } from './useAccountCustomization'
import type { Account } from '@/api/generated/models'
import {
  mockUseUpdateAccount,
  mockUpdateAccountMutateAsync,
  mockUpdateAccountReset,
  mockUseReorderAccounts,
  mockReorderAccountsMutateAsync,
  mockReorderAccountsReset,
} from '@/test/mocks/accounts-api'
import {
  mockUseUpdateUserAccount,
  mockUpdateUserAccountMutateAsync,
} from '@/test/mocks/user-accounts-api'

// Mock account data without user_account_id (legacy flow)
const mockAccountLegacy: Account = {
  id: 'acc-123',
  linked_item_id: 'item-456',
  user_id: 'user-789',
  plaid_account_id: 'plaid-acc-1',
  name: 'Chase Checking',
  official_name: 'Chase Total Checking',
  type: 'depository',
  subtype: 'checking',
  mask: '0000',
  current_balance: 2500.5,
  available_balance: 2300.5,
  currency_code: 'USD',
  institution_name: 'Chase',
  nickname: 'My Primary',
  custom_color: '#3B82F6',
  display_order: 0,
  is_hidden: false,
  status: 'active',
  created_at: '2025-10-18T14:30:00Z',
  updated_at: '2025-10-20T10:15:00Z',
}

// Mock account data with user_account_id (Issue #171 flow)
const mockAccountWithUserAccount: Account = {
  ...mockAccountLegacy,
  id: 'acc-456',
  user_account_id: 'ua-789',
}

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
}

function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useAccountCustomization', () => {
  beforeEach(() => {
    // Clear all mocks
    mockUseUpdateAccount.mockClear()
    mockUpdateAccountMutateAsync.mockClear()
    mockUpdateAccountReset.mockClear()
    mockUseReorderAccounts.mockClear()
    mockReorderAccountsMutateAsync.mockClear()
    mockReorderAccountsReset.mockClear()
    mockUseUpdateUserAccount.mockClear()
    mockUpdateUserAccountMutateAsync.mockClear()

    // Default successful responses
    mockUpdateAccountMutateAsync.mockResolvedValue({
      account: { ...mockAccountLegacy, nickname: 'Updated Name' },
    })

    mockUseUpdateAccount.mockReturnValue({
      mutateAsync: mockUpdateAccountMutateAsync,
      isPending: false,
      isError: false,
      error: null,
      reset: mockUpdateAccountReset,
    })

    mockUpdateUserAccountMutateAsync.mockResolvedValue({
      id: 'ua-789',
      nickname: 'Updated Name',
      custom_color: '#FF0000',
    })

    mockUseUpdateUserAccount.mockReturnValue({
      mutateAsync: mockUpdateUserAccountMutateAsync,
      isPending: false,
      isError: false,
      error: null,
      reset: mockUpdateAccountReset,
    })

    mockReorderAccountsMutateAsync.mockResolvedValue({ success: true })

    mockUseReorderAccounts.mockReturnValue({
      mutateAsync: mockReorderAccountsMutateAsync,
      isPending: false,
      isError: false,
      error: null,
      reset: mockReorderAccountsReset,
    })
  })

  describe('initial state', () => {
    it('should have correct initial state', () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isUpdating).toBe(false)
      expect(result.current.updateError).toBeNull()
      expect(result.current.isReordering).toBe(false)
      expect(result.current.reorderError).toBeNull()
    })
  })

  describe('updateAccount', () => {
    it('should update account via legacy Account endpoint when no user_account_id', async () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount(mockAccountLegacy, {
          nickname: 'Updated Name',
          custom_color: '#FF0000',
        })
      })

      expect(returnedAccount).not.toBeNull()
      expect(mockUpdateAccountMutateAsync).toHaveBeenCalled()
      expect(result.current.updateError).toBeNull()
    })

    it('should update account via UserAccount endpoint when user_account_id exists', async () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount(
          mockAccountWithUserAccount,
          {
            nickname: 'Updated Name',
            custom_color: '#FF0000',
          }
        )
      })

      expect(returnedAccount).not.toBeNull()
      expect(returnedAccount!.nickname).toBe('Updated Name')
      expect(returnedAccount!.custom_color).toBe('#FF0000')
      expect(mockUpdateUserAccountMutateAsync).toHaveBeenCalled()
      expect(result.current.updateError).toBeNull()
    })

    it('should set isUpdating to true during update', () => {
      mockUseUpdateAccount.mockReturnValue({
        mutateAsync: mockUpdateAccountMutateAsync,
        isPending: true,
        isError: false,
        error: null,
        reset: mockUpdateAccountReset,
      })

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isUpdating).toBe(true)
    })

    it('should set updateError on failure', async () => {
      const error = new Error('Network error')
      mockUpdateAccountMutateAsync.mockRejectedValue(error)
      mockUseUpdateAccount.mockReturnValue({
        mutateAsync: mockUpdateAccountMutateAsync,
        isPending: false,
        isError: true,
        error,
        reset: mockUpdateAccountReset,
      })

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount(mockAccountLegacy, {
          nickname: 'Test',
        })
      })

      expect(returnedAccount).toBeNull()
      expect(result.current.updateError).toBeInstanceOf(Error)
    })

    it('should convert non-Error to Error instance', () => {
      mockUseUpdateAccount.mockReturnValue({
        mutateAsync: mockUpdateAccountMutateAsync,
        isPending: false,
        isError: true,
        error: 'String error',
        reset: mockUpdateAccountReset,
      })

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      expect(result.current.updateError).toBeInstanceOf(Error)
    })
  })

  describe('reorderAccounts', () => {
    it('should reorder accounts successfully', async () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let success = false
      await act(async () => {
        success = await result.current.reorderAccounts([
          { account_id: 'acc-1', display_order: 0 },
          { account_id: 'acc-2', display_order: 1 },
          { account_id: 'acc-3', display_order: 2 },
        ])
      })

      expect(success).toBe(true)
      expect(mockReorderAccountsMutateAsync).toHaveBeenCalled()
      expect(result.current.reorderError).toBeNull()
    })

    it('should set isReordering to true during reorder', () => {
      mockUseReorderAccounts.mockReturnValue({
        mutateAsync: mockReorderAccountsMutateAsync,
        isPending: true,
        isError: false,
        error: null,
        reset: mockReorderAccountsReset,
      })

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isReordering).toBe(true)
    })

    it('should set reorderError on failure', async () => {
      const error = new Error('Reorder failed')
      mockReorderAccountsMutateAsync.mockRejectedValue(error)
      mockUseReorderAccounts.mockReturnValue({
        mutateAsync: mockReorderAccountsMutateAsync,
        isPending: false,
        isError: true,
        error,
        reset: mockReorderAccountsReset,
      })

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let success = false
      await act(async () => {
        success = await result.current.reorderAccounts([
          { account_id: 'acc-1', display_order: 0 },
        ])
      })

      expect(success).toBe(false)
      expect(result.current.reorderError).toBeInstanceOf(Error)
    })
  })

  describe('clearErrors', () => {
    it('should call reset on all mutations', () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      act(() => {
        result.current.clearErrors()
      })

      expect(mockUpdateAccountReset).toHaveBeenCalled()
      expect(mockReorderAccountsReset).toHaveBeenCalled()
    })
  })
})
