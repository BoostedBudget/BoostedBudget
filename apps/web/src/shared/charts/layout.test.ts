/**
 * Chart Layout Tests
 * Issue #445: Recharts to Plotly Migration
 * Issue #525: Auto-scale chart axes on filter changes
 */

import { describe, it, expect } from 'bun:test'
import {
  DEFAULT_CONFIG,
  CHART_TRANSITION,
  getBaseLayout,
  getCurrencyLayout,
  getBarChartLayout,
  createHorizontalReferenceLine,
} from './layout'
import { THEME_COLORS } from './colors'

describe('DEFAULT_CONFIG', () => {
  it('has responsive enabled', () => {
    expect(DEFAULT_CONFIG.responsive).toBe(true)
  })

  it('shows modebar on hover', () => {
    expect(DEFAULT_CONFIG.displayModeBar).toBe('hover')
  })

  it('has scroll zoom disabled', () => {
    expect(DEFAULT_CONFIG.scrollZoom).toBe(false)
  })

  it('removes selection tools from modebar', () => {
    expect(DEFAULT_CONFIG.modeBarButtonsToRemove).toContain('lasso2d')
    expect(DEFAULT_CONFIG.modeBarButtonsToRemove).toContain('select2d')
  })

  it('hides Plotly logo', () => {
    expect(DEFAULT_CONFIG.displaylogo).toBe(false)
  })
})

// Issue #525: CHART_TRANSITION tests
describe('CHART_TRANSITION', () => {
  it('has 400ms duration', () => {
    expect(CHART_TRANSITION.duration).toBe(400)
  })

  it('uses cubic-in-out easing', () => {
    expect(CHART_TRANSITION.easing).toBe('cubic-in-out')
  })

  it('has layout first ordering', () => {
    expect(CHART_TRANSITION.ordering).toBe('layout first')
  })
})

describe('getBaseLayout', () => {
  describe('light mode', () => {
    const layout = getBaseLayout(false)

    it('uses light background color', () => {
      expect(layout.paper_bgcolor).toBe(THEME_COLORS.background.light)
      expect(layout.plot_bgcolor).toBe(THEME_COLORS.background.light)
    })

    it('uses light text color', () => {
      expect(layout.font?.color).toBe(THEME_COLORS.text.light)
    })

    it('uses light grid color', () => {
      expect(layout.xaxis?.gridcolor).toBe(THEME_COLORS.grid.light)
      expect(layout.yaxis?.gridcolor).toBe(THEME_COLORS.grid.light)
    })

    it('uses light tooltip colors', () => {
      expect(layout.hoverlabel?.bgcolor).toBe(THEME_COLORS.tooltip.background.light)
      expect(layout.hoverlabel?.bordercolor).toBe(THEME_COLORS.tooltip.border.light)
    })
  })

  describe('dark mode', () => {
    const layout = getBaseLayout(true)

    it('uses dark background color', () => {
      expect(layout.paper_bgcolor).toBe(THEME_COLORS.background.dark)
      expect(layout.plot_bgcolor).toBe(THEME_COLORS.background.dark)
    })

    it('uses dark text color', () => {
      expect(layout.font?.color).toBe(THEME_COLORS.text.dark)
    })

    it('uses dark grid color', () => {
      expect(layout.xaxis?.gridcolor).toBe(THEME_COLORS.grid.dark)
      expect(layout.yaxis?.gridcolor).toBe(THEME_COLORS.grid.dark)
    })

    it('uses dark tooltip colors', () => {
      expect(layout.hoverlabel?.bgcolor).toBe(THEME_COLORS.tooltip.background.dark)
      expect(layout.hoverlabel?.bordercolor).toBe(THEME_COLORS.tooltip.border.dark)
    })
  })

  it('has autosize enabled', () => {
    expect(getBaseLayout(false).autosize).toBe(true)
  })

  it('has horizontal legend at bottom', () => {
    const layout = getBaseLayout(false)
    expect(layout.legend?.orientation).toBe('h')
    expect(layout.legend?.yanchor).toBe('bottom')
  })

  it('uses Inter font family', () => {
    const layout = getBaseLayout(false)
    expect(layout.font?.family).toContain('Inter')
  })

  // Issue #525: Transition and autorange tests
  it('includes transition configuration', () => {
    const layout = getBaseLayout(false)
    expect(layout.transition).toBeDefined()
    expect(layout.transition?.duration).toBe(400)
    expect(layout.transition?.easing).toBe('cubic-in-out')
  })

  it('has autorange enabled on x-axis', () => {
    const layout = getBaseLayout(false)
    expect(layout.xaxis?.autorange).toBe(true)
  })

  it('has autorange enabled on y-axis', () => {
    const layout = getBaseLayout(false)
    expect(layout.yaxis?.autorange).toBe(true)
  })
})

describe('getCurrencyLayout', () => {
  it('inherits base layout', () => {
    const layout = getCurrencyLayout(false)
    expect(layout.paper_bgcolor).toBe(THEME_COLORS.background.light)
  })

  it('sets default currency tick format', () => {
    const layout = getCurrencyLayout(false)
    expect(layout.yaxis?.tickformat).toBe('$,.0f')
  })

  it('allows custom tick format', () => {
    const layout = getCurrencyLayout(false, '$.2s')
    expect(layout.yaxis?.tickformat).toBe('$.2s')
  })

  it('respects dark mode', () => {
    const layout = getCurrencyLayout(true)
    expect(layout.paper_bgcolor).toBe(THEME_COLORS.background.dark)
  })
})

describe('getBarChartLayout', () => {
  it('inherits currency layout', () => {
    const layout = getBarChartLayout(false)
    expect(layout.yaxis?.tickformat).toBe('$,.0f')
  })

  it('sets barmode to group', () => {
    const layout = getBarChartLayout(false)
    expect(layout.barmode).toBe('group')
  })

  it('sets bar gap values', () => {
    const layout = getBarChartLayout(false)
    expect(layout.bargap).toBe(0.2)
    expect(layout.bargroupgap).toBe(0.1)
  })
})

describe('createHorizontalReferenceLine', () => {
  it('creates a line at specified y value', () => {
    const shapes = createHorizontalReferenceLine(0, false)
    expect(shapes).toHaveLength(1)
    expect(shapes![0].y0).toBe(0)
    expect(shapes![0].y1).toBe(0)
  })

  it('spans full width', () => {
    const shapes = createHorizontalReferenceLine(100, false)
    expect(shapes![0].x0).toBe(0)
    expect(shapes![0].x1).toBe(1)
    expect(shapes![0].xref).toBe('paper')
  })

  it('uses light theme colors in light mode', () => {
    const shapes = createHorizontalReferenceLine(0, false)
    expect(shapes![0].line?.color).toBe(THEME_COLORS.grid.light)
  })

  it('uses dark theme colors in dark mode', () => {
    const shapes = createHorizontalReferenceLine(0, true)
    expect(shapes![0].line?.color).toBe(THEME_COLORS.grid.dark)
  })

  it('has dotted line style', () => {
    const shapes = createHorizontalReferenceLine(0, false)
    expect(shapes![0].line?.dash).toBe('dot')
  })
})
