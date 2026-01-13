/**
 * useAutoCategorizationRules Hook Tests
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #575: Migrated to bun:test with centralized API mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useAutoCategorizationRules } from './useAutoCategorizationRules'
import type { CategorizationRule } from '../types'
import {
  mockUseGetUserRules,
  mockRefetchUserRules,
  mockCreateRuleMutateAsync,
  mockUpdateRuleMutateAsync,
  mockDeleteRuleMutateAsync,
  mockToggleRuleMutateAsync,
} from '@/test/mocks/auto-categorization-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useAutoCategorizationRules', () => {
  const mockRule1: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    condition_logic: 'AND',
    target_category: ['Food & Drink', 'Coffee'],
    target_tags: ['morning'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  const mockRule2: CategorizationRule = {
    ...mockRule1,
    id: 'rule-2',
    name: 'Groceries',
    priority: 5,
    created_at: '2025-01-02T00:00:00Z',
  }

  beforeEach(() => {
    mockUseGetUserRules.mockClear()
    mockRefetchUserRules.mockClear()
    mockCreateRuleMutateAsync.mockClear()
    mockUpdateRuleMutateAsync.mockClear()
    mockDeleteRuleMutateAsync.mockClear()
    mockToggleRuleMutateAsync.mockClear()
  })

  function configureQuery(config: {
    data?: CategorizationRule[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetUserRules.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchUserRules,
    })
  }

  describe('Fetching', () => {
    it('should return loading=true initially', () => {
      configureQuery({ isLoading: true })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
    })

    it('should fetch rules and sort by priority (highest first)', async () => {
      configureQuery({ data: [mockRule2, mockRule1] })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Rule 1 has priority 10, Rule 2 has priority 5
      expect(result.current.rules[0].id).toBe('rule-1')
      expect(result.current.rules[1].id).toBe('rule-2')
      expect(result.current.error).toBeNull()
    })

    it('should handle empty rules', async () => {
      configureQuery({ data: [] })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.rules).toHaveLength(0)
      expect(result.current.error).toBeNull()
    })

    it('should handle fetch errors', async () => {
      configureQuery({ error: new Error('Network error') })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBe('Network error')
      expect(result.current.rules).toHaveLength(0)
    })
  })

  describe('Create Rule', () => {
    it('should create a new rule', async () => {
      configureQuery({ data: [] })
      const newRule = { ...mockRule1, id: 'new-rule' }
      mockCreateRuleMutateAsync.mockResolvedValue(newRule)

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        const created = await result.current.createRule({
          name: 'Coffee Shops',
          description: 'Categorize coffee purchases',
          priority: 10,
          conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
          condition_logic: 'AND',
          target_category: ['Food & Drink', 'Coffee'],
          target_tags: ['morning'],
          tag_behavior: 'add',
        })
        expect(created.id).toBe('new-rule')
      })

      expect(mockCreateRuleMutateAsync).toHaveBeenCalled()
    })
  })

  describe('Update Rule', () => {
    it('should update an existing rule', async () => {
      configureQuery({ data: [mockRule1] })
      const updatedRule = { ...mockRule1, name: 'Updated Name' }
      mockUpdateRuleMutateAsync.mockResolvedValue(updatedRule)

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        const updated = await result.current.updateRule('rule-1', {
          name: 'Updated Name',
        })
        expect(updated.name).toBe('Updated Name')
      })

      expect(mockUpdateRuleMutateAsync).toHaveBeenCalledWith({
        id: 'rule-1',
        data: { name: 'Updated Name' },
      })
    })
  })

  describe('Delete Rule', () => {
    it('should delete a rule', async () => {
      configureQuery({ data: [mockRule1, mockRule2] })
      mockDeleteRuleMutateAsync.mockResolvedValue({})

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.deleteRule('rule-1')
      })

      expect(mockDeleteRuleMutateAsync).toHaveBeenCalledWith({ id: 'rule-1' })
    })
  })

  describe('Toggle Rule', () => {
    it('should toggle rule enabled status', async () => {
      configureQuery({ data: [mockRule1] })
      const toggledRule = { ...mockRule1, enabled: false }
      mockToggleRuleMutateAsync.mockResolvedValue(toggledRule)

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        const toggled = await result.current.toggleRule('rule-1', false)
        expect(toggled.enabled).toBe(false)
      })

      expect(mockToggleRuleMutateAsync).toHaveBeenCalledWith({
        id: 'rule-1',
        data: { enabled: false },
      })
    })
  })

  describe('Refresh', () => {
    it('should refresh rules when called', async () => {
      configureQuery({ data: [mockRule1] })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refresh()
      })

      expect(mockRefetchUserRules).toHaveBeenCalled()
    })
  })

  describe('Sorting', () => {
    it('should sort rules with same priority by created_at (newer first)', async () => {
      const rule1 = {
        ...mockRule1,
        priority: 10,
        created_at: '2025-01-01T00:00:00Z',
      }
      const rule2 = {
        ...mockRule1,
        id: 'rule-2',
        priority: 10,
        created_at: '2025-01-02T00:00:00Z',
      }

      configureQuery({ data: [rule1, rule2] })

      const { result } = renderHook(() => useAutoCategorizationRules(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Rule 2 was created later, should be first
      expect(result.current.rules[0].id).toBe('rule-2')
      expect(result.current.rules[1].id).toBe('rule-1')
    })
  })
})
