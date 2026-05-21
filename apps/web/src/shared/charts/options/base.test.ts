import { describe, it, expect } from 'bun:test'
import { THEME_COLORS } from '../colors'
import { getBaseOption } from './base'

describe('getBaseOption', () => {
  it('returns an ECharts option object', () => {
    const opt = getBaseOption(false)
    expect(opt).toBeDefined()
    expect(typeof opt).toBe('object')
  })

  it('includes animation settings', () => {
    const opt = getBaseOption(false)
    expect(opt.animation).toBe(true)
    expect(opt.animationDuration).toBe(400)
    expect(opt.animationEasing).toBe('cubicInOut')
  })

  it('includes grid margins mapping Plotly margin convention', () => {
    const opt = getBaseOption(false)
    expect(opt.grid).toBeDefined()
    expect(opt.grid).toMatchObject({ top: 10, right: 10, bottom: 40, left: 60 })
  })

  it('light mode xAxis uses correct grid color', () => {
    const opt = getBaseOption(false)
    expect(opt.xAxis).toBeDefined()
    const xAxis = opt.xAxis as Record<string, unknown>
    expect((xAxis.splitLine as Record<string, unknown>)?.lineStyle).toMatchObject({
      color: THEME_COLORS.grid.light,
    })
  })

  it('dark mode xAxis uses dark grid color', () => {
    const opt = getBaseOption(true)
    const xAxis = opt.xAxis as Record<string, unknown>
    expect((xAxis.splitLine as Record<string, unknown>)?.lineStyle).toMatchObject({
      color: THEME_COLORS.grid.dark,
    })
  })

  it('includes legend positioned at bottom center (h orientation)', () => {
    const opt = getBaseOption(false)
    expect(opt.legend).toMatchObject({
      orient: 'horizontal',
      bottom: 0,
      left: 'center',
    })
  })

  it('includes tooltip with trigger axis', () => {
    const opt = getBaseOption(false)
    expect(opt.tooltip).toBeDefined()
  })
})
