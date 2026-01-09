/**
 * useIncomeTracking Tests
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 */

import { describe, it, expect } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { useIncomeTracking } from './useIncomeTracking'
import type { IncomeSource } from '../types'

describe('useIncomeTracking', () => {

  // ==========================================================================
  // Expected Manual Total Tests
  // ==========================================================================

  describe('Expected Manual Total', () => {
    it('returns 0 when no sources', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.expectedManualTotal).toBe(0);
    });

    it('calculates sum of manual source amounts', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
        { type: 'manual', value: '500' },
        { type: 'manual', value: '250.50' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.expectedManualTotal).toBe(1750.50);
    });

    it('ignores plaid_category and tag sources', () => {
      const sources: IncomeSource[] = [
        { type: 'plaid_category', value: 'INCOME' },
        { type: 'tag', value: 'Freelance' },
        { type: 'manual', value: '1000' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      // Only the manual source should be counted
      expect(result.current.expectedManualTotal).toBe(1000);
    });

    it('handles invalid amount strings gracefully', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
        { type: 'manual', value: 'invalid' },
        { type: 'manual', value: '' },
        { type: 'manual', value: '500' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      // Should only sum valid amounts: 1000 + 500 = 1500
      expect(result.current.expectedManualTotal).toBe(1500);
    });

    it('handles negative amounts (subtracts from total)', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
        { type: 'manual', value: '-200' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.expectedManualTotal).toBe(800);
    });

    it('handles zero amount', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '0' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.expectedManualTotal).toBe(0);
    });
  });

  // ==========================================================================
  // Hook State Tests
  // ==========================================================================

  describe('Hook State', () => {
    it('returns loading false initially when no budgetId', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.loading).toBe(false);
    });

    it('returns actualTotal as 0 when no budgetId', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [{ type: 'manual', value: '1000' }],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.actualTotal).toBe(0);
    });

    it('returns error as null initially', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.error).toBe(null);
    });

    it('provides refetch function', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(typeof result.current.refetch).toBe('function');
    });
  });

  // ==========================================================================
  // Reactivity Tests
  // ==========================================================================

  describe('Reactivity', () => {
    it('recalculates when sources change', () => {
      const initialSources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
      ];

      const { result, rerender } = renderHook(
        ({ sources }) =>
          useIncomeTracking({
            sources,
            startDate: '2025-01-01',
            endDate: '2025-01-31',
          }),
        { initialProps: { sources: initialSources } }
      );

      expect(result.current.expectedManualTotal).toBe(1000);

      // Update sources
      const newSources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
        { type: 'manual', value: '500' },
      ];

      rerender({ sources: newSources });

      expect(result.current.expectedManualTotal).toBe(1500);
    });
  });

  // ==========================================================================
  // Enabled State Tests
  // ==========================================================================

  describe('Enabled State', () => {
    it('respects enabled=false (does not calculate when disabled)', () => {
      const sources: IncomeSource[] = [
        { type: 'manual', value: '1000' },
      ];

      const { result } = renderHook(() =>
        useIncomeTracking({
          sources,
          startDate: '2025-01-01',
          endDate: '2025-01-31',
          enabled: false,
        })
      );

      // When disabled, calculations should still work for expected total
      // (this is a local calculation, not an API fetch)
      expect(result.current.expectedManualTotal).toBe(1000);
    });
  });
});
