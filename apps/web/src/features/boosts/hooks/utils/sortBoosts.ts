import type { Boost } from '@boostedbudget/api-client/models'
import type { SortColumn, SortDirection } from '../../components/BoostTable'

export function sortBoosts(
  boosts: Boost[],
  sortColumn: SortColumn | undefined,
  sortDirection: SortDirection
): Boost[] {
  if (!sortColumn) return boosts

  return [...boosts].sort((a, b) => {
    // lastRun: nulls always last, regardless of sort direction
    if (sortColumn === 'lastRun') {
      const aRun = a.last_executed_at ?? null
      const bRun = b.last_executed_at ?? null
      if (aRun === null && bRun === null) return 0
      if (aRun === null) return 1
      if (bRun === null) return -1
      const cmp = aRun.localeCompare(bRun)
      return sortDirection === 'asc' ? cmp : -cmp
    }

    let comparison = 0
    switch (sortColumn) {
      case 'name':
        comparison = a.name.localeCompare(b.name)
        break
      case 'type':
        comparison = a.boost_type.localeCompare(b.boost_type)
        break
      case 'status':
        comparison = Number(a.enabled) - Number(b.enabled)
        break
      case 'trigger':
        comparison = (a.trigger_config?.type ?? '').localeCompare(
          b.trigger_config?.type ?? ''
        )
        break
      case 'priority':
        comparison = a.priority - b.priority
        break
    }
    return sortDirection === 'asc' ? comparison : -comparison
  })
}
