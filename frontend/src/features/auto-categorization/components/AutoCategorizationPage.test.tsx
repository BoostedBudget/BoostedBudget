/**
 * AutoCategorizationPage Tests
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * NOTE: Uses API-level mocks to test the page with real useAutoCategorizationRules hook.
 * This prevents mock pollution that would break useAutoCategorizationRules.test.tsx.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { CategorizationRule } from '../types'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import {
  mockUseGetUserRules,
  mockRefetchUserRules,
  mockCreateRuleMutateAsync,
  mockUseCreateRule,
  mockUpdateRuleMutateAsync,
  mockUseUpdateRule,
  mockDeleteRuleMutateAsync,
  mockUseDeleteRule,
  mockToggleRuleMutateAsync,
  mockUseToggleRule,
} from '@/test/mocks/auto-categorization-api'

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{ui}</BrowserRouter>
    </QueryClientProvider>
  )
}

describe('AutoCategorizationPage', () => {
  const mockRule1: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    condition_logic: 'AND',
    target_category: ['Food & Drink', 'Coffee'],
    target_tags: ['morning'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  const mockRule2: CategorizationRule = {
    ...mockRule1,
    id: 'rule-2',
    name: 'Groceries',
    priority: 5,
  }

  const mockCategories = [
    ['Food & Drink'],
    ['Food & Drink', 'Coffee'],
    ['Shopping'],
  ]

  function configureQuery(config: {
    data?: CategorizationRule[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetUserRules.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchUserRules,
    })
  }

  function configureMutations() {
    mockUseCreateRule.mockReturnValue({
      mutateAsync: mockCreateRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseUpdateRule.mockReturnValue({
      mutateAsync: mockUpdateRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseDeleteRule.mockReturnValue({
      mutateAsync: mockDeleteRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseToggleRule.mockReturnValue({
      mutateAsync: mockToggleRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
  }

  beforeEach(() => {
    mockUseGetUserRules.mockClear()
    mockRefetchUserRules.mockClear()
    mockCreateRuleMutateAsync.mockClear()
    mockUseCreateRule.mockClear()
    mockUpdateRuleMutateAsync.mockClear()
    mockUseUpdateRule.mockClear()
    mockDeleteRuleMutateAsync.mockClear()
    mockUseDeleteRule.mockClear()
    mockToggleRuleMutateAsync.mockClear()
    mockUseToggleRule.mockClear()

    // Default configuration - not loading, no error, empty rules
    configureQuery({ data: [] })
    configureMutations()
    mockCreateRuleMutateAsync.mockResolvedValue(mockRule1)
    mockUpdateRuleMutateAsync.mockResolvedValue(mockRule1)
    mockDeleteRuleMutateAsync.mockResolvedValue({})
    mockToggleRuleMutateAsync.mockResolvedValue({ ...mockRule1, enabled: false })
  })

  afterEach(() => {
    window.history.replaceState({}, '', '/')
  })

  describe('Loading state', () => {
    it('should show loading spinner when loading', () => {
      configureQuery({ isLoading: true })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/loading rules/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when error occurs', () => {
      configureQuery({ error: new Error('Failed to fetch rules') })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/failed to fetch rules/i)).toBeInTheDocument()
    })

    it('should show retry button on error', async () => {
      configureQuery({ error: new Error('Network error') })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      await waitFor(() => {
        expect(mockRefetchUserRules).toHaveBeenCalled()
      })
    })
  })

  describe('Empty state', () => {
    it('should show empty state when no rules', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/no rules yet/i)).toBeInTheDocument()
    })

    it('should show create button in empty state', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
    })
  })

  describe('Rules list', () => {
    it('should render list of rules', () => {
      configureQuery({ data: [mockRule1, mockRule2] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Groceries')).toBeInTheDocument()
    })

    it('should show rule count', () => {
      configureQuery({ data: [mockRule1, mockRule2] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/2 rules/i)).toBeInTheDocument()
    })
  })

  describe('Create rule', () => {
    it('should open editor when Create Rule is clicked', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })

    it('should close editor on cancel', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('heading', { name: /create rule/i })).not.toBeInTheDocument()
    })

    it('should show editor form when create button clicked', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })
  })

  describe('Edit rule', () => {
    it('should open editor with rule data when Edit is clicked', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      expect(screen.getByRole('heading', { name: /edit rule/i })).toBeInTheDocument()
      expect(screen.getByDisplayValue('Coffee Shops')).toBeInTheDocument()
    })
  })

  describe('Delete rule', () => {
    it('should call deleteRule when delete is confirmed', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)
      const confirmButtons = screen.getAllByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButtons[confirmButtons.length - 1])
      await waitFor(() => {
        expect(mockDeleteRuleMutateAsync).toHaveBeenCalled()
      })
    })
  })

  describe('Toggle rule', () => {
    it('should call toggleRule when toggle is clicked', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /disable/i }))
      await waitFor(() => {
        expect(mockToggleRuleMutateAsync).toHaveBeenCalled()
      })
    })
  })

  describe('Tier limits', () => {
    it('should show tier limit warning when at limit', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/rule limit reached/i)).toBeInTheDocument()
    })

    it('should disable create button when at limit', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByRole('button', { name: /create rule/i })).toBeDisabled()
    })

    it('should show upgrade prompt for free tier', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/upgrade/i)).toBeInTheDocument()
    })
  })
})
