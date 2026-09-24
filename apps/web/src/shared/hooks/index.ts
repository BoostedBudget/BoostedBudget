/**
 * Shared custom React hooks
 */

export { useTabState } from './useTabState'
export { useDocumentHead } from './useDocumentHead'
export { useCopyToClipboard } from './useCopyToClipboard'
export { useRegions } from './useRegions'
export { useUrlState } from './useUrlState'
export type { UseUrlStateOptions, UseUrlStateReturn } from './useUrlState'
export { useSortUrlState, parseSortParam, serializeSortParam, toggleSort } from './useSortUrlState'
export type { UseSortUrlStateOptions, UseSortUrlStateReturn } from './useSortUrlState'
export { useFilterState } from './useFilterState'
export { useKeyboardShortcuts } from './useKeyboardShortcuts'
export type { KeyMap } from './useKeyboardShortcuts'
export type {
  FilterSpec,
  FilterFieldSpec,
  ScalarCodec,
  UseFilterStateOptions,
  UseFilterStateReturn,
  InferFilters,
} from './useFilterState'
