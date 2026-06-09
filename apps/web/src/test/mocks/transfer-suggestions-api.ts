// Issue #952: Centralized transfer suggestions API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

import * as RealTransferSuggestionsModule from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'

// === Query key helpers (pass through real implementations) ===
export const getGetTransferSuggestionsQueryKey = RealTransferSuggestionsModule.getGetTransferSuggestionsQueryKey

// === useGetTransferSuggestions ===
export const mockRefetch = mock(() => Promise.resolve())
export const mockUseGetTransferSuggestions = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
  refetch: mockRefetch,
}))

// === useAcceptTransferSuggestion ===
export const mockAcceptTransferSuggestionMutateAsync = mock(() => Promise.resolve({
  message: 'Transfer suggestion accepted',
  suggestion: null,
}))
export const mockUseAcceptTransferSuggestion = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockAcceptTransferSuggestionMutateAsync,
  isPending: false,
  isError: false,
}))

// === useRejectTransferSuggestion ===
export const mockRejectTransferSuggestionMutateAsync = mock(() => Promise.resolve({
  message: 'Transfer suggestion rejected',
  suggestion: null,
}))
export const mockUseRejectTransferSuggestion = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockRejectTransferSuggestionMutateAsync,
  isPending: false,
  isError: false,
}))

// === useConfirmTransferSuggestionMatch ===
export const mockConfirmTransferSuggestionMatchMutateAsync = mock(() => Promise.resolve({
  message: 'Transfer match confirmed',
  suggestion: null,
}))
export const mockUseConfirmTransferSuggestionMatch = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockConfirmTransferSuggestionMatchMutateAsync,
  isPending: false,
  isError: false,
}))

// === useDeclineTransferSuggestionMatch (kept for notification-center usage) ===
export const mockDeclineTransferSuggestionMatchMutate = mock(() => {})
export const mockUseDeclineTransferSuggestionMatch = mock(() => ({
  mutate: mockDeclineTransferSuggestionMatchMutate,
  mutateAsync: mock(() => Promise.resolve({ message: 'Transfer match declined', suggestion: null })),
  isPending: false,
  isError: false,
}))

// === useCancelTransferSuggestion ===
export const mockCancelTransferSuggestionMutate = mock(() => {})
export const mockUseCancelTransferSuggestion = mock(() => ({
  mutate: mockCancelTransferSuggestionMutate,
  mutateAsync: mock(() => Promise.resolve({ message: 'Transfer suggestion cancelled', suggestion: null })),
  isPending: false,
  isError: false,
}))

mock.module('@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions', () => ({
  ...RealTransferSuggestionsModule,
  useGetTransferSuggestions: mockUseGetTransferSuggestions,
  useAcceptTransferSuggestion: mockUseAcceptTransferSuggestion,
  useRejectTransferSuggestion: mockUseRejectTransferSuggestion,
  useConfirmTransferSuggestionMatch: mockUseConfirmTransferSuggestionMatch,
  useDeclineTransferSuggestionMatch: mockUseDeclineTransferSuggestionMatch,
  useCancelTransferSuggestion: mockUseCancelTransferSuggestion,
}))
