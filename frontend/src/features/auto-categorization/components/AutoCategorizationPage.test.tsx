import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import type { CategorizationRule, UseAutoCategorizationRulesResult } from '../types'

const mockUseAutoCategorizationRules = mock(() => ({
  rules: [],
  loading: false,
  error: null,
  createRule: mock(() => Promise.resolve({})),
  updateRule: mock(() => Promise.resolve({})),
  deleteRule: mock(() => Promise.resolve(undefined)),
  toggleRule: mock(() => Promise.resolve({})),
  refresh: mock(() => Promise.resolve(undefined)),
}))

mock.module('../hooks/useAutoCategorizationRules', () => ({
  useAutoCategorizationRules: mockUseAutoCategorizationRules,
}))

// Note: We don't mock useRuleFilters - let it use the real implementation with BrowserRouter
// This prevents mock pollution affecting useRuleFilters.test.ts

import { AutoCategorizationPage } from './AutoCategorizationPage'

// Helper to render with router context
function renderWithRouter(ui: React.ReactElement) {
  return render(<BrowserRouter>{ui}</BrowserRouter>)
}

describe('AutoCategorizationPage', () => {
  afterEach(() => {
    // Reset URL state to prevent pollution to other tests
    window.history.replaceState({}, '', '/')
  })

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

  const createMockResult = (overrides: Partial<UseAutoCategorizationRulesResult> = {}): UseAutoCategorizationRulesResult => ({
    rules: [],
    loading: false,
    error: null,
    createRule: mock(() => Promise.resolve(mockRule1)),
    updateRule: mock(() => Promise.resolve(mockRule1)),
    deleteRule: mock(() => Promise.resolve(undefined)),
    toggleRule: mock(() => Promise.resolve(mockRule1)),
    refresh: mock(() => Promise.resolve(undefined)),
    ...overrides,
  })

  beforeEach(() => {
    mockUseAutoCategorizationRules.mockClear()
  })

  describe('Loading state', () => {
    it('should show loading spinner when loading', () => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ loading: true }))
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/loading rules/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when error occurs', () => {
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ error: 'Failed to fetch rules' })
      )
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/failed to fetch rules/i)).toBeInTheDocument()
    })

    it('should show retry button on error', () => {
      const mockRefresh = mock(() => {})
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ error: 'Network error', refresh: mockRefresh })
      )
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      expect(mockRefresh).toHaveBeenCalled()
    })
  })

  describe('Empty state', () => {
    beforeEach(() => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ rules: [] }))
    })

    it('should show empty state when no rules', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/no rules yet/i)).toBeInTheDocument()
    })

    it('should show create button in empty state', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
    })
  })

  describe('Rules list', () => {
    beforeEach(() => {
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ rules: [mockRule1, mockRule2] })
      )
    })

    it('should render list of rules', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Groceries')).toBeInTheDocument()
    })

    it('should show rule count', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/2 rules/i)).toBeInTheDocument()
    })
  })

  describe('Create rule', () => {
    it('should open editor when Create Rule is clicked', () => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ rules: [mockRule1] }))
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })

    it('should close editor on cancel', () => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ rules: [] }))
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('heading', { name: /create rule/i })).not.toBeInTheDocument()
    })

    it('should show editor form when create button clicked', async () => {
      const mockCreateRule = mock(() => Promise.resolve(mockRule1))
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ rules: [], createRule: mockCreateRule })
      )
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })
  })

  describe('Edit rule', () => {
    it('should open editor with rule data when Edit is clicked', () => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ rules: [mockRule1] }))
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      expect(screen.getByRole('heading', { name: /edit rule/i })).toBeInTheDocument()
      expect(screen.getByDisplayValue('Coffee Shops')).toBeInTheDocument()
    })
  })

  describe('Delete rule', () => {
    it('should call deleteRule when delete is confirmed', async () => {
      const mockDeleteRule = mock(() => Promise.resolve(undefined))
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ rules: [mockRule1], deleteRule: mockDeleteRule })
      )
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)
      const confirmButtons = screen.getAllByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButtons[confirmButtons.length - 1])
      await waitFor(() => {
        expect(mockDeleteRule).toHaveBeenCalledWith('rule-1')
      })
    })
  })

  describe('Toggle rule', () => {
    it('should call toggleRule when toggle is clicked', async () => {
      const mockToggleRule = mock(() => Promise.resolve({ ...mockRule1, enabled: false }))
      mockUseAutoCategorizationRules.mockReturnValue(
        createMockResult({ rules: [mockRule1], toggleRule: mockToggleRule })
      )
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /disable/i }))
      await waitFor(() => {
        expect(mockToggleRule).toHaveBeenCalledWith('rule-1', false)
      })
    })
  })

  describe('Tier limits', () => {
    beforeEach(() => {
      mockUseAutoCategorizationRules.mockReturnValue(createMockResult({ rules: [mockRule1] }))
    })

    it('should show tier limit warning when at limit', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/rule limit reached/i)).toBeInTheDocument()
    })

    it('should disable create button when at limit', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByRole('button', { name: /create rule/i })).toBeDisabled()
    })

    it('should show upgrade prompt for free tier', () => {
      renderWithRouter(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)
      expect(screen.getByText(/upgrade/i)).toBeInTheDocument()
    })
  })
})
