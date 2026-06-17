/**
 * normalizeWidgetSettings tests
 * Issue #1297: Investment widgets — per-widget settings persistence
 *
 * normalizeWidgetSettings is a boundary function that converts
 * the opaque {[key: string]: unknown} from the backend into a typed
 * WidgetSettings value, tolerating missing/null/malformed input.
 */

import { describe, it, expect } from 'bun:test'
import { normalizeWidgetSettings } from './settings'

describe('normalizeWidgetSettings', () => {
  describe('portfolio-holdings', () => {
    it('returns defaults when settings is undefined', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', undefined)
      expect(result).toEqual({ topN: 5, sortBy: 'value' })
    })

    it('returns defaults when settings is null', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', null as never)
      expect(result).toEqual({ topN: 5, sortBy: 'value' })
    })

    it('returns defaults when settings is empty object', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', {})
      expect(result).toEqual({ topN: 5, sortBy: 'value' })
    })

    it('returns typed settings for valid topN and sortBy', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', {
        topN: 10,
        sortBy: 'day_change',
      })
      expect(result).toEqual({ topN: 10, sortBy: 'day_change' })
    })

    it('accepts topN 5, 10, 20', () => {
      expect(normalizeWidgetSettings('portfolio-holdings', { topN: 5 }).topN).toBe(5)
      expect(normalizeWidgetSettings('portfolio-holdings', { topN: 10 }).topN).toBe(10)
      expect(normalizeWidgetSettings('portfolio-holdings', { topN: 20 }).topN).toBe(20)
    })

    it('falls back to default topN for invalid values', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', { topN: 99 })
      expect(result.topN).toBe(5)
    })

    it('falls back to default topN for string topN', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', { topN: 'bad' })
      expect(result.topN).toBe(5)
    })

    it('accepts sortBy "value" and "day_change"', () => {
      expect(normalizeWidgetSettings('portfolio-holdings', { sortBy: 'value' }).sortBy).toBe('value')
      expect(normalizeWidgetSettings('portfolio-holdings', { sortBy: 'day_change' }).sortBy).toBe('day_change')
    })

    it('falls back to default sortBy for invalid values', () => {
      const result = normalizeWidgetSettings('portfolio-holdings', { sortBy: 'invalid' })
      expect(result.sortBy).toBe('value')
    })

    it('does not crash on scalar settings (string)', () => {
      expect(() =>
        normalizeWidgetSettings('portfolio-holdings', 'bad' as never)
      ).not.toThrow()
      const result = normalizeWidgetSettings('portfolio-holdings', 'bad' as never)
      expect(result).toEqual({ topN: 5, sortBy: 'value' })
    })

    it('does not crash on array settings', () => {
      expect(() =>
        normalizeWidgetSettings('portfolio-holdings', [1, 2] as never)
      ).not.toThrow()
      const result = normalizeWidgetSettings('portfolio-holdings', [1, 2] as never)
      expect(result).toEqual({ topN: 5, sortBy: 'value' })
    })
  })

  describe('asset-allocation', () => {
    it('returns defaults when settings is undefined', () => {
      const result = normalizeWidgetSettings('asset-allocation', undefined)
      expect(result).toEqual({ groupBy: 'security_type' })
    })

    it('returns defaults when settings is null', () => {
      const result = normalizeWidgetSettings('asset-allocation', null as never)
      expect(result).toEqual({ groupBy: 'security_type' })
    })

    it('accepts valid groupBy values', () => {
      const vals = ['security_type', 'asset_class', 'account'] as const
      for (const v of vals) {
        const result = normalizeWidgetSettings('asset-allocation', { groupBy: v })
        expect(result.groupBy).toBe(v)
      }
    })

    it('falls back to default groupBy for invalid value', () => {
      const result = normalizeWidgetSettings('asset-allocation', { groupBy: 'bad' })
      expect(result.groupBy).toBe('security_type')
    })
  })

  describe('portfolio-summary (no configurable settings)', () => {
    it('returns empty object for undefined', () => {
      const result = normalizeWidgetSettings('portfolio-summary', undefined)
      expect(result).toEqual({})
    })

    it('returns empty object regardless of input', () => {
      const result = normalizeWidgetSettings('portfolio-summary', { anything: 'ignored' })
      expect(result).toEqual({})
    })
  })

  describe('widgets with no settings (passthrough to empty)', () => {
    it('returns empty object for any unrecognized widget type', () => {
      const result = normalizeWidgetSettings('balance-overview', { whatever: 1 })
      expect(result).toEqual({})
    })
  })
})
