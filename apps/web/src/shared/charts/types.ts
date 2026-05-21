/**
 * Chart Type Definitions
 * Issue #1169: Plotly to ECharts Migration
 */

export interface BaseChartProps {
  className?: string;
  'data-testid'?: string;
}

export interface TimeSeriesDataPoint {
  date: string;
  value: number;
  label?: string;
}

export interface BalanceDataPoint {
  date: string;
  fullDate: string;
  balance: number;
  depository?: number;
  credit?: number;
  investment?: number;
  loan?: number;
}

export interface CashFlowDataPoint {
  label: string;
  income: number;
  expenses: number;
  net: number;
}

export interface ChartTheme {
  isDark: boolean;
  colors: {
    background: string;
    text: string;
    grid: string;
    axis: string;
  };
}
