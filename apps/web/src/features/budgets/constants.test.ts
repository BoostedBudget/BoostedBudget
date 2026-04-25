/**
 * Tests for Budgets Feature Constants
 * Issue #1051: Budgets Table Navigation
 */

import { describe, it, expect } from 'bun:test'
import { STORAGE_KEY_LIST_VIEW, getBudgetDetailPath } from './constants'

describe('budgets constants', () => {
  describe('STORAGE_KEY_LIST_VIEW', () => {
    it('has the correct value', () => {
      expect(STORAGE_KEY_LIST_VIEW).toBe('budgets-list-view')
    })
  })

  describe('getBudgetDetailPath', () => {
    it('returns the correct path for a given id', () => {
      expect(getBudgetDetailPath('abc-123')).toBe('/budgets/abc-123')
    })

    it('handles UUID-style ids', () => {
      const uuid = '550e8400-e29b-41d4-a716-446655440000'
      expect(getBudgetDetailPath(uuid)).toBe(`/budgets/${uuid}`)
    })

    it('includes the leading slash', () => {
      expect(getBudgetDetailPath('test-id')).toMatch(/^\/budgets\//)
    })
  })
})
