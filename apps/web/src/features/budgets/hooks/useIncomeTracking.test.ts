import { describe, it, expect } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { useIncomeTracking } from './useIncomeTracking'
import type { IncomeSource } from '../types'

describe('useIncomeTracking', () => {
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

    it('ignores pfc_primary and tag sources', () => {
      const sources: IncomeSource[] = [
        { type: 'pfc_primary', value: 'INCOME' },
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

    // Issue #1048: loading/error/refetch are dead fields now that actualTotal
    // is derived directly from budgetIncomeAmount — no API call needed.
    it('does not expose loading, error, or refetch on the result (dead fields removed)', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect('loading' in result.current).toBe(false);
      expect('error' in result.current).toBe(false);
      expect('refetch' in result.current).toBe(false);
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
  // expectedTotal from budget fields
  // ==========================================================================

  describe('expectedTotal from budget fields', () => {
    it('uses budget.expected_income when provided', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
          budgetExpectedIncome: 3000,
          budgetAmount: 5000,
          budgetIncomeAmount: 0,
        })
      );

      expect(result.current.expectedTotal).toBe(3000);
    });

    it('falls back to budget.amount when expected_income is undefined', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
          budgetAmount: 5000,
          budgetIncomeAmount: 0,
        })
      );

      expect(result.current.expectedTotal).toBe(5000);
    });

    it('returns 0 for expectedTotal when neither expected_income nor amount provided', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
        })
      );

      expect(result.current.expectedTotal).toBe(0);
    });

    it('uses budget.income_amount as actualTotal', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
          budgetIncomeAmount: 1500,
          budgetExpectedIncome: 3000,
          budgetAmount: 9999,
        })
      );

      expect(result.current.actualTotal).toBe(1500);
    });

    it('returns 0 for actualTotal when budget.income_amount is undefined', () => {
      const { result } = renderHook(() =>
        useIncomeTracking({
          sources: [],
          startDate: '2025-01-01',
          endDate: '2025-01-31',
          budgetExpectedIncome: 3000,
        })
      );

      expect(result.current.actualTotal).toBe(0);
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
