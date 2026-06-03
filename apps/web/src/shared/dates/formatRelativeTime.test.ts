import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { formatRelativeTime } from './formatRelativeTime'

describe('formatRelativeTime', () => {
  const FIXED_NOW = new Date('2024-01-15T12:00:00.000Z')
  // Capture before any patching so afterEach can restore the real constructor.
  const OriginalDate = Date

  beforeEach(() => {
    mock.module('bun:test', () => ({}))
    globalThis.Date = class extends OriginalDate {
      constructor(...args: ConstructorParameters<typeof OriginalDate>) {
        if (args.length === 0) {
          super(FIXED_NOW.getTime())
        } else {
          super(...args)
        }
      }
      static now() {
        return FIXED_NOW.getTime()
      }
    } as typeof Date
  })

  afterEach(() => {
    globalThis.Date = OriginalDate
  })

  it('returns "just now" for a date less than 1 minute ago', () => {
    const thirtySecondsAgo = new Date(FIXED_NOW.getTime() - 30 * 1000).toISOString()
    expect(formatRelativeTime(thirtySecondsAgo)).toBe('just now')
  })

  it('returns "Xm ago" for a date that is several minutes ago', () => {
    const fiveMinutesAgo = new Date(FIXED_NOW.getTime() - 5 * 60 * 1000).toISOString()
    expect(formatRelativeTime(fiveMinutesAgo)).toBe('5m ago')
  })

  it('returns "1m ago" for exactly 1 minute ago', () => {
    const oneMinuteAgo = new Date(FIXED_NOW.getTime() - 60 * 1000).toISOString()
    expect(formatRelativeTime(oneMinuteAgo)).toBe('1m ago')
  })

  it('returns "Xh ago" for a date that is several hours ago', () => {
    const threeHoursAgo = new Date(FIXED_NOW.getTime() - 3 * 3600 * 1000).toISOString()
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago')
  })

  it('returns "1h ago" for exactly 1 hour ago', () => {
    const oneHourAgo = new Date(FIXED_NOW.getTime() - 3600 * 1000).toISOString()
    expect(formatRelativeTime(oneHourAgo)).toBe('1h ago')
  })

  it('returns "Xd ago" for a date within the past week', () => {
    const threeDaysAgo = new Date(FIXED_NOW.getTime() - 3 * 86400 * 1000).toISOString()
    expect(formatRelativeTime(threeDaysAgo)).toBe('3d ago')
  })

  it('returns "1d ago" for exactly 1 day ago', () => {
    const oneDayAgo = new Date(FIXED_NOW.getTime() - 86400 * 1000).toISOString()
    expect(formatRelativeTime(oneDayAgo)).toBe('1d ago')
  })

  it('returns "6d ago" for 6 days ago', () => {
    const sixDaysAgo = new Date(FIXED_NOW.getTime() - 6 * 86400 * 1000).toISOString()
    expect(formatRelativeTime(sixDaysAgo)).toBe('6d ago')
  })

  it('returns toLocaleDateString for a date 7 or more days ago', () => {
    const sevenDaysAgo = new Date(FIXED_NOW.getTime() - 7 * 86400 * 1000)
    const result = formatRelativeTime(sevenDaysAgo.toISOString())
    expect(result).toBe(sevenDaysAgo.toLocaleDateString())
  })

  it('returns toLocaleDateString for a date 30 days ago', () => {
    const thirtyDaysAgo = new Date(FIXED_NOW.getTime() - 30 * 86400 * 1000)
    const result = formatRelativeTime(thirtyDaysAgo.toISOString())
    expect(result).toBe(thirtyDaysAgo.toLocaleDateString())
  })
})
