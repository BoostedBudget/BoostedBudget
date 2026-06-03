import { describe, it, expect } from 'bun:test'
import { getBarOption } from './bar'

describe('getBarOption', () => {
  it('extends getCurrencyOption (has grid, animation, yAxis formatter)', () => {
    const opt = getBarOption(false)
    expect(opt.grid).toBeDefined()
    expect(opt.animation).toBe(true)
    const yAxis = opt.yAxis as Record<string, unknown>
    expect(typeof (yAxis.axisLabel as Record<string, unknown>).formatter).toBe('function')
  })

  it('sets tooltip trigger to axis for multi-series bar charts', () => {
    const opt = getBarOption(false)
    const tooltip = opt.tooltip as Record<string, unknown>
    expect(tooltip.trigger).toBe('axis')
  })
})
