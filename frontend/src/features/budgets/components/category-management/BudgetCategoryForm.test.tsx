import { describe, it, expect, mock, beforeEach } from 'bun:test'
import type { BudgetCategory } from '../../types'
import type { UserTag } from '@/features/tags/types'

const mockUsePreviewCategories = mock(() => ({
  preview: null,
  loading: false,
  error: null,
}))

mock.module('../../hooks/usePreviewCategories', () => ({
  usePreviewCategories: mockUsePreviewCategories,
}))

import { render, screen, waitFor, act, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BudgetCategoryForm } from './BudgetCategoryForm'

describe('BudgetCategoryForm', () => {
  const mockOnClose = mock(() => {})
  const mockOnSubmit = mock(() => Promise.resolve())

  const mockAvailableCategories: string[][] = [
    ['Food and Drink', 'Restaurants'],
    ['Food and Drink', 'Groceries'],
    ['Transportation', 'Gas'],
    ['Shopping', 'Clothing'],
  ]

  const mockAvailableTags: UserTag[] = [
    {
      id: '1',
      user_id: 'user-1',
      name: 'Essential',
      color: '#22c55e',
      usage_count: 5,
      created_at: '',
      updated_at: '',
    },
    {
      id: '2',
      user_id: 'user-1',
      name: 'Discretionary',
      color: '#f59e0b',
      usage_count: 3,
      created_at: '',
      updated_at: '',
    },
  ]

  const createMockCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory => ({
    id: 'cat-1',
    budget_id: 'budget-1',
    category: ['Food and Drink', 'Restaurants'],
    allocated_amount: 500,
    spent_amount: 250,
    tags: ['Essential'],
    match_logic: 'category_only',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  })

  beforeEach(() => {
    mockOnClose.mockClear()
    mockOnSubmit.mockClear()
    mockOnSubmit.mockResolvedValue(undefined)
    mockUsePreviewCategories.mockClear()
    mockUsePreviewCategories.mockReturnValue({
      preview: null,
      loading: false,
      error: null,
    })
  })

  describe('Create Mode', () => {
    it('should render with empty fields in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      expect(amountInput).toHaveValue(null)
      expect(screen.getByText(/add.*category/i)).toBeInTheDocument()
    })

    it('should show category selector enabled in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect(categorySelect).not.toBeDisabled()
    })

    it('should require category selection before submit', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/category.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should call onSubmit with CreateBudgetCategoryRequest', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'Food and Drink > Restaurants' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '500' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category: ['Food and Drink', 'Restaurants'],
            allocated_amount: 500,
            match_logic: 'category_only',
          })
        )
      })
    })
  })

  describe('Edit Mode', () => {
    it('should pre-fill form with initialData in edit mode', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.getByText(/edit.*category/i)).toBeInTheDocument()

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      expect(amountInput).toHaveValue(500)
    })

    it('should disable category selector in edit mode', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect(categorySelect).toBeDisabled()
    })

    it('should call onSubmit with UpdateBudgetCategoryRequest', async () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '600' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            allocated_amount: 600,
          })
        )
      })
    })
  })

  describe('Validation', () => {
    it('should validate allocated_amount must be greater than 0', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      await user.clear(amountInput)
      await user.type(amountInput, '0')

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })
      await user.click(submitButton)

      await waitFor(() => {
        expect(screen.getByText(/amount.*greater.*0/i)).toBeInTheDocument()
      })

      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should show validation error for empty required fields', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })
      await user.click(submitButton)

      await waitFor(() => {
        expect(screen.getByText(/category.*required/i)).toBeInTheDocument()
      })

      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should validate at least one tag when match_logic is tag_only', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'Food and Drink > Restaurants' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      // Select "Match by tags only" radio option - the real CategoryMatchConfig uses radio buttons
      const tagOnlyRadio = screen.getByRole('radio', { name: /match by tags only/i })
      fireEvent.click(tagOnlyRadio)

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/at least one tag.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })
  })

  describe('Match Logic Integration', () => {
    it('should render CategoryMatchConfig and toggle tag picker based on logic', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // CategoryMatchConfig renders as a fieldset with radio buttons
      expect(screen.getByRole('group', { name: /match logic/i })).toBeInTheDocument()
      expect(screen.queryByText(/^tags$/i)).not.toBeInTheDocument()

      // Select "Match by tags only" radio option to show tags picker
      const tagOnlyRadio = screen.getByRole('radio', { name: /match by tags only/i })
      await user.click(tagOnlyRadio)

      await waitFor(() => {
        expect(screen.getByText(/^tags$/i)).toBeInTheDocument()
      })
    })
  })

  describe('Submit Behavior', () => {
    const fillFormBasic = () => {
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'Food and Drink > Restaurants' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })
    }

    it('should disable submit button while submitting', async () => {
      let resolveSubmit: () => void
      mockOnSubmit.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            resolveSubmit = resolve
          })
      )

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      fillFormBasic()

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(submitButton).toBeDisabled()
      })

      await act(async () => {
        resolveSubmit!()
      })
    })

    it('should show error message on failed submit', async () => {
      mockOnSubmit.mockRejectedValue(new Error('Network error'))

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      fillFormBasic()

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/failed|error/i)).toBeInTheDocument()
    })
  })

  describe('Dialog Behavior', () => {
    it('should close dialog when cancel clicked', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      await user.click(cancelButton)

      expect(mockOnClose).toHaveBeenCalledTimes(1)
    })

    it('should not render when isOpen is false', () => {
      render(
        <BudgetCategoryForm
          isOpen={false}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.queryByText(/add.*category/i)).not.toBeInTheDocument()
    })
  })

  describe('Category Preview', () => {
    it('should show preview when category selected and budget dates provided', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      await waitFor(() => {
        expect(screen.getByTestId('category-preview')).toBeInTheDocument()
        expect(screen.getByText(/matches/i)).toBeInTheDocument()
        expect(screen.getByText('5')).toBeInTheDocument()
        expect(screen.getByText(/\$250\.50/)).toBeInTheDocument()
      })
    })

    it('should show loading state while fetching preview', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: null,
        loading: true,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      await waitFor(() => {
        expect(screen.getByText(/calculating/i)).toBeInTheDocument()
      })
    })

    it('should show error message when preview fails', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: null,
        loading: false,
        error: new Error('Network error'),
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      await waitFor(() => {
        expect(screen.getByText(/unable to load preview/i)).toBeInTheDocument()
      })
    })

    it('should not show preview when no category selected', () => {
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      expect(screen.queryByTestId('category-preview')).not.toBeInTheDocument()
    })

    it('should not show preview when budget dates not provided', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      expect(screen.queryByTestId('category-preview')).not.toBeInTheDocument()
    })

    it('should show singular transaction text when count is 1', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 1, total_amount: 50.00 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, 'Food and Drink > Restaurants')

      await waitFor(() => {
        const preview = screen.getByTestId('category-preview')
        expect(preview).toHaveTextContent(/transaction totaling/i)
        expect(preview).not.toHaveTextContent(/transactions totaling/i)
      })
    })
  })
})
