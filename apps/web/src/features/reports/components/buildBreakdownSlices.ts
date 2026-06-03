/**
 * Resolves pie-slice name + color for the spending breakdown chart. Custom categories
 * (`custom:<uuid>`) use the user's chosen custom-category color so they are visually
 * distinct from PFC categories; everything else uses the rotating chart palette (#1203).
 */
import { CATEGORY_COLORS } from '@/shared/charts'

export interface BreakdownSliceInput {
  category: string
  pfc_primary_display?: string | null
  total: number
}

export interface BreakdownSlice {
  name: string
  value: number
  itemStyle: { color: string }
}

export function buildBreakdownSlices(
  categories: BreakdownSliceInput[],
  customById: Map<string, { name?: string | null; color?: string | null }>
): BreakdownSlice[] {
  return categories.map((c, i) => {
    const fallback = CATEGORY_COLORS[i % CATEGORY_COLORS.length]
    if (c.category.startsWith('custom:')) {
      const meta = customById.get(c.category.slice('custom:'.length))
      return {
        name: meta?.name || c.pfc_primary_display || c.category,
        value: c.total,
        itemStyle: { color: meta?.color || fallback },
      }
    }
    return {
      name: c.pfc_primary_display ?? c.category,
      value: c.total,
      itemStyle: { color: fallback },
    }
  })
}
