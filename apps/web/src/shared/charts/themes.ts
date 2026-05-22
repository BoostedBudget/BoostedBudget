import type { EChartsOption } from 'echarts'
import { echarts } from './echarts-setup'
import { CATEGORY_COLORS, THEME_COLORS } from './colors'

function buildTheme(isDark: boolean): EChartsOption {
  const bg = isDark ? THEME_COLORS.background.dark : THEME_COLORS.background.light;
  const text = isDark ? THEME_COLORS.text.dark : THEME_COLORS.text.light;
  const grid = isDark ? THEME_COLORS.grid.dark : THEME_COLORS.grid.light;
  const axis = isDark ? THEME_COLORS.axis.dark : THEME_COLORS.axis.light;
  const tooltipBg = isDark ? THEME_COLORS.tooltip.background.dark : THEME_COLORS.tooltip.background.light;
  const tooltipBorder = isDark ? THEME_COLORS.tooltip.border.dark : THEME_COLORS.tooltip.border.light;
  const tooltipText = isDark ? THEME_COLORS.tooltip.text.dark : THEME_COLORS.tooltip.text.light;

  const axisConfig = {
    axisLine: { lineStyle: { color: axis } },
    axisTick: { lineStyle: { color: axis } },
    axisLabel: { color: text },
    splitLine: { lineStyle: { color: grid } },
  };

  return {
    backgroundColor: bg,
    textStyle: { color: text, fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif' },
    color: [...CATEGORY_COLORS],
    axisPointer: { lineStyle: { color: grid } },
    tooltip: { backgroundColor: tooltipBg, borderColor: tooltipBorder, textStyle: { color: tooltipText } },
    categoryAxis: axisConfig,
    valueAxis: axisConfig,
  };
}

export const BB_LIGHT_THEME = buildTheme(false);
export const BB_DARK_THEME = buildTheme(true);

echarts.registerTheme('bb-light', BB_LIGHT_THEME);
echarts.registerTheme('bb-dark', BB_DARK_THEME);
