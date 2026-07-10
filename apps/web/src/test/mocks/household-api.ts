/**
 * Centralized mock for household API hooks
 * Issue #1370: Household feature — hooks layer
 *
 * Query-key getter functions (getGetMyHouseholdQueryKey, etc.) are intentionally
 * left un-mocked (passed through via the ...RealHouseholdModule spread) — they are
 * pure functions with no side effects, and leaving them real means hook code and
 * test code that both import them see identical, production-accurate query keys.
 */

import { mock } from 'bun:test'
import * as RealHouseholdModule from '@boostedbudget/api-client/endpoints/household/household'

// --- useGetMyHousehold ---
export const mockRefetchMyHousehold = mock(() => Promise.resolve({}))
export const mockUseGetMyHousehold = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchMyHousehold,
}))

// --- useGetHouseholdEntitlement ---
export const mockRefetchHouseholdEntitlement = mock(() => Promise.resolve({}))
export const mockUseGetHouseholdEntitlement = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchHouseholdEntitlement,
}))

// --- useListHouseholdMembers ---
export const mockRefetchHouseholdMembers = mock(() => Promise.resolve({}))
export const mockUseListHouseholdMembers = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchHouseholdMembers,
}))

// --- useRemoveHouseholdMember ---
export const mockRemoveHouseholdMemberMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseRemoveHouseholdMember = mock(() => ({
  mutateAsync: mockRemoveHouseholdMemberMutateAsync,
  isPending: false,
}))

// --- useListHouseholdInvites ---
export const mockRefetchHouseholdInvites = mock(() => Promise.resolve({}))
export const mockUseListHouseholdInvites = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchHouseholdInvites,
}))

// --- useCreateHouseholdInvite ---
export const mockCreateHouseholdInviteMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseCreateHouseholdInvite = mock(() => ({
  mutateAsync: mockCreateHouseholdInviteMutateAsync,
  isPending: false,
}))

// --- useRevokeHouseholdInvite ---
export const mockRevokeHouseholdInviteMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseRevokeHouseholdInvite = mock(() => ({
  mutateAsync: mockRevokeHouseholdInviteMutateAsync,
  isPending: false,
}))

// --- useListMyAccountShares ---
export const mockRefetchMyAccountShares = mock(() => Promise.resolve({}))
export const mockUseListMyAccountShares = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchMyAccountShares,
}))

// --- useSetHouseholdShareVisibility ---
export const mockSetHouseholdShareVisibilityMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseSetHouseholdShareVisibility = mock(() => ({
  mutateAsync: mockSetHouseholdShareVisibilityMutateAsync,
  isPending: false,
}))

// --- useGetHouseholdInvitePreview ---
export const mockRefetchHouseholdInvitePreview = mock(() => Promise.resolve({}))
export const mockUseGetHouseholdInvitePreview = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockRefetchHouseholdInvitePreview,
}))

// --- useAcceptHouseholdInvite ---
export const mockAcceptHouseholdInviteMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseAcceptHouseholdInvite = mock(() => ({
  mutateAsync: mockAcceptHouseholdInviteMutateAsync,
  isPending: false,
}))

// --- useDeclineHouseholdInvite ---
export const mockDeclineHouseholdInviteMutateAsync = mock(() => Promise.resolve(undefined))
export const mockUseDeclineHouseholdInvite = mock(() => ({
  mutateAsync: mockDeclineHouseholdInviteMutateAsync,
  isPending: false,
}))

mock.module('@boostedbudget/api-client/endpoints/household/household', () => ({
  ...RealHouseholdModule,
  useGetMyHousehold: mockUseGetMyHousehold,
  useGetHouseholdEntitlement: mockUseGetHouseholdEntitlement,
  useListHouseholdMembers: mockUseListHouseholdMembers,
  useRemoveHouseholdMember: mockUseRemoveHouseholdMember,
  useListHouseholdInvites: mockUseListHouseholdInvites,
  useCreateHouseholdInvite: mockUseCreateHouseholdInvite,
  useRevokeHouseholdInvite: mockUseRevokeHouseholdInvite,
  useListMyAccountShares: mockUseListMyAccountShares,
  useSetHouseholdShareVisibility: mockUseSetHouseholdShareVisibility,
  useGetHouseholdInvitePreview: mockUseGetHouseholdInvitePreview,
  useAcceptHouseholdInvite: mockUseAcceptHouseholdInvite,
  useDeclineHouseholdInvite: mockUseDeclineHouseholdInvite,
}))
