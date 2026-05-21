import { THEME_COLORS } from '../colors'

interface MarkLineConfig {
  silent: boolean
  symbol: string[]
  animation: boolean
  data: Array<{ yAxis: number; lineStyle: { type: string; color: string } }>
}

export function createMarkLineData(yValue: number, isDark: boolean): MarkLineConfig {
  return {
    silent: true,
    symbol: ['none', 'none'],
    animation: false,
    data: [{
      yAxis: yValue,
      lineStyle: {
        type: 'dotted',
        color: isDark ? THEME_COLORS.grid.dark : THEME_COLORS.grid.light,
      },
    }],
  }
}
