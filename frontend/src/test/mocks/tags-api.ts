// Issue #575: Centralized tags API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealTagsModule from '@/api/generated/endpoints/tags/tags'

// === useGetUserTags ===
export const mockRefetchUserTags = mock(() => Promise.resolve())
export const mockUseGetUserTags = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchUserTags,
}))

// === useCreateTag ===
export const mockCreateTagMutateAsync = mock(() => Promise.resolve())
export const mockUseCreateTag = mock(() => ({
  mutateAsync: mockCreateTagMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useDeleteTag ===
export const mockDeleteTagMutateAsync = mock(() => Promise.resolve())
export const mockUseDeleteTag = mock(() => ({
  mutateAsync: mockDeleteTagMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === useGetTagByID ===
export const mockRefetchTagByID = mock(() => Promise.resolve())
export const mockUseGetTagByID = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchTagByID,
}))

// === useUpdateTag ===
export const mockUpdateTagMutateAsync = mock(() => Promise.resolve())
export const mockUseUpdateTag = mock(() => ({
  mutateAsync: mockUpdateTagMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/tags/tags', () => ({
  ...RealTagsModule,
  useGetUserTags: (...args: unknown[]) => mockUseGetUserTags(...args),
  useCreateTag: (...args: unknown[]) => mockUseCreateTag(...args),
  useDeleteTag: (...args: unknown[]) => mockUseDeleteTag(...args),
  useGetTagByID: (...args: unknown[]) => mockUseGetTagByID(...args),
  useUpdateTag: (...args: unknown[]) => mockUseUpdateTag(...args),
}))
