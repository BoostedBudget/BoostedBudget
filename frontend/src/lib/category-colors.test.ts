/**
 * Category Colors Utility Tests
 *
 * TDD RED: Tests for category-to-color mapping.
 * Issue #531: Transaction List Visual Enhancements
 */

import { describe, it, expect } from 'vitest'
import {
  getCategoryColor,
  CATEGORY_COLORS,
  DEFAULT_CATEGORY_COLOR,
} from './category-colors'

describe('CATEGORY_COLORS', () => {
  it('should define colors for common Plaid categories', () => {
    // Verify key categories have colors defined
    expect(CATEGORY_COLORS['Food and Drink']).toBeDefined()
    expect(CATEGORY_COLORS['Shopping']).toBeDefined()
    expect(CATEGORY_COLORS['Travel']).toBeDefined()
    expect(CATEGORY_COLORS['Entertainment']).toBeDefined()
    expect(CATEGORY_COLORS['Transportation']).toBeDefined()
    expect(CATEGORY_COLORS['Healthcare']).toBeDefined()
    expect(CATEGORY_COLORS['Income']).toBeDefined()
    expect(CATEGORY_COLORS['Utilities']).toBeDefined()
    expect(CATEGORY_COLORS['Recreation']).toBeDefined()
  })

  it('should have valid hex color format for all categories', () => {
    const hexColorRegex = /^#[0-9A-Fa-f]{6}$/
    Object.entries(CATEGORY_COLORS).forEach(([_category, color]) => {
      expect(color).toMatch(hexColorRegex)
    })
  })

  it('should have at least 15 category colors defined', () => {
    expect(Object.keys(CATEGORY_COLORS).length).toBeGreaterThanOrEqual(15)
  })
})

describe('DEFAULT_CATEGORY_COLOR', () => {
  it('should be a valid hex color', () => {
    expect(DEFAULT_CATEGORY_COLOR).toMatch(/^#[0-9A-Fa-f]{6}$/)
  })

  it('should be a neutral gray color', () => {
    // #6B7280 is Tailwind gray-500
    expect(DEFAULT_CATEGORY_COLOR).toBe('#6B7280')
  })
})

describe('getCategoryColor', () => {
  it('should return predefined color for known category', () => {
    expect(getCategoryColor('Food and Drink')).toBe(CATEGORY_COLORS['Food and Drink'])
    expect(getCategoryColor('Shopping')).toBe(CATEGORY_COLORS['Shopping'])
    expect(getCategoryColor('Travel')).toBe(CATEGORY_COLORS['Travel'])
  })

  it('should be case-insensitive for category matching', () => {
    const expectedColor = CATEGORY_COLORS['Food and Drink']
    expect(getCategoryColor('food and drink')).toBe(expectedColor)
    expect(getCategoryColor('FOOD AND DRINK')).toBe(expectedColor)
    expect(getCategoryColor('Food And Drink')).toBe(expectedColor)
  })

  it('should return default color for unknown category', () => {
    expect(getCategoryColor('Random Unknown Category')).toBe(DEFAULT_CATEGORY_COLOR)
    expect(getCategoryColor('NotARealCategory')).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should return default color for null', () => {
    expect(getCategoryColor(null)).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should return default color for undefined', () => {
    expect(getCategoryColor(undefined)).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should return default color for empty string', () => {
    expect(getCategoryColor('')).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should handle whitespace-only strings', () => {
    expect(getCategoryColor('   ')).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should trim whitespace from category names', () => {
    const expectedColor = CATEGORY_COLORS['Food and Drink']
    expect(getCategoryColor('  Food and Drink  ')).toBe(expectedColor)
  })

  it('should handle Uncategorized specially', () => {
    // Uncategorized should return default color
    expect(getCategoryColor('Uncategorized')).toBe(DEFAULT_CATEGORY_COLOR)
  })

  it('should return consistent colors for the same category', () => {
    // Calling multiple times should return same result
    const color1 = getCategoryColor('Shopping')
    const color2 = getCategoryColor('Shopping')
    const color3 = getCategoryColor('Shopping')
    expect(color1).toBe(color2)
    expect(color2).toBe(color3)
  })

  it('should return different colors for different categories', () => {
    const foodColor = getCategoryColor('Food and Drink')
    const travelColor = getCategoryColor('Travel')
    const incomeColor = getCategoryColor('Income')

    expect(foodColor).not.toBe(travelColor)
    expect(travelColor).not.toBe(incomeColor)
    expect(foodColor).not.toBe(incomeColor)
  })
})
