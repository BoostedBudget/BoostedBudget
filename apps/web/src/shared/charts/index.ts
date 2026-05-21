/**
 * Shared Charts Module
 * Issue #1169: Plotly to ECharts Migration
 *
 * Centralized exports for chart components, utilities, and types.
 * All chart-related functionality should be imported from this module.
 */

// ECharts component (NEW)
export { EChartsChart } from './EChartsChart';
export type { EChartsChartProps } from './EChartsChart';

// ECharts option builders (NEW)
export { getBaseOption } from './options/base';
export { getCurrencyOption } from './options/currency';
export { getBarOption } from './options/bar';
export { createMarkLineData } from './options/referenceLine';

// Plot component (DEPRECATED - will be removed in Phase 7)
export { Plot, Plotly } from './PlotlyChart';

// Hooks
export { useDarkMode } from './useDarkMode';

// Layout utilities
export {
  DEFAULT_CONFIG,
  getBaseLayout,
  getCurrencyLayout,
  getBarChartLayout,
  createHorizontalReferenceLine,
} from './layout';

// Colors
export {
  CHART_COLORS,
  ACCOUNT_TYPE_COLORS,
  CASH_FLOW_COLORS,
  THEME_COLORS,
  CATEGORY_COLORS,
} from './colors';

// Formatters
export {
  formatCurrency,
  formatCurrencyCompact,
  formatCurrencyFull,
  getCurrencyTickFormat,
  getCurrencyCompactTickFormat,
  createCurrencyHoverTemplate,
  createTimeSeriesHoverTemplate,
} from './formatters';

// Types
export type {
  BaseChartProps,
  TimeSeriesDataPoint,
  BalanceDataPoint,
  CashFlowDataPoint,
  ChartTheme,
  PlotlyTraceType,
  PlotlyScatterMode,
  PlotProps,
  ChartConfigOptions,
} from './types';
