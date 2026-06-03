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
const sortByCreatedAt = (a: BudgetCategory, b: BudgetCategory) =>
  a.created_at.localeCompare(b.created_at)

const getFirstItem = (group: BudgetCategoryGroup) =>
  group.group ?? group.children[0]

export function groupBudgetCategoriesByPrimary(
  categories: BudgetCategory[],
  customCategoryMap?: CustomCategoryMap
): BudgetCategoryGroup[] {
  if (categories.length === 0) return []

  const pfcCategories = categories.filter(c => c.category_kind === 'pfc')
  const customCategories = categories.filter(c => c.category_kind !== 'pfc')

  // Map pfc_primary → primary-only budget (parent)
  const primaryMap = new Map<string, BudgetCategory>()
  for (const cat of pfcCategories) {
    if (cat.pfc_primary && !cat.pfc_detailed) {
      primaryMap.set(cat.pfc_primary, cat)
    }
  }

  // Map pfc_primary → children, and collect orphans
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
  const pfcGroups: BudgetCategoryGroup[] = Array.from(primaryMap, ([primary, parent]) => ({
    group: parent,
    children: (childrenMap.get(primary) ?? []).sort(sortByCreatedAt),
  }))

  // Add orphan groups
  for (const orphan of orphans) {
    pfcGroups.push({ group: orphan, children: [] })
  }

  // Sort groups by first item's created_at
  pfcGroups.sort((a, b) => {
    const aFirst = getFirstItem(a)
    const bFirst = getFirstItem(b)
    return aFirst && bFirst ? sortByCreatedAt(aFirst, bFirst) : 0
  })

  // Nest custom categories under PFC parents when possible
  const standaloneCustom: BudgetCategory[] = []
  for (const cat of customCategories) {
    let nested = false
    if (customCategoryMap && cat.custom_category_id) {
      const meta = customCategoryMap.get(cat.custom_category_id)
      if (meta?.parent_pfc_primary) {
        const parentGroup = pfcGroups.find(g => g.group?.pfc_primary === meta.parent_pfc_primary)
        if (parentGroup) {
          parentGroup.children.push(cat)
          nested = true
        }
      }
    }
    if (!nested) {
      standaloneCustom.push(cat)
    }
  }

  // Re-sort children after adding custom categories
  for (const group of pfcGroups) {
    group.children.sort(sortByCreatedAt)
  }

  // Build custom groups, nesting custom sub-allocations under their budgeted custom
  // parent (custom_parent_id → a budgeted custom category's custom_category_id), #1203.
  const customById = new Map<string, BudgetCategory>()
  for (const cat of standaloneCustom) {
    if (cat.custom_category_id) customById.set(cat.custom_category_id, cat)
  }
  const customGroupMap = new Map<string, BudgetCategoryGroup>()
  const customChildren: BudgetCategory[] = []
  for (const cat of standaloneCustom) {
    if (cat.custom_parent_id && customById.has(cat.custom_parent_id)) {
      customChildren.push(cat)
    } else {
      customGroupMap.set(cat.custom_category_id ?? cat.id, { group: cat, children: [] })
    }
  }
  for (const child of customChildren) {
    const parentGroup = child.custom_parent_id ? customGroupMap.get(child.custom_parent_id) : undefined
    if (parentGroup) {
      parentGroup.children.push(child)
    } else {
      // Parent not budgeted as a top-level group (shouldn't happen) — show standalone.
      customGroupMap.set(child.custom_category_id ?? child.id, { group: child, children: [] })
    }
  }
  const customGroups = Array.from(customGroupMap.values())
    .sort((a, b) => sortByCreatedAt(a.group as BudgetCategory, b.group as BudgetCategory))
  for (const group of customGroups) {
    group.children.sort(sortByCreatedAt)
  }

  return [...pfcGroups, ...customGroups]
}
