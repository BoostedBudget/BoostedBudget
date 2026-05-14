import { useCallback, useRef } from 'react'
import type { SortDirection, SortState } from '@/shared/components/DataTable'
import { useUrlState } from './useUrlState'

export function parseSortParam(
  raw: string | null,
  validColumns: ReadonlySet<string>,
): SortState | null {
  if (!raw) return null
  const parts = raw.split(':')
  if (parts.length !== 2) return null
  const [col, dir] = parts
  if (!col || !dir) return null
  if (!validColumns.has(col)) return null
  if (dir !== 'asc' && dir !== 'desc') return null
  return { columnId: col, direction: dir as SortDirection }
}

export function serializeSortParam(state: SortState | null): string | null {
  if (!state) return null
  return `${state.columnId}:${state.direction}`
}

export function toggleSort(current: SortState | null, columnId: string): SortState {
  if (current?.columnId === columnId) {
    return { columnId, direction: current.direction === 'asc' ? 'desc' : 'asc' }
  }
  return { columnId, direction: 'asc' }
}

export interface UseSortUrlStateOptions {
  defaultSort: SortState | null
  validColumns: ReadonlySet<string>
  paramName?: string
}

export interface UseSortUrlStateReturn {
  sort: SortState | null
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
  isDefaultSort: boolean
}

export function useSortUrlState(opts: UseSortUrlStateOptions): UseSortUrlStateReturn {
  const { defaultSort, validColumns, paramName = 'sort' } = opts

  // eslint-disable-next-line no-undef
  if (process.env.NODE_ENV !== 'production' && defaultSort !== null && !validColumns.has(defaultSort.columnId)) {
    console.warn(
      `[useSortUrlState] defaultSort column "${defaultSort.columnId}" is not in validColumns. Coercing to null.`,
    )
  }
  const resolvedDefault = defaultSort !== null && !validColumns.has(defaultSort.columnId) ? null : defaultSort

  const lastWarnedRaw = useRef<string | null>(null)
  const warnIfInvalid = (raw: string | null) => {
    // eslint-disable-next-line no-undef
    if (process.env.NODE_ENV !== 'production' && raw !== null && raw !== '' && raw !== lastWarnedRaw.current) {
      if (parseSortParam(raw, validColumns) === null) {
        lastWarnedRaw.current = raw
        console.warn(
          `[useSortUrlState] Invalid "${paramName}" URL param: "${raw}". Falling back to default.`,
        )
      }
    }
  }

  const { value: sort, setValue: setSort, reset: resetSort, isDefault: isDefaultSort } = useUrlState<SortState | null>({
    key: paramName,
    parse: (raw) => {
      warnIfInvalid(raw)
      return parseSortParam(raw, validColumns)
    },
    serialize: serializeSortParam,
    defaultValue: resolvedDefault,
    isDefault: (v) => {
      if (v === null && resolvedDefault === null) return true
      if (v === null || resolvedDefault === null) return false
      return v.columnId === resolvedDefault.columnId && v.direction === resolvedDefault.direction
    },
  })

  const toggleSortColumn = useCallback(
    (columnId: string) => {
      setSort(toggleSort(sort, columnId))
    },
    [sort, setSort],
  )

  return { sort, setSort, resetSort, toggleSortColumn, isDefaultSort }
}
