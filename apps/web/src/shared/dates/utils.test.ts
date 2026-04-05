import { describe, test, expect } from 'bun:test'
import { inferPreset, formatDateForAPI } from './utils'

describe('formatDateForAPI', () => {
  test('formats Date to YYYY-MM-DD', () => {
    const d = new Date(2024, 2, 5) // March 5, 2024 local
    const result = formatDateForAPI(d)
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(result).toBe('2024-03-05')
  })

  test('pads single-digit month and day', () => {
    const d = new Date(2024, 0, 9) // Jan 9
    expect(formatDateForAPI(d)).toBe('2024-01-09')
  })
})

describe('inferPreset', () => {
  const today = formatDateForAPI(new Date())

  test('no dates returns all', () => {
    expect(inferPreset(undefined, undefined)).toBe('all')
    expect(inferPreset('', '')).toBe('all')
  })

  test('this_month: start = first of current month, end = last day of current month', () => {
    // Get this month boundaries
    const now = new Date()
    const firstOfMonth = new Date(now.getFullYear(), now.getMonth(), 1)
    const lastOfMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0)
    const start = formatDateForAPI(firstOfMonth)
    const end = formatDateForAPI(lastOfMonth)
    expect(inferPreset(start, end)).toBe('this_month')
  })

  test('last_month: start = first of prev month, end = last of prev month', () => {
    const now = new Date()
    const firstOfPrev = new Date(now.getFullYear(), now.getMonth() - 1, 1)
    const lastOfPrev = new Date(now.getFullYear(), now.getMonth(), 0)
    const start = formatDateForAPI(firstOfPrev)
    const end = formatDateForAPI(lastOfPrev)
    expect(inferPreset(start, end)).toBe('last_month')
  })

  test('ytd: Jan 1 of current year to today', () => {
    const start = `${new Date().getFullYear()}-01-01`
    expect(inferPreset(start, today)).toBe('ytd')
  })

  test('unrecognized range returns custom', () => {
    expect(inferPreset('2020-01-01', '2020-06-15')).toBe('custom')
  })

  test('end in the future returns custom', () => {
    const future = `${new Date().getFullYear() + 1}-01-01`
    expect(inferPreset('2024-01-01', future)).toBe('custom')
  })
})
