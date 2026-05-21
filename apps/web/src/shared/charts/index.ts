/**
 * Shared Charts Module
 * Issue #1169: Plotly to ECharts Migration
 *
 * Centralized exports for chart components, utilities, and types.
 * All chart-related functionality should be imported from this module.
 */

// ECharts component
export { EChartsChart } from './EChartsChart';
export type { EChartsChartProps } from './EChartsChart';

// ECharts option builders
export { getBaseOption } from './options/base';
export { getCurrencyOption } from './options/currency';
export { getBarOption } from './options/bar';
export { createMarkLineData } from './options/referenceLine';

// Hooks
export { useDarkMode } from './useDarkMode';

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
} from './formatters';

// Types
export type {
  BaseChartProps,
  TimeSeriesDataPoint,
  BalanceDataPoint,
  CashFlowDataPoint,
  ChartTheme,
} from './types';
