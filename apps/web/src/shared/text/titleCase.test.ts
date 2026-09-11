import { describe, expect, it } from 'bun:test'
import { titleCaseWords } from './titleCase'

describe('titleCaseWords', () => {
  it('humanises space- and underscore-separated tokens', () => {
    expect(titleCaseWords('long-term capital gain')).toBe('Long-term Capital Gain')
    expect(titleCaseWords('non_resident_tax')).toBe('Non Resident Tax')
    expect(titleCaseWords('TAX WITHHELD')).toBe('Tax Withheld')
  })

  it('collapses repeated separators rather than emitting empty words', () => {
    expect(titleCaseWords('  fund   fee  ')).toBe('Fund Fee')
  })
})
