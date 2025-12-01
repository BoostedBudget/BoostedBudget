import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useRules } from './useRules'
import * as rulesApi from '../api/rulesApi'
import type { CategorizationRule } from '../types'

// Mock the API module
vi.mock('../api/rulesApi')

describe('useRules', () => {
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
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('Fetching', () => {
    it('should fetch rules on mount', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule1])

      const { result } = renderHook(() => useRules())

      expect(result.current.loading).toBe(true)

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(rulesApi.fetchRules).toHaveBeenCalledTimes(1)
      expect(result.current.rules).toHaveLength(1)
      expect(result.current.rules[0].id).toBe('rule-1')
      expect(result.current.error).toBeNull()
    })

    it('should sort rules by priority (highest first)', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule2, mockRule1])

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Rule 1 has priority 10, Rule 2 has priority 5
      expect(result.current.rules[0].id).toBe('rule-1')
      expect(result.current.rules[1].id).toBe('rule-2')
    })

    it('should handle empty rules', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([])

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.rules).toEqual([])
    })

    it('should handle fetch error', async () => {
      vi.mocked(rulesApi.fetchRules).mockRejectedValue(new Error('Network error'))

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.error).toBe('Network error')
      expect(result.current.rules).toEqual([])
    })
  })

  describe('Create Rule', () => {
    it('should create a new rule and add it to the list', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule2])
      vi.mocked(rulesApi.createRule).mockResolvedValue(mockRule1)

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let createdRule: CategorizationRule | undefined
      await act(async () => {
        createdRule = await result.current.createRule({
          name: 'Coffee Shops',
          conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
          target_category: ['Food & Drink', 'Coffee'],
        })
      })

      expect(createdRule).toEqual(mockRule1)
      expect(result.current.rules).toHaveLength(2)
      // New rule should be first (higher priority)
      expect(result.current.rules[0].id).toBe('rule-1')
    })
  })

  describe('Update Rule', () => {
    it('should update a rule in the list', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule1])
      const updatedRule = { ...mockRule1, name: 'Updated Name' }
      vi.mocked(rulesApi.updateRule).mockResolvedValue(updatedRule)

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let returnedRule: CategorizationRule | undefined
      await act(async () => {
        returnedRule = await result.current.updateRule('rule-1', { name: 'Updated Name' })
      })

      expect(returnedRule?.name).toBe('Updated Name')
      expect(result.current.rules[0].name).toBe('Updated Name')
    })

    it('should re-sort after priority change', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule1, mockRule2])
      const updatedRule2 = { ...mockRule2, priority: 20 }
      vi.mocked(rulesApi.updateRule).mockResolvedValue(updatedRule2)

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      // Initially rule-1 is first (priority 10 > 5)
      expect(result.current.rules[0].id).toBe('rule-1')

      await act(async () => {
        await result.current.updateRule('rule-2', { priority: 20 })
      })

      // After update, rule-2 should be first (priority 20 > 10)
      expect(result.current.rules[0].id).toBe('rule-2')
    })
  })

  describe('Delete Rule', () => {
    it('should delete a rule from the list', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule1, mockRule2])
      vi.mocked(rulesApi.deleteRule).mockResolvedValue(undefined)

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.rules).toHaveLength(2)

      await act(async () => {
        await result.current.deleteRule('rule-1')
      })

      expect(rulesApi.deleteRule).toHaveBeenCalledWith('rule-1')
      expect(result.current.rules).toHaveLength(1)
      expect(result.current.rules[0].id).toBe('rule-2')
    })
  })

  describe('Toggle Rule', () => {
    it('should toggle a rule enabled status', async () => {
      vi.mocked(rulesApi.fetchRules).mockResolvedValue([mockRule1])
      const disabledRule = { ...mockRule1, enabled: false }
      vi.mocked(rulesApi.toggleRule).mockResolvedValue(disabledRule)

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.rules[0].enabled).toBe(true)

      await act(async () => {
        await result.current.toggleRule('rule-1', false)
      })

      expect(rulesApi.toggleRule).toHaveBeenCalledWith('rule-1', false)
      expect(result.current.rules[0].enabled).toBe(false)
    })
  })

  describe('Refresh', () => {
    it('should refetch rules', async () => {
      vi.mocked(rulesApi.fetchRules)
        .mockResolvedValueOnce([mockRule1])
        .mockResolvedValueOnce([mockRule1, mockRule2])

      const { result } = renderHook(() => useRules())

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.rules).toHaveLength(1)

      await act(async () => {
        await result.current.refresh()
      })

      expect(rulesApi.fetchRules).toHaveBeenCalledTimes(2)
      expect(result.current.rules).toHaveLength(2)
    })
  })
})
