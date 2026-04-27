/**
 * Category Color Mapping
 *
 * Issue #531: Transaction List Visual Enhancements
 *
 * Maps Plaid primary categories to consistent colors across the app.
 * Colors are based on Tailwind CSS palette for consistency with the design system.
 */

/**
 * Predefined colors for common Plaid primary categories.
 * Each color is chosen to be visually distinct and meaningful.
 */
export const CATEGORY_COLORS: Record<string, string> = {
  // Income & Money In
  'Income': '#10B981',              // Emerald-500 - Growth/positive
  'Transfer': '#06B6D4',            // Cyan-500 - Neutral movement
  'Payment': '#0EA5E9',             // Sky-500 - Transactions
  'Interest': '#84CC16',            // Lime-500 - Passive income
  'Dividend': '#22C55E',            // Green-500 - Investment returns

  // Living Expenses
  'Food and Drink': '#F97316',      // Orange-500 - Warm, food-related
  'Groceries': '#22C55E',           // Green-500 - Fresh produce
  'Shopping': '#A855F7',            // Purple-500 - Retail/consumer
  'Shops': '#8B5CF6',               // Violet-500 - Alternative shopping
  'Recreation': '#EC4899',          // Pink-500 - Fun/leisure
  'Entertainment': '#F43F5E',       // Rose-500 - Media/events

  // Home & Services
  'Service': '#6366F1',             // Indigo-500 - Professional
  'Utilities': '#EAB308',           // Yellow-500 - Essential services
  'Home': '#14B8A6',                // Teal-500 - Household
  'Rent': '#64748B',                // Slate-500 - Fixed expense

  // Transportation & Travel
  'Travel': '#3B82F6',              // Blue-500 - Adventure/journey
  'Transportation': '#0EA5E9',      // Sky-500 - Daily commute
  'Gas': '#F59E0B',                 // Amber-500 - Fuel

  // Health & Personal
  'Healthcare': '#EF4444',          // Red-500 - Medical (attention)
  'Personal Care': '#D946EF',       // Fuchsia-500 - Self-care

  // Finance & Government
  'Bank Fees': '#64748B',           // Slate-500 - Administrative
  'Tax': '#DC2626',                 // Red-600 - Important/attention
  'Government': '#4F46E5',          // Indigo-600 - Official
  'Insurance': '#7C3AED',           // Violet-600 - Protection

  // PFCv2 primary display names (ensure all 18 resolve to a non-default color)
  'Food And Drink': '#F97316',        // alias for existing 'Food and Drink' entry
  'General Merchandise': '#A855F7',   // Purple-500 - Retail
  'General Services': '#6366F1',      // Indigo-500 - Services
  'Government And Non Profit': '#4F46E5', // Indigo-600 - Official
  'Home Improvement': '#14B8A6',      // Teal-500 - Home
  'Loan Disbursements': '#DC2626',    // Red-600 - New debt
  'Loan Payments': '#EF4444',         // Red-500 - Debt repayment
  'Medical': '#EF4444',               // Red-500 - Health attention
  'Rent And Utilities': '#64748B',    // Slate-500 - Fixed monthly
  'Transfer In': '#06B6D4',           // Cyan-500 - Money in
  'Transfer Out': '#0EA5E9',          // Sky-500 - Money out
  'Other': '#9CA3AF',                 // Gray-400 - Catch-all (distinct from #6B7280 default)

  // Other
  'Subscriptions': '#8B5CF6',       // Violet-500 - Recurring
  'Community': '#F472B6',           // Pink-400 - Social
  'General': '#6B7280',             // Gray-500 - Catch-all
}

/**
 * Default color for unknown/uncategorized transactions.
 * Uses Tailwind gray-500 for a neutral appearance.
 */
export const DEFAULT_CATEGORY_COLOR = '#6B7280'

/**
 * Get the display color for a category.
 *
 * @param category - The category name (case-insensitive)
 * @returns Hex color string for the category
 *
 * @example
 * getCategoryColor('Food and Drink') // Returns '#F97316'
 * getCategoryColor('Unknown') // Returns '#6B7280' (default)
 * getCategoryColor(null) // Returns '#6B7280' (default)
 */
export function getCategoryColor(category: string | null | undefined): string {
  // Handle null, undefined, or empty/whitespace-only strings
  if (!category || !category.trim()) {
    return DEFAULT_CATEGORY_COLOR
  }

  const trimmed = category.trim()

  // Handle "Uncategorized" specially
  if (trimmed.toLowerCase() === 'uncategorized') {
    return DEFAULT_CATEGORY_COLOR
  }

  // Try exact match first (case-insensitive)
  for (const [key, color] of Object.entries(CATEGORY_COLORS)) {
    if (key.toLowerCase() === trimmed.toLowerCase()) {
      return color
    }
  }

  // No match found, return default
  return DEFAULT_CATEGORY_COLOR
}
