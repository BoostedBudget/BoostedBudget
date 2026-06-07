// Issue #952: Centralized transfer suggestions API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

import * as RealTransferSuggestionsModule from '@boostedbudget/api-client/endpoints/transfer-suggestions/transfer-suggestions'

// === Query key helpers (pass through real implementations) ===
export const getGetTransferSuggestionsQueryKey = RealTransferSuggestionsModule.getGetTransferSuggestionsQueryKey

// === useGetTransferSuggestions ===
export const mockUseGetTransferSuggestions = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
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

// === useDeclineTransferSuggestionMatch ===
export const mockDeclineTransferSuggestionMatchMutate = mock(() => {})
export const mockUseDeclineTransferSuggestionMatch = mock(() => ({
  mutate: mockDeclineTransferSuggestionMatchMutate,
  mutateAsync: mock(() => Promise.resolve({ message: 'Transfer match declined', suggestion: null })),
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
}))
