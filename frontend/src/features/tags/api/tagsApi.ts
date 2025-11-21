import { apiClient } from '../../../api/axiosConfig'
import type { UserTag, CreateTagRequest, UpdateTagRequest } from '../types'

/**
 * Fetch all tags for the authenticated user
 * GET /api/v1/tags
 */
export async function fetchTags(): Promise<UserTag[]> {
  const response = await apiClient.get<UserTag[]>('/v1/tags')
  return response.data
}

/**
 * Get a specific tag by ID
 * GET /api/v1/tags/:id
 */
export async function fetchTagById(tagId: string): Promise<UserTag> {
  const response = await apiClient.get<UserTag>(`/v1/tags/${tagId}`)
  return response.data
}

/**
 * Create a new tag
 * POST /api/v1/tags
 */
export async function createTag(request: CreateTagRequest): Promise<UserTag> {
  const response = await apiClient.post<UserTag>('/v1/tags', request)
  return response.data
}

/**
 * Update an existing tag
 * PUT /api/v1/tags/:id
 */
export async function updateTag(tagId: string, request: UpdateTagRequest): Promise<UserTag> {
  const response = await apiClient.put<UserTag>(`/v1/tags/${tagId}`, request)
  return response.data
}

/**
 * Delete a tag
 * DELETE /api/v1/tags/:id
 */
export async function deleteTag(tagId: string): Promise<void> {
  await apiClient.delete(`/v1/tags/${tagId}`)
}
