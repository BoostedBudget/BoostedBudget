import { echarts } from './echarts-setup'
import { CATEGORY_COLORS, THEME_COLORS } from './colors'

// ECharts theme object type (subset of what registerTheme accepts)
interface BBTheme {
  backgroundColor: string
  textStyle: { color: string; fontFamily: string }
  color: string[]
  axisPointer: { lineStyle: { color: string } }
  tooltip: { backgroundColor: string; borderColor: string; textStyle: { color: string } }
  categoryAxis: { axisLine: { lineStyle: { color: string } }; axisTick: { lineStyle: { color: string } }; axisLabel: { color: string }; splitLine: { lineStyle: { color: string } } }
  valueAxis: { axisLine: { lineStyle: { color: string } }; axisTick: { lineStyle: { color: string } }; axisLabel: { color: string }; splitLine: { lineStyle: { color: string } } }
}

function buildTheme(isDark: boolean): BBTheme {
  const bg = isDark ? THEME_COLORS.background.dark : THEME_COLORS.background.light
  const text = isDark ? THEME_COLORS.text.dark : THEME_COLORS.text.light
  const grid = isDark ? THEME_COLORS.grid.dark : THEME_COLORS.grid.light
  const axis = isDark ? THEME_COLORS.axis.dark : THEME_COLORS.axis.light
  const tooltipBg = isDark ? THEME_COLORS.tooltip.background.dark : THEME_COLORS.tooltip.background.light
  const tooltipBorder = isDark ? THEME_COLORS.tooltip.border.dark : THEME_COLORS.tooltip.border.light
  const tooltipText = isDark ? THEME_COLORS.tooltip.text.dark : THEME_COLORS.tooltip.text.light

  const axisConfig = {
    axisLine: { lineStyle: { color: axis } },
    axisTick: { lineStyle: { color: axis } },
    axisLabel: { color: text },
    splitLine: { lineStyle: { color: grid } },
  }

  return {
    backgroundColor: bg,
    textStyle: { color: text, fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif' },
    color: [...CATEGORY_COLORS],
    axisPointer: { lineStyle: { color: grid } },
    tooltip: { backgroundColor: tooltipBg, borderColor: tooltipBorder, textStyle: { color: tooltipText } },
    categoryAxis: axisConfig,
    valueAxis: axisConfig,
  }
}

export const BB_LIGHT_THEME = buildTheme(false)
export const BB_DARK_THEME = buildTheme(true)

// Register both themes once at module load. Side-effect intentional.
echarts.registerTheme('bb-light', BB_LIGHT_THEME)
echarts.registerTheme('bb-dark', BB_DARK_THEME)
