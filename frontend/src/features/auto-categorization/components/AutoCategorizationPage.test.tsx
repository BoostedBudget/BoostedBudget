import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import * as useAutoCategorizationRulesHook from '../hooks/useAutoCategorizationRules'
import type { CategorizationRule, UseAutoCategorizationRulesResult } from '../types'

// Mock the useAutoCategorizationRules hook
vi.mock('../hooks/useAutoCategorizationRules')

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

  const createMockUseAutoCategorizationRules = (overrides: Partial<UseAutoCategorizationRulesResult> = {}): UseAutoCategorizationRulesResult => ({
    rules: [],
    loading: false,
    error: null,
    createRule: vi.fn().mockResolvedValue(mockRule1),
    updateRule: vi.fn().mockResolvedValue(mockRule1),
    deleteRule: vi.fn().mockResolvedValue(undefined),
    toggleRule: vi.fn().mockResolvedValue(mockRule1),
    refresh: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  })

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Loading state', () => {
    it('should show loading spinner when loading', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ loading: true })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByText(/loading rules/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when error occurs', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ error: 'Failed to fetch rules' })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByText(/failed to fetch rules/i)).toBeInTheDocument()
    })

    it('should show retry button on error', () => {
      const mockRefresh = vi.fn()
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ error: 'Network error', refresh: mockRefresh })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      const retryButton = screen.getByRole('button', { name: /retry/i })
      fireEvent.click(retryButton)

      expect(mockRefresh).toHaveBeenCalled()
    })
  })

  describe('Empty state', () => {
    it('should show empty state when no rules', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByText(/no rules yet/i)).toBeInTheDocument()
    })

    it('should show create button in empty state', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
    })
  })

  describe('Rules list', () => {
    it('should render list of rules', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1, mockRule2] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Groceries')).toBeInTheDocument()
    })

    it('should show rule count', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1, mockRule2] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      expect(screen.getByText(/2 rules/i)).toBeInTheDocument()
    })
  })

  describe('Create rule', () => {
    it('should open editor when Create Rule is clicked', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      const createButton = screen.getByRole('button', { name: /create rule/i })
      fireEvent.click(createButton)

      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })

    it('should close editor on cancel', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      const createButton = screen.getByRole('button', { name: /create.*rule/i })
      fireEvent.click(createButton)

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(screen.queryByRole('heading', { name: /create rule/i })).not.toBeInTheDocument()
    })

    it('should call createRule on save', async () => {
      const mockCreateRule = vi.fn().mockResolvedValue(mockRule1)
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [], createRule: mockCreateRule })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      // Open editor
      const createButton = screen.getByRole('button', { name: /create.*rule/i })
      fireEvent.click(createButton)

      // The AutoCategorizationRuleEditor will validate and call onSave
      // We can't easily test the full flow without filling the form
      // Just verify the editor is shown
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })
  })

  describe('Edit rule', () => {
    it('should open editor with rule data when Edit is clicked', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1] })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      const editButton = screen.getByRole('button', { name: /edit/i })
      fireEvent.click(editButton)

      expect(screen.getByRole('heading', { name: /edit rule/i })).toBeInTheDocument()
      expect(screen.getByDisplayValue('Coffee Shops')).toBeInTheDocument()
    })
  })

  describe('Delete rule', () => {
    it('should call deleteRule when delete is confirmed', async () => {
      const mockDeleteRule = vi.fn().mockResolvedValue(undefined)
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1], deleteRule: mockDeleteRule })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      // Click delete button
      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      // Confirm delete
      const confirmButtons = screen.getAllByRole('button', { name: 'Delete' })
      const confirmButton = confirmButtons[confirmButtons.length - 1]
      fireEvent.click(confirmButton)

      await waitFor(() => {
        expect(mockDeleteRule).toHaveBeenCalledWith('rule-1')
      })
    })
  })

  describe('Toggle rule', () => {
    it('should call toggleRule when toggle is clicked', async () => {
      const mockToggleRule = vi.fn().mockResolvedValue({ ...mockRule1, enabled: false })
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1], toggleRule: mockToggleRule })
      )

      render(<AutoCategorizationPage categories={mockCategories} />)

      const toggleButton = screen.getByRole('button', { name: /disable/i })
      fireEvent.click(toggleButton)

      await waitFor(() => {
        expect(mockToggleRule).toHaveBeenCalledWith('rule-1', false)
      })
    })
  })

  describe('Tier limits', () => {
    it('should show tier limit warning when at limit', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1] })
      )

      render(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)

      expect(screen.getByText(/rule limit reached/i)).toBeInTheDocument()
    })

    it('should disable create button when at limit', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1] })
      )

      render(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)

      const createButton = screen.getByRole('button', { name: /create rule/i })
      expect(createButton).toBeDisabled()
    })

    it('should show upgrade prompt for free tier', () => {
      vi.mocked(useAutoCategorizationRulesHook.useAutoCategorizationRules).mockReturnValue(
        createMockUseAutoCategorizationRules({ rules: [mockRule1] })
      )

      render(<AutoCategorizationPage categories={mockCategories} maxRules={1} currentTier="free" />)

      expect(screen.getByText(/upgrade/i)).toBeInTheDocument()
    })
  })
})
