/**
 * Category Utilities Tests
 *
 * TDD RED: Tests for Plaid category formatting utilities.
 * Issue #402: Fix Category Dropdowns in Auto-Categorization Rule Editor
 */

import { describe, it, expect } from 'bun:test'
import { formatPlaidCategory, toPlaidCategoryEnum } from './categoryUtils'

describe('formatPlaidCategory', () => {
  it('should convert FOOD_AND_DRINK to Food And Drink', () => {
    expect(formatPlaidCategory('FOOD_AND_DRINK')).toBe('Food And Drink')
  })

  it('should convert GENERAL_MERCHANDISE to General Merchandise', () => {
    expect(formatPlaidCategory('GENERAL_MERCHANDISE')).toBe('General Merchandise')
  })

  it('should convert TRANSPORTATION to Transportation', () => {
    expect(formatPlaidCategory('TRANSPORTATION')).toBe('Transportation')
  })

  it('should convert HOME_IMPROVEMENT to Home Improvement', () => {
    expect(formatPlaidCategory('HOME_IMPROVEMENT')).toBe('Home Improvement')
  })

  it('should handle single word categories', () => {
    expect(formatPlaidCategory('SHOPPING')).toBe('Shopping')
    expect(formatPlaidCategory('TRAVEL')).toBe('Travel')
    expect(formatPlaidCategory('INCOME')).toBe('Income')
  })

  it('should handle empty string', () => {
    expect(formatPlaidCategory('')).toBe('')
  })

  it('should handle categories with multiple underscores', () => {
    expect(formatPlaidCategory('RENT_AND_UTILITIES')).toBe('Rent And Utilities')
  })
})

describe('toPlaidCategoryEnum', () => {
  it('should convert Food And Drink to FOOD_AND_DRINK', () => {
    expect(toPlaidCategoryEnum('Food And Drink')).toBe('FOOD_AND_DRINK')
  })

  it('should convert General Merchandise to GENERAL_MERCHANDISE', () => {
    expect(toPlaidCategoryEnum('General Merchandise')).toBe('GENERAL_MERCHANDISE')
  })

  it('should handle single word categories', () => {
    expect(toPlaidCategoryEnum('Shopping')).toBe('SHOPPING')
    expect(toPlaidCategoryEnum('Travel')).toBe('TRAVEL')
  })

  it('should handle empty string', () => {
    expect(toPlaidCategoryEnum('')).toBe('')
  })

  it('should handle categories with multiple words', () => {
    expect(toPlaidCategoryEnum('Rent And Utilities')).toBe('RENT_AND_UTILITIES')
  })
})
