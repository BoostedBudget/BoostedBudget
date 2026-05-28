/**
 * pfcOptions utility tests
 * Issue #1200: Multi-Level Category Hierarchy
 */

import { describe, it, expect } from 'bun:test'
import { getDetailedLabel, getPrimaryLabel, useCuratedPfcDetailed } from './pfcOptions'

describe('getPrimaryLabel', () => {
  it('returns the curated label with ampersand for Food & Drink', () => {
    expect(getPrimaryLabel('FOOD_AND_DRINK')).toBe('Food & Drink')
  })

  it('returns the curated label for Rent & Utilities', () => {
    expect(getPrimaryLabel('RENT_AND_UTILITIES')).toBe('Rent & Utilities')
  })

  it('returns the curated label for a single-word primary', () => {
    expect(getPrimaryLabel('TRANSPORTATION')).toBe('Transportation')
  })

  it('returns null for an unknown primary value', () => {
    expect(getPrimaryLabel('UNKNOWN_CATEGORY')).toBeNull()
  })

  it('returns null for empty string', () => {
    expect(getPrimaryLabel('')).toBeNull()
  })
})

describe('getDetailedLabel', () => {
  it('returns the curated label for a known detailed value', () => {
    expect(getDetailedLabel('FOOD_AND_DRINK_RESTAURANT')).toBe('Restaurant')
  })

  it('returns the curated label for a detailed value with ampersand', () => {
    expect(getDetailedLabel('FOOD_AND_DRINK_BEER_WINE_AND_LIQUOR')).toBe('Beer, Wine & Liquor')
  })

  it('returns null for an unknown detailed value', () => {
    expect(getDetailedLabel('UNKNOWN_DETAILED')).toBeNull()
  })
})

describe('useCuratedPfcDetailed', () => {
  it('returns a non-empty data array', () => {
    const { data } = useCuratedPfcDetailed()
    expect(data.length).toBeGreaterThan(0)
  })

  it('every primary has a non-empty primaryLabel', () => {
    const { data } = useCuratedPfcDetailed()
    for (const primary of data) {
      expect(primary.primaryLabel.length).toBeGreaterThan(0)
    }
  })

  it('every primary has at least one detailed option', () => {
    const { data } = useCuratedPfcDetailed()
    for (const primary of data) {
      expect(primary.detailed.length).toBeGreaterThan(0)
    }
  })
})
