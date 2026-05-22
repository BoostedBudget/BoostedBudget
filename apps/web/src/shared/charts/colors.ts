/**
 * Chart Color Palette
 *
 * Colorblind-safe palette for all chart visualizations.
 * Colors are consistent with Tailwind's color system.
 */

/**
 * Account type colors for balance charts
 */
export const ACCOUNT_TYPE_COLORS = {
  balance: '#3b82f6', // blue-500 - Total balance
  depository: '#10b981', // emerald-500 - Checking/Savings
  credit: '#ef4444', // red-500 - Credit cards
  investment: '#8b5cf6', // purple-500 - Investment accounts
  loan: '#f59e0b', // amber-500 - Loans
} as const;

/**
 * Cash flow chart colors
 */
export const CASH_FLOW_COLORS = {
  income: '#3b82f6', // blue-500
  expenses: '#f97316', // orange-500
  netPositive: '#10b981', // emerald-500
  netNegative: '#ef4444', // red-500
} as const;

/**
 * Theme-aware colors for chart elements
 */
export const THEME_COLORS = {
  grid: {
    light: '#e5e7eb', // gray-200
    dark: '#374151', // gray-700
  },
  axis: {
    light: '#6b7280', // gray-500
    dark: '#9ca3af', // gray-400
  },
  background: {
    light: '#ffffff',
    dark: '#1f2937', // gray-800
  },
  text: {
    light: '#111827', // gray-900
    dark: '#f3f4f6', // gray-100
  },
  tooltip: {
    background: {
      light: '#ffffff',
      dark: '#374151', // gray-700
    },
    border: {
      light: '#e5e7eb', // gray-200
      dark: '#4b5563', // gray-600
    },
    text: {
      light: '#111827', // gray-900
      dark: '#f3f4f6', // gray-100
    },
  },
} as const;

/**
 * Category spending chart colors (colorblind-safe palette)
 * Used for spending donut/stacked bar charts in reports
 */
export const CATEGORY_COLORS = [
  '#3b82f6', // blue-500
  '#10b981', // emerald-500
  '#f59e0b', // amber-500
  '#8b5cf6', // violet-500
  '#f97316', // orange-500
  '#06b6d4', // cyan-500
  '#ec4899', // pink-500
  '#84cc16', // lime-500
  '#6366f1', // indigo-500
  '#14b8a6', // teal-500
] as const;

/**
 * Combined chart colors export for convenience
 */
export const CHART_COLORS = {
  ...ACCOUNT_TYPE_COLORS,
  ...CASH_FLOW_COLORS,
  theme: THEME_COLORS,
  categories: CATEGORY_COLORS,
} as const;
