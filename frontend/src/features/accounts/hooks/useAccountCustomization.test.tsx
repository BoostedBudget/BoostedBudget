/**
 * useAccountCustomization Hook Tests
 *
 * Tests for account customization operations (Issue #307)
 * Issue #171: Updated to test UserAccount endpoint for stable customization storage
 * Uses MSW for API mocking with React Query.
 */

import { describe, it, expect, afterEach, afterAll, beforeAll } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import React from 'react'
import { useAccountCustomization } from './useAccountCustomization'
import type { Account } from '@/api/generated/models'

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
  current_balance: 2500.50,
  available_balance: 2300.50,
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

// MSW server setup
const server = setupServer(
  http.patch('*/api/v1/accounts/reorder', () => {
    return HttpResponse.json({ success: true })
  }),
  http.patch('*/api/v1/accounts/:id', () => {
    return HttpResponse.json({ account: mockAccountLegacy })
  }),
  // Issue #171: UserAccount endpoint for stable customization storage
  http.patch('*/api/v1/user-accounts/:id', () => {
    return HttpResponse.json({
      id: 'ua-789',
      nickname: 'Updated Name',
      custom_color: '#FF0000',
    })
  })
)

// Create a fresh QueryClient for each test
function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
      },
      mutations: {
        retry: false,
      },
    },
  })
}

// Wrapper component for tests
function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )
  }
}

describe('useAccountCustomization', () => {
  beforeAll(() => {
    server.listen({ onUnhandledRequest: 'error' })
  })

  afterEach(() => {
    server.resetHandlers()
  })

  afterAll(() => {
    server.close()
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
      const updatedAccount = { ...mockAccountLegacy, nickname: 'Updated Name', custom_color: '#FF0000' }
      server.use(
        http.patch('*/api/v1/accounts/:id', () => {
          return HttpResponse.json({ account: updatedAccount })
        })
      )

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

      expect(returnedAccount).toEqual(updatedAccount)
      expect(result.current.updateError).toBeNull()
    })

    it('should update account via UserAccount endpoint when user_account_id exists (Issue #171)', async () => {
      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount(mockAccountWithUserAccount, {
          nickname: 'Updated Name',
          custom_color: '#FF0000',
        })
      })

      // Should return account with updated values
      expect(returnedAccount).not.toBeNull()
      expect(returnedAccount?.nickname).toBe('Updated Name')
      expect(returnedAccount?.custom_color).toBe('#FF0000')
      expect(result.current.updateError).toBeNull()
    })

    it('should set isUpdating to true during update', async () => {
      server.use(
        http.patch('*/api/v1/accounts/:id', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100))
          return HttpResponse.json({ account: mockAccountLegacy })
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      // Start the update
      act(() => {
        result.current.updateAccount(mockAccountLegacy, { nickname: 'Test' })
      })

      // Check loading state is true
      await waitFor(() => {
        expect(result.current.isUpdating).toBe(true)
      })

      // Wait for completion
      await waitFor(() => {
        expect(result.current.isUpdating).toBe(false)
      })
    })

    it('should set updateError on failure', async () => {
      server.use(
        http.patch('*/api/v1/accounts/:id', () => {
          return HttpResponse.json(
            { message: 'Network error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let returnedAccount: Account | null = null
      await act(async () => {
        returnedAccount = await result.current.updateAccount(mockAccountLegacy, { nickname: 'Test' })
      })

      expect(returnedAccount).toBeNull()
      // Wait for React to update state after mutation error
      await waitFor(() => {
        expect(result.current.updateError).toBeInstanceOf(Error)
      })
    })

    it('should convert API errors to Error instances', async () => {
      server.use(
        http.patch('*/api/v1/accounts/:id', () => {
          return HttpResponse.json(
            { message: 'String error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.updateAccount(mockAccountLegacy, { nickname: 'Test' })
      })

      // Wait for React to update state after mutation error
      await waitFor(() => {
        expect(result.current.updateError).toBeInstanceOf(Error)
      })
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
      expect(result.current.reorderError).toBeNull()
    })

    it('should set isReordering to true during reorder', async () => {
      server.use(
        http.patch('*/api/v1/accounts/reorder', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100))
          return HttpResponse.json({ success: true })
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      // Start the reorder
      act(() => {
        result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      // Check loading state is true
      await waitFor(() => {
        expect(result.current.isReordering).toBe(true)
      })

      // Wait for completion
      await waitFor(() => {
        expect(result.current.isReordering).toBe(false)
      })
    })

    it('should set reorderError on failure', async () => {
      server.use(
        http.patch('*/api/v1/accounts/reorder', () => {
          return HttpResponse.json(
            { message: 'Reorder failed' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      let success = false
      await act(async () => {
        success = await result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      expect(success).toBe(false)
      // Wait for React to update state after mutation error
      await waitFor(() => {
        expect(result.current.reorderError).toBeInstanceOf(Error)
      })
    })

    it('should convert API errors to Error instances', async () => {
      server.use(
        http.patch('*/api/v1/accounts/reorder', () => {
          return HttpResponse.json(
            { message: 'String error' },
            { status: 500 }
          )
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      // Wait for React to update state after mutation error
      await waitFor(() => {
        expect(result.current.reorderError).toBeInstanceOf(Error)
      })
    })
  })

  describe('clearErrors', () => {
    it('should clear update error', async () => {
      server.use(
        http.patch('*/api/v1/accounts/:id', () => {
          return HttpResponse.json({ message: 'Error' }, { status: 500 })
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.updateAccount(mockAccountLegacy, { nickname: 'Test' })
      })

      // Wait for error state to be set
      await waitFor(() => {
        expect(result.current.updateError).not.toBeNull()
      })

      await act(async () => {
        result.current.clearErrors()
      })

      // Wait for error to be cleared
      await waitFor(() => {
        expect(result.current.updateError).toBeNull()
      })
    })

    it('should clear reorder error', async () => {
      server.use(
        http.patch('*/api/v1/accounts/reorder', () => {
          return HttpResponse.json({ message: 'Error' }, { status: 500 })
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      // Wait for error state to be set
      await waitFor(() => {
        expect(result.current.reorderError).not.toBeNull()
      })

      await act(async () => {
        result.current.clearErrors()
      })

      // Wait for error to be cleared
      await waitFor(() => {
        expect(result.current.reorderError).toBeNull()
      })
    })

    it('should clear both errors at once', async () => {
      server.use(
        http.patch('*/api/v1/accounts/:id', () => {
          return HttpResponse.json({ message: 'Error' }, { status: 500 })
        }),
        http.patch('*/api/v1/accounts/reorder', () => {
          return HttpResponse.json({ message: 'Error' }, { status: 500 })
        })
      )

      const { result } = renderHook(() => useAccountCustomization(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.updateAccount(mockAccountLegacy, { nickname: 'Test' })
        await result.current.reorderAccounts([{ account_id: 'acc-1', display_order: 0 }])
      })

      // Wait for error states to be set
      await waitFor(() => {
        expect(result.current.updateError).not.toBeNull()
        expect(result.current.reorderError).not.toBeNull()
      })

      await act(async () => {
        result.current.clearErrors()
      })

      // Wait for errors to be cleared
      await waitFor(() => {
        expect(result.current.updateError).toBeNull()
        expect(result.current.reorderError).toBeNull()
      })
    })
  })
})
