// Builds the display hierarchy for custom categories (Issue #1203).
//
// 2-level model: a custom category is exactly one of
//   - top-level (no parent_pfc_primary, no parent_id) — may have custom children
//   - nested under a PFC primary (parent_pfc_primary) — a leaf
//   - nested under a custom primary (parent_id) — a leaf
import type { CustomCategory } from '../types'

export interface CategoryGroup {
  parent: CustomCategory
  children: CustomCategory[] // custom categories whose parent_id === parent.id (alphabetical)
}

export interface PfcGroup {
  pfcPrimary: string
  categories: CustomCategory[] // custom categories nested under this PFC primary (alphabetical)
}

export interface CategoryHierarchy {
  /** Top-level custom categories with their custom children, alphabetical by name. */
  topLevel: CategoryGroup[]
  /** Custom categories nested under a PFC primary, grouped by primary (alphabetical). */
  pfcGroups: PfcGroup[]
}

function byName(a: CustomCategory, b: CustomCategory): number {
  return a.name.localeCompare(b.name)
}

export function buildCategoryHierarchy(categories: CustomCategory[]): CategoryHierarchy {
  const childrenByParent = new Map<string, CustomCategory[]>()
  const pfcMap = new Map<string, CustomCategory[]>()
  const topLevelParents: CustomCategory[] = []

  for (const cat of categories) {
    if (cat.parent_id) {
      const arr = childrenByParent.get(cat.parent_id) ?? []
      arr.push(cat)
      childrenByParent.set(cat.parent_id, arr)
    } else if (cat.parent_pfc_primary) {
      const arr = pfcMap.get(cat.parent_pfc_primary) ?? []
      arr.push(cat)
      pfcMap.set(cat.parent_pfc_primary, arr)
    } else {
      topLevelParents.push(cat)
    }
  }

  const topLevel: CategoryGroup[] = topLevelParents
    .slice()
    .sort(byName)
    .map((parent) => ({
      parent,
      children: (childrenByParent.get(parent.id) ?? []).slice().sort(byName),
    }))

  const pfcGroups: PfcGroup[] = Array.from(pfcMap.entries())
    .map(([pfcPrimary, cats]) => ({ pfcPrimary, categories: cats.slice().sort(byName) }))
    .sort((a, b) => a.pfcPrimary.localeCompare(b.pfcPrimary))

  return { topLevel, pfcGroups }
}

/**
 * Returns the custom categories eligible to be a parent for the category being edited.
 * Only top-level customs (no parent of their own) can be parents, and a category cannot
 * be its own parent (2-level rule).
 */
export function eligibleCustomParents(
  categories: CustomCategory[],
  selfId?: string
): CustomCategory[] {
  return categories
    .filter((c) => !c.parent_id && !c.parent_pfc_primary && c.id !== selfId)
    .slice()
    .sort(byName)
}
