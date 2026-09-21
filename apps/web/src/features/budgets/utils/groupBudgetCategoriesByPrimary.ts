/**
 * groupBudgetCategoriesByPrimary
 * Issue #1200: Multi-level category hierarchy — Task 7
 *
 * Pure function that groups budget categories into a primary→children hierarchy
 * for hierarchical display in the budget category manager.
 */
import type { BudgetCategory } from '../types'
import { isContainingMatchLogic, isContainedMatchLogic } from '../types'

export interface BudgetCategoryGroup {
  /** Primary-only PFC budget (parent) — null when detailed has no parent budget */
  group: BudgetCategory | null
  /** Detailed PFC budget children under this primary */
  children: BudgetCategory[]
}

/**
 * Optional map from custom_category_id (UUID) → { name, parent_pfc_primary }.
 * Used to resolve a custom category's display name (the manager) and its legend label
 * (AllocationBreakdown). It is deliberately NOT consulted for nesting: the budget ROW's own
 * `custom_parent_pfc_primary` / `custom_parent_id` fields are the linkage (#1686), because a
 * map entry can disagree with the row and the row is what the remainder arithmetic reads.
 */
export type CustomCategoryMap = Map<string, { name: string; parent_pfc_primary?: string | null }>

/**
 * Groups budget categories into primary→children hierarchy.
 *
 * Rules:
 * - PFC category with pfc_primary + NO pfc_detailed → parent
 * - PFC category with pfc_primary + pfc_detailed → child of matching parent
 * - Custom categories → standalone by default; nested under a PRIMARY-ONLY PFC parent group
 *   when the row's `custom_parent_pfc_primary` names it (#1686), under a budgeted custom
 *   parent via `custom_parent_id` (#1203), or under a PFC root via
 *   `custom_grandparent_pfc_primary` (#1686, transitive)
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

/**
 * Finds the PRIMARY-ONLY PFC group a custom category may nest under, given the primary name the
 * row links to. Both match_logic gates mirror isSubAllocationOfParent (types.ts) so display
 * nesting stays a strict subset of the remainder arithmetic that computeParentAllocationRemainder
 * performs.
 */
function findPfcParentGroup(
  pfcGroups: BudgetCategoryGroup[],
  pfcPrimary: string,
  childMatchLogic: BudgetCategory['match_logic']
): BudgetCategoryGroup | undefined {
  return pfcGroups.find(
    (g) =>
      g.group !== null &&
      g.group.pfc_primary === pfcPrimary &&
      !g.group.pfc_detailed &&
      isContainingMatchLogic(g.group.match_logic) &&
      isContainedMatchLogic(childMatchLogic)
  )
}

export function groupBudgetCategoriesByPrimary(
  categories: BudgetCategory[]
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

  // Nest custom categories under PFC parents when possible (#1686).
  //
  // The ROW field `custom_parent_pfc_primary` is the ONLY linkage consulted — the map's
  // `parent_pfc_primary` is no longer read here. The row field is authoritative because the DB
  // forbids a custom category from carrying both linkages
  // (000078_add_custom_category_parent_id.up.sql: CHECK (NOT (parent_pfc_primary IS NOT NULL AND
  // parent_id IS NOT NULL)), constraint `custom_category_single_parent`).
  //
  // The parent must be a PRIMARY-ONLY row (`!pfc_detailed`): a detailed row sharing the primary
  // is a SIBLING, not a parent (#1686).
  const standaloneCustom: BudgetCategory[] = []
  for (const cat of customCategories) {
    let nested = false
    if (cat.custom_parent_pfc_primary) {
      const parentGroup = findPfcParentGroup(
        pfcGroups,
        cat.custom_parent_pfc_primary,
        cat.match_logic
      )
      if (parentGroup) {
        parentGroup.children.push(cat)
        nested = true
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
  // Final pass (#1686): a custom whose DIRECT parent is not a budgeted custom — unbudgeted, or
  // budgeted but itself nested under a PFC root — is still a GRANDCHILD of a PFC root when
  // custom_grandparent_pfc_primary names one. Mirrors isSubAllocationWithin's transitive branch
  // (types.ts:146-150) so display nesting stays a strict subset of the remainder arithmetic.
  // Runs AFTER the custom-to-custom pass: a custom already nested under its budgeted custom
  // parent is no longer in customGroupMap and is left alone. Deliberately NOT gated on the
  // direct parent being unbudgeted — a budgeted-but-PFC-nested parent has no custom group
  // either, and its child still belongs under the PFC root.
  for (const [key, group] of customGroupMap) {
    const cat = group.group
    if (!cat || !cat.custom_parent_id || !cat.custom_grandparent_pfc_primary) continue
    const grandparentGroup = findPfcParentGroup(
      pfcGroups,
      cat.custom_grandparent_pfc_primary,
      cat.match_logic
    )
    if (grandparentGroup) {
      grandparentGroup.children.push(cat)
      customGroupMap.delete(key)
    }
  }

  // Re-sort PFC children after the grandparent pass may have added to them.
  for (const group of pfcGroups) {
    group.children.sort(sortByCreatedAt)
  }

  const customGroups = Array.from(customGroupMap.values())
    .sort((a, b) => sortByCreatedAt(a.group as BudgetCategory, b.group as BudgetCategory))
  for (const group of customGroups) {
    group.children.sort(sortByCreatedAt)
  }

  return [...pfcGroups, ...customGroups]
}
