import { describe, it, expect } from 'bun:test'
import { THEME_COLORS } from '../colors'
import { createMarkLineData } from './referenceLine'

describe('createMarkLineData', () => {
  it('returns a markLine configuration object', () => {
    const result = createMarkLineData(0, false)
    expect(result).toBeDefined()
    expect(result.silent).toBe(true)
    expect(result.symbol).toEqual(['none', 'none'])
    expect(result.animation).toBe(false)
  })

  it('data array contains one entry with the specified yAxis value', () => {
    const result = createMarkLineData(100, false)
    expect(Array.isArray(result.data)).toBe(true)
    const data = result.data as Array<Record<string, unknown>>
    expect(data[0].yAxis).toBe(100)
  })

  it('light mode uses light grid color', () => {
    const result = createMarkLineData(0, false)
    const data = result.data as Array<Record<string, unknown>>
    const lineStyle = data[0].lineStyle as Record<string, unknown>
    expect(lineStyle.color).toBe(THEME_COLORS.grid.light)
  })

  it('dark mode uses dark grid color', () => {
    const result = createMarkLineData(0, true)
    const data = result.data as Array<Record<string, unknown>>
    const lineStyle = data[0].lineStyle as Record<string, unknown>
    expect(lineStyle.color).toBe(THEME_COLORS.grid.dark)
  })

  it('uses dotted line style', () => {
    const result = createMarkLineData(0, false)
    const data = result.data as Array<Record<string, unknown>>
    const lineStyle = data[0].lineStyle as Record<string, unknown>
    expect(lineStyle.type).toBe('dotted')
  })
})
