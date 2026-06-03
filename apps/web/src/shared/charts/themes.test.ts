import { describe, it, expect, beforeAll } from 'bun:test'
import { CATEGORY_COLORS, THEME_COLORS } from './colors'

// Import themes for its side effect (registerTheme calls)
// and then verify via echarts API
beforeAll(async () => {
  await import('./themes')
})

describe('ECharts theme registration', () => {
  it('bb-light theme is registered', async () => {
    const { echarts } = await import('./echarts-setup')
    const container = document.createElement('div')
    container.style.width = '100px'
    container.style.height = '100px'
    document.body.appendChild(container)
    expect(() => {
      const chart = echarts.init(container, 'bb-light', { renderer: 'svg', width: 100, height: 100 })
      chart.dispose()
    }).not.toThrow()
    container.remove()
  })

  it('bb-dark theme is registered', async () => {
    const { echarts } = await import('./echarts-setup')
    const container = document.createElement('div')
    container.style.width = '100px'
    container.style.height = '100px'
    document.body.appendChild(container)
    expect(() => {
      const chart = echarts.init(container, 'bb-dark', { renderer: 'svg', width: 100, height: 100 })
      chart.dispose()
    }).not.toThrow()
    container.remove()
  })

  it('exported BB_LIGHT_THEME has correct backgroundColor from THEME_COLORS', async () => {
    const { BB_LIGHT_THEME } = await import('./themes')
    expect(BB_LIGHT_THEME.backgroundColor).toBe(THEME_COLORS.background.light)
  })

  it('exported BB_DARK_THEME has correct backgroundColor from THEME_COLORS', async () => {
    const { BB_DARK_THEME } = await import('./themes')
    expect(BB_DARK_THEME.backgroundColor).toBe(THEME_COLORS.background.dark)
  })

  it('bb-light color series matches CATEGORY_COLORS', async () => {
    const { BB_LIGHT_THEME } = await import('./themes')
    expect(BB_LIGHT_THEME.color).toEqual([...CATEGORY_COLORS])
  })

  it('bb-dark color series matches CATEGORY_COLORS', async () => {
    const { BB_DARK_THEME } = await import('./themes')
    expect(BB_DARK_THEME.color).toEqual([...CATEGORY_COLORS])
  })

  it('bb-light textStyle uses correct text color', async () => {
    const { BB_LIGHT_THEME } = await import('./themes')
    expect(BB_LIGHT_THEME.textStyle?.color).toBe(THEME_COLORS.text.light)
  })

  it('bb-dark textStyle uses correct text color', async () => {
    const { BB_DARK_THEME } = await import('./themes')
    expect(BB_DARK_THEME.textStyle?.color).toBe(THEME_COLORS.text.dark)
  })
})
