import { describe, it, expect } from 'bun:test'
import { getCurrencyOption } from './currency'

describe('getCurrencyOption', () => {
  it('includes a yAxis formatter function', () => {
    const opt = getCurrencyOption(false)
    const yAxis = opt.yAxis as Record<string, unknown>
    const axisLabel = yAxis.axisLabel as Record<string, unknown>
    expect(typeof axisLabel.formatter).toBe('function')
  })

  it('yAxis formatter formats numeric values as USD', () => {
    const opt = getCurrencyOption(false)
    const yAxis = opt.yAxis as Record<string, unknown>
    const formatter = (yAxis.axisLabel as Record<string, unknown>).formatter as (v: unknown) => string
    expect(formatter(1234)).toBe('$1,234')
    expect(formatter(0)).toBe('$0')
    expect(formatter(-500)).toBe('-$500')
  })

  it('yAxis formatter returns string for non-numeric values (prevents $NaN)', () => {
    const opt = getCurrencyOption(false)
    const yAxis = opt.yAxis as Record<string, unknown>
    const formatter = (yAxis.axisLabel as Record<string, unknown>).formatter as (v: unknown) => string
    expect(formatter('Jan')).toBe('Jan')
    expect(formatter(undefined)).toBe('undefined')
  })

  it('compact mode uses compact formatter', () => {
    const opt = getCurrencyOption(false, true)
    const yAxis = opt.yAxis as Record<string, unknown>
    const formatter = (yAxis.axisLabel as Record<string, unknown>).formatter as (v: unknown) => string
    expect(formatter(1500)).toBe('$1.5k')
    expect(formatter(2_000_000)).toBe('$2.0M')
  })

  it('extends getBaseOption (has grid, animation)', () => {
    const opt = getCurrencyOption(false)
    expect(opt.grid).toBeDefined()
    expect(opt.animation).toBe(true)
  })
})
