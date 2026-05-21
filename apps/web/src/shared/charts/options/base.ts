import type { EChartsOption } from 'echarts'
import { THEME_COLORS } from '../colors'

export function getBaseOption(isDark: boolean): EChartsOption {
  const grid = isDark ? THEME_COLORS.grid.dark : THEME_COLORS.grid.light
  const axis = isDark ? THEME_COLORS.axis.dark : THEME_COLORS.axis.light
  const text = isDark ? THEME_COLORS.text.dark : THEME_COLORS.text.light

  const axisStyle = {
    axisLine: { lineStyle: { color: axis } },
    axisTick: { lineStyle: { color: axis } },
    axisLabel: { color: text, fontSize: 11 },
    splitLine: { lineStyle: { color: grid } },
  }

  return {
    animation: true,
    animationDuration: 400,
    animationEasing: 'cubicInOut',
    grid: { top: 10, right: 10, bottom: 40, left: 60, containLabel: false },
    tooltip: { trigger: 'item' },
    legend: { orient: 'horizontal', bottom: 0, left: 'center', textStyle: { color: text } },
    xAxis: { type: 'category', ...axisStyle },
    yAxis: { type: 'value', ...axisStyle },
  }
}
