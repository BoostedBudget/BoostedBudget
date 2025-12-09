/**
 * Shared Charts Module
 * Issue #445: Recharts to Plotly Migration
 *
 * Centralized exports for chart components, utilities, and types.
 * All chart-related functionality should be imported from this module.
 *
 * Usage:
 * ```tsx
 * import {
 *   Plot,
 *   useDarkMode,
 *   getBaseLayout,
 *   CHART_COLORS,
 *   formatCurrency,
 * } from '@/shared/charts';
 * ```
 */

// Plot component
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
