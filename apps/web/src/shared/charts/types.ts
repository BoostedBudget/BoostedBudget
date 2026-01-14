/**
 * Chart Type Definitions
 * Issue #445: Recharts to Plotly Migration
 *
 * TypeScript types for Plotly chart components and configurations.
 */

import type { Data, Layout, Config } from 'plotly.js';

/**
 * Base props for all chart components
 */
export interface BaseChartProps {
  /** Additional CSS class names */
  className?: string;
  /** Test ID for testing */
  'data-testid'?: string;
}

/**
 * Common data point structure for time-series charts
 */
export interface TimeSeriesDataPoint {
  date: string;
  value: number;
  label?: string;
}

/**
 * Balance chart data point
 */
export interface BalanceDataPoint {
  date: string;
  fullDate: string;
  balance: number;
  depository?: number;
  credit?: number;
  investment?: number;
  loan?: number;
}

/**
 * Cash flow chart data point
 */
export interface CashFlowDataPoint {
  label: string;
  income: number;
  expenses: number;
  net: number;
}

/**
 * Chart theme configuration
 */
export interface ChartTheme {
  isDark: boolean;
  colors: {
    background: string;
    text: string;
    grid: string;
    axis: string;
  };
}

/**
 * Plotly trace type (subset we use)
 */
export type PlotlyTraceType = 'scatter' | 'bar';

/**
 * Plotly scatter mode
 */
export type PlotlyScatterMode = 'lines' | 'markers' | 'lines+markers';

/**
 * Plot component props (from react-plotly.js)
 */
export interface PlotProps {
  data: Data[];
  layout?: Partial<Layout>;
  config?: Partial<Config>;
  style?: React.CSSProperties;
  className?: string;
  useResizeHandler?: boolean;
  onInitialized?: (figure: { data: Data[]; layout: Partial<Layout> }) => void;
  onUpdate?: (figure: { data: Data[]; layout: Partial<Layout> }) => void;
}

/**
 * Chart configuration options
 */
export interface ChartConfigOptions {
  /** Show modebar on hover, always, or never */
  displayModeBar?: boolean | 'hover';
  /** Enable responsive sizing */
  responsive?: boolean;
  /** Enable scroll zoom */
  scrollZoom?: boolean;
  /** Buttons to remove from modebar */
  modeBarButtonsToRemove?: string[];
}
