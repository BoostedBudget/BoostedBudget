// Create/update/delete custom categories with cache invalidation + analytics (#1203).
import { useQueryClient } from '@tanstack/react-query'
import {
  useCreateCategory,
  useUpdateCategory,
  useDeleteCategory,
  getGetUserCategoriesQueryKey,
} from '@boostedbudget/api-client/endpoints/categories/categories'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { CreateCategoryRequest, UpdateCategoryRequest, CustomCategory } from '../types'

/** Classifies how a category is parented, for analytics (never sends the name). */
function parentKind(input: { parent_id?: string; parent_pfc_primary?: string }): 'none' | 'pfc' | 'custom' {
  if (input.parent_id) return 'custom'
  if (input.parent_pfc_primary) return 'pfc'
  return 'none'
}

export interface UseCategoryMutationsResult {
  createCategory: (req: CreateCategoryRequest) => Promise<CustomCategory>
  updateCategory: (id: string, req: UpdateCategoryRequest) => Promise<CustomCategory>
  deleteCategory: (id: string) => Promise<void>
  isPending: boolean
}

export function useCategoryMutations(): UseCategoryMutationsResult {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: getGetUserCategoriesQueryKey() })
  }

  const createMutation = useCreateCategory()
  const updateMutation = useUpdateCategory()
  const deleteMutation = useDeleteCategory()

  const createCategory = async (req: CreateCategoryRequest): Promise<CustomCategory> => {
    const created = (await createMutation.mutateAsync({ data: req })) as CustomCategory
    invalidate()
    trackEvent(ANALYTICS_EVENTS.CUSTOM_CATEGORY_CREATED, {
      id: created?.id,
      parent_kind: parentKind(req),
    })
    return created
  }

  const updateCategory = async (id: string, req: UpdateCategoryRequest): Promise<CustomCategory> => {
    const updated = (await updateMutation.mutateAsync({ id, data: req })) as CustomCategory
    invalidate()
    trackEvent(ANALYTICS_EVENTS.CUSTOM_CATEGORY_UPDATED, { id, parent_kind: parentKind(req) })
    return updated
  }

  const deleteCategory = async (id: string): Promise<void> => {
    await deleteMutation.mutateAsync({ id })
    invalidate()
    trackEvent(ANALYTICS_EVENTS.CUSTOM_CATEGORY_DELETED, { id })
  }

  return {
    createCategory,
    updateCategory,
    deleteCategory,
    isPending: createMutation.isPending || updateMutation.isPending || deleteMutation.isPending,
  }
}
