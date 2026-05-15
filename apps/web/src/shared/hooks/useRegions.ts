import { useGetRegions } from '@boostedbudget/api-client/endpoints/regions/regions'
import type { RegionResponse } from '@boostedbudget/api-client/models/regionResponse'

export type { RegionResponse }

export function useRegions() {
  const { data, isLoading, error, refetch } = useGetRegions({
    query: {
      staleTime: 24 * 60 * 60 * 1000, // 24h - regions rarely change
      gcTime: 24 * 60 * 60 * 1000,
    },
  })

  const regions = data?.regions ?? null
  const defaultRegion = data?.default_region ?? null

  function getRegionDisplay(code: string): string {
    return regions?.[code]?.display_name ?? code
  }

  function getRegionWithFlag(code: string): string {
    const region = regions?.[code]
    if (!region) return code
    return `${region.flag} ${region.display_name}`
  }

  return {
    regions,
    defaultRegion,
    loading: isLoading,
    error,
    refetch,
    getRegionDisplay,
    getRegionWithFlag,
  }
}
