/**
 * groupBudgetCategoriesByPrimary
 * Issue #1200: Multi-level category hierarchy — Task 7
 *
 * Pure function that groups budget categories into a primary→children hierarchy
 * for hierarchical display in the budget category manager.
 */
import type { BudgetCategory } from '../types'

export interface BudgetCategoryGroup {
  /** Primary-only PFC budget (parent) — null when detailed has no parent budget */
  group: BudgetCategory | null
  /** Detailed PFC budget children under this primary */
  children: BudgetCategory[]
}

/**
 * Optional map from custom_category_id (UUID) → { name, parent_pfc_primary }.
 * When provided, custom categories with a parent_pfc_primary matching an existing
 * PFC parent group are nested as children of that group instead of being standalone.
 */
export type CustomCategoryMap = Map<string, { name: string; parent_pfc_primary?: string | null }>

/**
 * Groups budget categories into primary→children hierarchy.
 *
 * Rules:
 * - PFC category with pfc_primary + NO pfc_detailed → parent
 * - PFC category with pfc_primary + pfc_detailed → child of matching parent
 * - Custom categories → standalone by default; nested under PFC parent group when
 *   customCategoryMap resolves a matching parent_pfc_primary
 * - Orphan detailed (no parent exists) → standalone ({ group: orphan, children: [] })
 *
 * Sort order:
 * - Within each group: parent first (if exists), then children sorted by created_at ASC
 * - Groups sorted by their first item's created_at ASC
 * - Standalone custom categories appended after PFC groups
 */
export function groupBudgetCategoriesByPrimary(
  categories: BudgetCategory[],
  customCategoryMap?: CustomCategoryMap
): BudgetCategoryGroup[] {
  if (categories.length === 0) return []

  // Separate PFC and custom categories
  const pfcCategories = categories.filter(c => c.category_kind === 'pfc')
  const customCategories = categories.filter(c => c.category_kind !== 'pfc')

  // Build map of pfc_primary → primary-only budget (the parent)
  const primaryMap = new Map<string, BudgetCategory>()
  for (const cat of pfcCategories) {
    if (cat.pfc_primary && !cat.pfc_detailed) {
      primaryMap.set(cat.pfc_primary, cat)
    }
  }

  // Build map of pfc_primary → children (detailed budgets)
  const childrenMap = new Map<string, BudgetCategory[]>()
  const orphans: BudgetCategory[] = []

  for (const cat of pfcCategories) {
    if (!cat.pfc_detailed) continue

    if (cat.pfc_primary && primaryMap.has(cat.pfc_primary)) {
      const children = childrenMap.get(cat.pfc_primary) ?? []
      children.push(cat)
      childrenMap.set(cat.pfc_primary, children)
    } else {
      orphans.push(cat)
    }
  }

  // Build PFC groups
  const pfcGroups: BudgetCategoryGroup[] = []

  // Groups for primaries (with or without children)
  for (const [primary, parent] of primaryMap) {
    const children = (childrenMap.get(primary) ?? []).sort(
      (a, b) => a.created_at.localeCompare(b.created_at)
    )
    pfcGroups.push({ group: parent, children })
  }

  // Orphan groups (detailed with no parent)
  for (const orphan of orphans) {
    pfcGroups.push({ group: orphan, children: [] })
  }

  // Sort PFC groups by their first item's created_at ASC
  pfcGroups.sort((a, b) => {
    const aFirst = a.group ?? a.children[0]
    const bFirst = b.group ?? b.children[0]
    if (!aFirst || !bFirst) return 0
    return aFirst.created_at.localeCompare(bFirst.created_at)
  })

  // When customCategoryMap is provided, nest custom categories under their PFC parent group.
  // Otherwise all custom categories are standalone.
  const standaloneCustomCategories: BudgetCategory[] = []

  for (const cat of customCategories) {
    let nested = false
    if (customCategoryMap && cat.custom_category_id) {
      const meta = customCategoryMap.get(cat.custom_category_id)
      if (meta?.parent_pfc_primary) {
        // Find the PFC group for this primary
        const parentGroup = pfcGroups.find(g => g.group?.pfc_primary === meta.parent_pfc_primary)
        if (parentGroup) {
          parentGroup.children.push(cat)
          nested = true
        }
      }
    }
    if (!nested) {
      standaloneCustomCategories.push(cat)
    }
  }

  // Sort children within each group by created_at ASC (re-sort after custom children may have been added)
  for (const group of pfcGroups) {
    group.children.sort((a, b) => a.created_at.localeCompare(b.created_at))
  }

  // Standalone custom category groups, sorted by created_at ASC
  const customGroups: BudgetCategoryGroup[] = standaloneCustomCategories
    .sort((a, b) => a.created_at.localeCompare(b.created_at))
    .map(cat => ({ group: cat, children: [] }))

  // Custom categories appended after PFC groups
  return [...pfcGroups, ...customGroups]
}
