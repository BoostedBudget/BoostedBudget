/**
 * Chart Layout Configuration
 * Issue #445: Recharts to Plotly Migration
 *
 * Provides theme-aware Plotly layout defaults and configuration.
 */

import type { Layout, Config } from 'plotly.js';
import { THEME_COLORS } from './colors';

/**
 * Default Plotly configuration
 * - displayModeBar: 'hover' - Show toolbar on hover
 * - responsive: true - Resize with container
 * - scrollZoom: false - Disable scroll zoom (prevents accidental zoom)
 */
export const DEFAULT_CONFIG: Partial<Config> = {
  responsive: true,
  displayModeBar: 'hover',
  scrollZoom: false,
  modeBarButtonsToRemove: ['lasso2d', 'select2d'],
  displaylogo: false,
};

/**
 * Get theme-aware base layout for charts
 * @param isDark - Whether dark mode is enabled
 * @returns Partial Layout object with theme-appropriate colors
 */
export function getBaseLayout(isDark: boolean): Partial<Layout> {
  const theme = isDark
    ? {
        background: THEME_COLORS.background.dark,
        text: THEME_COLORS.text.dark,
        grid: THEME_COLORS.grid.dark,
        axis: THEME_COLORS.axis.dark,
        tooltipBg: THEME_COLORS.tooltip.background.dark,
        tooltipBorder: THEME_COLORS.tooltip.border.dark,
        tooltipText: THEME_COLORS.tooltip.text.dark,
      }
    : {
        background: THEME_COLORS.background.light,
        text: THEME_COLORS.text.light,
        grid: THEME_COLORS.grid.light,
        axis: THEME_COLORS.axis.light,
        tooltipBg: THEME_COLORS.tooltip.background.light,
        tooltipBorder: THEME_COLORS.tooltip.border.light,
        tooltipText: THEME_COLORS.tooltip.text.light,
      };

  return {
    autosize: true,
    margin: { t: 10, r: 10, b: 40, l: 60 },
    paper_bgcolor: theme.background,
    plot_bgcolor: theme.background,
    font: {
      color: theme.text,
      family: 'Inter, system-ui, -apple-system, sans-serif',
      size: 12,
    },
    xaxis: {
      gridcolor: theme.grid,
      linecolor: theme.grid,
      tickfont: { size: 11, color: theme.axis },
      zeroline: false,
      showgrid: true,
      gridwidth: 1,
    },
    yaxis: {
      gridcolor: theme.grid,
      linecolor: theme.grid,
      tickfont: { size: 11, color: theme.axis },
      zeroline: false,
      showgrid: true,
      gridwidth: 1,
    },
    legend: {
      font: { size: 12, color: theme.text },
      bgcolor: 'transparent',
      orientation: 'h',
      yanchor: 'bottom',
      y: -0.25,
      xanchor: 'center',
      x: 0.5,
    },
    hoverlabel: {
      bgcolor: theme.tooltipBg,
      bordercolor: theme.tooltipBorder,
      font: {
        color: theme.tooltipText,
        family: 'Inter, system-ui, -apple-system, sans-serif',
        size: 12,
      },
    },
  };
}

/**
 * Get layout with currency Y-axis formatting
 * @param isDark - Whether dark mode is enabled
 * @param tickFormat - Optional custom tick format (default: '$,.0f')
 */
export function getCurrencyLayout(
  isDark: boolean,
  tickFormat: string = '$,.0f'
): Partial<Layout> {
  const base = getBaseLayout(isDark);
  return {
    ...base,
    yaxis: {
      ...base.yaxis,
      tickformat: tickFormat,
    },
  };
}

/**
 * Get layout for bar charts with grouping
 * @param isDark - Whether dark mode is enabled
 */
export function getBarChartLayout(isDark: boolean): Partial<Layout> {
  const base = getCurrencyLayout(isDark);
  return {
    ...base,
    barmode: 'group',
    bargap: 0.2,
    bargroupgap: 0.1,
  };
}

/**
 * Create a horizontal reference line shape
 * @param yValue - Y value for the reference line
 * @param isDark - Whether dark mode is enabled
 */
export function createHorizontalReferenceLine(
  yValue: number,
  isDark: boolean
): Layout['shapes'] {
  return [
    {
      type: 'line',
      x0: 0,
      x1: 1,
      xref: 'paper',
      y0: yValue,
      y1: yValue,
      line: {
        color: isDark ? THEME_COLORS.grid.dark : THEME_COLORS.grid.light,
        width: 1,
        dash: 'dot',
      },
    },
  ];
}
