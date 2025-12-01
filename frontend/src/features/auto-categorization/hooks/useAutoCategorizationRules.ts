/**
 * useAutoCategorizationRules Hook
 *
 * React hook for managing auto-categorization rules - fetching, creating, updating, deleting, and toggling.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState, useEffect, useCallback } from 'react'
import {
  fetchAutoCategorizationRules,
  createAutoCategorizationRule as apiCreateRule,
  updateAutoCategorizationRule as apiUpdateRule,
  deleteAutoCategorizationRule as apiDeleteRule,
  toggleAutoCategorizationRule as apiToggleRule,
} from '../api/autoCategorizationApi'
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  UseAutoCategorizationRulesResult,
} from '../types'

/**
 * Hook for managing auto-categorization rules
 *
 * Features:
 * - Fetches rules on mount
 * - CRUD operations with optimistic updates
 * - Toggle enabled/disabled status
 * - Manual refresh
 * - Error handling
 *
 * @returns Object containing rules, loading state, error, and CRUD methods
 *
 * @example
 * ```tsx
 * function AutoCategorizationRulesList() {
 *   const {
 *     rules,
 *     loading,
 *     error,
 *     createRule,
 *     updateRule,
 *     deleteRule,
 *     toggleRule,
 *     refresh
 *   } = useAutoCategorizationRules()
 *
 *   if (loading) return <LoadingSpinner />
 *   if (error) return <ErrorMessage error={error} onRetry={refresh} />
 *
 *   return (
 *     <>
 *       {rules.map(rule => (
 *         <AutoCategorizationRuleCard
 *           key={rule.id}
 *           rule={rule}
 *           onToggle={(enabled) => toggleRule(rule.id, enabled)}
 *           onDelete={() => deleteRule(rule.id)}
 *         />
 *       ))}
 *     </>
 *   )
 * }
 * ```
 */
export function useAutoCategorizationRules(): UseAutoCategorizationRulesResult {
  const [rules, setRules] = useState<CategorizationRule[]>([])
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<string | null>(null)

  // Load rules function
  const loadRules = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const result = await fetchAutoCategorizationRules()
      // Sort by priority (higher first), then by created_at (newer first)
      const sorted = result.sort((a, b) => {
        if (b.priority !== a.priority) {
          return b.priority - a.priority
        }
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })
      setRules(sorted)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to fetch rules'
      setError(message)
      setRules([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Create a new rule
  const createRule = useCallback(async (request: CreateRuleRequest): Promise<CategorizationRule> => {
    const newRule = await apiCreateRule(request)
    setRules((prev) => {
      const updated = [newRule, ...prev]
      // Re-sort after adding
      return updated.sort((a, b) => {
        if (b.priority !== a.priority) {
          return b.priority - a.priority
        }
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })
    })
    return newRule
  }, [])

  // Update an existing rule
  const updateRule = useCallback(
    async (ruleId: string, request: UpdateRuleRequest): Promise<CategorizationRule> => {
      const updatedRule = await apiUpdateRule(ruleId, request)
      setRules((prev) => {
        const updated = prev.map((rule) => (rule.id === ruleId ? updatedRule : rule))
        // Re-sort in case priority changed
        return updated.sort((a, b) => {
          if (b.priority !== a.priority) {
            return b.priority - a.priority
          }
          return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        })
      })
      return updatedRule
    },
    []
  )

  // Delete a rule
  const deleteRule = useCallback(async (ruleId: string): Promise<void> => {
    await apiDeleteRule(ruleId)
    setRules((prev) => prev.filter((rule) => rule.id !== ruleId))
  }, [])

  // Toggle rule enabled status
  const toggleRule = useCallback(
    async (ruleId: string, enabled: boolean): Promise<CategorizationRule> => {
      const updatedRule = await apiToggleRule(ruleId, enabled)
      setRules((prev) => prev.map((rule) => (rule.id === ruleId ? updatedRule : rule)))
      return updatedRule
    },
    []
  )

  // Refresh rules
  const refresh = useCallback(async (): Promise<void> => {
    await loadRules()
  }, [loadRules])

  // Load on mount
  useEffect(() => {
    loadRules()
  }, [loadRules])

  return {
    rules,
    loading,
    error,
    createRule,
    updateRule,
    deleteRule,
    toggleRule,
    refresh,
  }
}
