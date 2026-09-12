import type { EffectiveCategory } from './getEffectiveCategory'
import { getDetailedLabel, getPrimaryLabel } from './pfcOptions'
import { formatPFCPrimary } from './formatPfc'
import { titleCaseWords } from '@/shared/text/titleCase'

/** Minimal category metadata shared by transaction, review and editor surfaces. */
export interface CategoryMetadata {
  id: string
  name: string
  parent_id?: string | null
  parent_pfc_primary?: string | null
}

export interface CategoryPresentation {
  categoryLabel: string
  subcategoryLabel: string
  breadcrumb: string
}

/** Explicit hierarchy presentation; the existing leaf-first label helper remains unchanged. */
export function getCategoryPresentation(
  effective: EffectiveCategory | null,
  customCategories: readonly CategoryMetadata[] = [],
): CategoryPresentation {
  const path: string[] = []
  if (!effective) {
    path.push('Uncategorized')
  } else if (effective.kind === 'pfc') {
    const primary = effective.pfc_primary
    path.push(getPrimaryLabel(primary) ?? effective.pfc_primary_display ?? formatPFCPrimary(primary))
    const detailed = effective.pfc_detailed
    if (detailed) {
      const suffix = detailed.startsWith(`${primary}_`) ? detailed.slice(primary.length + 1) : detailed
      path.push(getDetailedLabel(detailed) ?? effective.pfc_detailed_display ?? titleCaseWords(suffix))
    }
  } else {
    const byId = new Map(customCategories.map(category => [category.id, category]))
    let category = byId.get(effective.custom_category_id)
    const visited = new Set<string>()
    while (category && !visited.has(category.id)) {
      visited.add(category.id)
      path.unshift(category.name)
      if (category.parent_pfc_primary) {
        path.unshift(getPrimaryLabel(category.parent_pfc_primary) ?? formatPFCPrimary(category.parent_pfc_primary))
        break
      }
      category = category.parent_id ? byId.get(category.parent_id) : undefined
    }
    if (!path.length) path.push(effective.custom_category_display ?? effective.custom_category_id)
  }
  return {
    categoryLabel: path[0],
    subcategoryLabel: path.length > 1 ? path.slice(1).join(' › ') : '—',
    breadcrumb: path.join(' › '),
  }
}
