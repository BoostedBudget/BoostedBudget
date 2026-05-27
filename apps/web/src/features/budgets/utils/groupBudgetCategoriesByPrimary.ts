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
 * Groups budget categories into primary→children hierarchy.
 *
 * Rules:
 * - PFC category with pfc_primary + NO pfc_detailed → parent
 * - PFC category with pfc_primary + pfc_detailed → child of matching parent
 * - Custom categories → always standalone ({ group: customCat, children: [] })
 * - Orphan detailed (no parent exists) → standalone ({ group: orphan, children: [] })
 *
 * Sort order:
 * - Within each group: parent first (if exists), then children sorted by created_at ASC
 * - Groups sorted by their first item's created_at ASC
 * - Standalone custom categories appended after PFC groups
 */
export function groupBudgetCategoriesByPrimary(categories: BudgetCategory[]): BudgetCategoryGroup[] {
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
    if (!cat.pfc_detailed) continue // skip primaries, handled above

    if (cat.pfc_primary && primaryMap.has(cat.pfc_primary)) {
      // Has a parent primary budget
      const existing = childrenMap.get(cat.pfc_primary) ?? []
      existing.push(cat)
      childrenMap.set(cat.pfc_primary, existing)
    } else {
      // Orphan — no parent budget exists
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

  // Custom category standalone groups, sorted by created_at ASC
  const customGroups: BudgetCategoryGroup[] = customCategories
    .sort((a, b) => a.created_at.localeCompare(b.created_at))
    .map(cat => ({ group: cat, children: [] }))

  // Custom categories appended after PFC groups
  return [...pfcGroups, ...customGroups]
}
