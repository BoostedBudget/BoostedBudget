import {
  useGetPlaidPrimaryCategories,
  useGetPlaidDetailedCategories,
} from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary, Detailed } from '@boostedbudget/api-client/models'

export function useCuratedPfcPrimaries(): Primary[] {
  const { data } = useGetPlaidPrimaryCategories({})
  return data ?? []
}

export function useCuratedPfcDetailed(): Detailed[] {
  const { data } = useGetPlaidDetailedCategories({})
  return data ?? []
}
