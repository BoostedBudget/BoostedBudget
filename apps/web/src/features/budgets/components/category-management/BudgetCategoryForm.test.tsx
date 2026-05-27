import { describe, it, expect, mock, beforeEach } from 'bun:test'
import type { BudgetCategory } from '../../types'
import type { UserTag } from '@/features/tags/types'
import type { CustomCategory } from '@boostedbudget/api-client/models'

const mockUsePreviewCategories = mock(() => ({
  preview: null,
  loading: false,
  error: null,
}))

mock.module('../../hooks/usePreviewCategories', () => ({
  usePreviewCategories: mockUsePreviewCategories,
}))

const mockUseGetUserCategories = mock(() => ({
  data: [] as CustomCategory[],
  isLoading: false,
  error: null,
}))

mock.module('@boostedbudget/api-client/endpoints/categories/categories', () => ({
  useGetUserCategories: mockUseGetUserCategories,
}))

import { render, screen, waitFor, act, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BudgetCategoryForm } from './BudgetCategoryForm'

// Minimal CustomCategory factory
const makeCustomCategory = (
  id: string,
  name: string,
  parent_pfc_primary?: string
): CustomCategory => ({
  id,
  user_id: 'user-1',
  name,
  parent_pfc_primary: parent_pfc_primary ?? undefined,
  color: undefined,
  icon: undefined,
  usage_count: 0,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
})

// Default mock user categories (used across most tests)
const defaultUserCategories: CustomCategory[] = [
  makeCustomCategory('cat-uuid-1', 'Restaurants', 'FOOD_AND_DRINK'),
  makeCustomCategory('cat-uuid-2', 'Groceries'),
  makeCustomCategory('cat-uuid-3', 'Gas', 'TRANSPORTATION'),
  makeCustomCategory('cat-uuid-4', 'Clothing'),
]

describe('BudgetCategoryForm', () => {
  const mockOnClose = mock(() => {})
  const mockOnSubmit = mock(() => Promise.resolve())

  // PFC enum-style categories (used only for the PFC tree picker path)
  const mockAvailableCategories: string[][] = [
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANT'],
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_GROCERIES'],
    ['TRANSPORTATION', 'TRANSPORTATION_GAS'],
  ]

  // PFC enum-style categories for testing the PFC path
  const mockPFCAvailableCategories: string[][] = [
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANT'],
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_GROCERIES'],
    ['TRANSPORTATION', 'TRANSPORTATION_GAS'],
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
    category_kind: 'pfc',
    pfc_primary: 'Food and Drink',
    pfc_detailed: 'Restaurants',
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
    mockUseGetUserCategories.mockClear()
    mockUseGetUserCategories.mockReturnValue({
      data: defaultUserCategories,
      isLoading: false,
      error: null,
    })
  })

  /** Helper: switch the kind toggle to 'custom' */
  async function switchToCustom() {
    const customBtn = screen.getByRole('button', { name: /custom categories/i })
    fireEvent.click(customBtn)
  }

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

    it('should show category kind toggle buttons in create mode', () => {
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

      expect(screen.getByRole('button', { name: /plaid categories/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /custom categories/i })).toBeInTheDocument()
    })

    it('should show HierarchicalCategoryTreeSelect when kind is pfc', () => {
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

      // The tree select trigger button is rendered (aria-label from the component)
      expect(screen.getByRole('button', { name: /select pfc category/i })).toBeInTheDocument()
      // The flat select should NOT be visible
      expect(screen.queryByRole('combobox', { name: /^category$/i })).not.toBeInTheDocument()
    })

    it('should show flat select when kind is custom', async () => {
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect(categorySelect).not.toBeDisabled()
    })

    it('should NOT show Custom Categories button when useGetUserCategories returns empty', () => {
      mockUseGetUserCategories.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
      })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockPFCAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.queryByRole('button', { name: /custom categories/i })).not.toBeInTheDocument()
    })

    it('should clear pfc fields when switching to custom', async () => {
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

      // Switch to custom and select
      await switchToCustom()
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      // Switch back to pfc - custom_category_id should be cleared
      const pfcBtn = screen.getByRole('button', { name: /plaid categories/i })
      fireEvent.click(pfcBtn)

      // Tree select is shown again
      expect(screen.getByRole('button', { name: /select pfc category/i })).toBeInTheDocument()
      // Flat select is gone
      expect(screen.queryByRole('combobox', { name: /^category$/i })).not.toBeInTheDocument()
    })

    it('should require category selection before submit (pfc mode)', async () => {
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

    it('should require category selection before submit (custom mode)', async () => {
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

      await switchToCustom()

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/category.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should call onSubmit with CreateBudgetCategoryRequest (custom mode)', async () => {
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '500' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: 'cat-uuid-1',
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

    it('should disable category kind toggle buttons in edit mode', () => {
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

      const pfcBtn = screen.getByRole('button', { name: /plaid categories/i })
      expect(pfcBtn).toBeDisabled()

      // Custom Categories button only shown when custom categories exist
      const customBtn = screen.queryByRole('button', { name: /custom categories/i })
      if (customBtn) {
        expect(customBtn).toBeDisabled()
      }
    })

    it('should show tree select disabled in edit mode when kind is pfc', () => {
      const category = createMockCategory({ category_kind: 'pfc' })
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

      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      expect(treeSelectBtn).toBeDisabled()
    })

    it('should show flat select disabled in edit mode when kind is custom', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: '',
        pfc_detailed: '',
        custom_category_id: 'cat-uuid-1',
      })
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

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
    const fillFormBasic = async () => {
      await switchToCustom()
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

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

      await fillFormBasic()

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

      await fillFormBasic()

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

  describe('Soft Warning — allocation exceeds expected income', () => {
    it('TestSoftWarning_ShowsWhenAllocationExceedsIncome — shows warning and keeps save enabled', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          expectedIncome={1000}
          totalAllocatedExcludingCurrent={900}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '200' } })

      // Warning should be visible (900 + 200 = 1100 > 1000)
      expect(await screen.findByText(/exceed/i)).toBeInTheDocument()

      // Save button must remain enabled
      const saveButton = screen.getByRole('button', { name: /save|add|create/i })
      expect(saveButton).not.toBeDisabled()
    })

    it('TestSoftWarning_HiddenWhenUnder — no warning when allocation is within income', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          expectedIncome={1000}
          totalAllocatedExcludingCurrent={900}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '50' } })

      // No warning (900 + 50 = 950 <= 1000)
      expect(screen.queryByText(/exceed/i)).not.toBeInTheDocument()
    })
  })

  describe('Custom Category UUID fix (Gap 1)', () => {
    it('submits real UUID as custom_category_id when a custom category is selected', async () => {
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-2' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '300' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: 'cat-uuid-2',
          })
        )
        // Must NOT be a name string — verify it's a UUID-shaped value
        const call = mockOnSubmit.mock.calls[0][0] as { custom_category_id?: string }
        expect(call.custom_category_id).toBe('cat-uuid-2')
        expect(call.custom_category_id).not.toContain('|')
        expect(call.custom_category_id).not.toContain('Food')
      })
    })

    it('shows Custom Categories button when useGetUserCategories returns categories', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={[]}
          availableTags={mockAvailableTags}
        />
      )

      // Button visible because mockUseGetUserCategories returns 4 categories by default
      expect(screen.getByRole('button', { name: /custom categories/i })).toBeInTheDocument()
    })

    it('groups custom categories with parent_pfc_primary into optgroups', async () => {
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })

      // "Restaurants" (FOOD_AND_DRINK parent) and "Gas" (TRANSPORTATION parent)
      // should be rendered as options inside optgroups
      expect(categorySelect.querySelector('option[value="cat-uuid-1"]')).not.toBeNull()
      expect(categorySelect.querySelector('option[value="cat-uuid-3"]')).not.toBeNull()

      // Check optgroup elements exist (at least one group with parent label)
      const optgroups = categorySelect.querySelectorAll('optgroup')
      expect(optgroups.length).toBeGreaterThan(0)

      // "Groceries" and "Clothing" have no parent — should be in a "no parent" group
      expect(categorySelect.querySelector('option[value="cat-uuid-2"]')).not.toBeNull()
      expect(categorySelect.querySelector('option[value="cat-uuid-4"]')).not.toBeNull()
    })

    it('pre-fills select with existing UUID in edit mode', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'cat-uuid-3',
      })
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
      expect((categorySelect as HTMLSelectElement).value).toBe('cat-uuid-3')
    })
  })

  describe('Category form submission — PFC and Custom paths', () => {
    it('submits pfc_primary with pfc_detailed undefined when only primary selected', async () => {
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

      // Open the PFC tree picker and select only a primary category
      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      fireEvent.click(treeSelectBtn)

      // Click on the FOOD_AND_DRINK primary row (no detailed)
      const primaryRow = screen.getByTestId('primary-row-FOOD_AND_DRINK')
      fireEvent.click(primaryRow)

      // Fill in the amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '150' } })

      // Submit
      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'pfc',
            pfc_primary: 'FOOD_AND_DRINK',
            pfc_detailed: undefined,
            allocated_amount: 150,
          })
        )
      })
    })

    it('submits both pfc_primary and pfc_detailed when both are selected', async () => {
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

      // Open the PFC tree picker
      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      fireEvent.click(treeSelectBtn)

      // Expand FOOD_AND_DRINK to see detailed options
      fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))

      // Select a detailed sub-category
      fireEvent.click(screen.getByTestId('detailed-row-FOOD_AND_DRINK_RESTAURANT'))

      // Fill in the amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '200' } })

      // Submit
      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'pfc',
            pfc_primary: 'FOOD_AND_DRINK',
            pfc_detailed: 'FOOD_AND_DRINK_RESTAURANT',
            allocated_amount: 200,
          })
        )
      })
    })

    it('submits real UUID as custom_category_id when a custom category is selected', async () => {
      const realUUID = 'f47ac10b-58cc-4372-a567-0e02b2c3d479'
      mockUseGetUserCategories.mockReturnValue({
        data: [makeCustomCategory(realUUID, 'Restaurants', 'FOOD_AND_DRINK')],
        isLoading: false,
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

      await switchToCustom()

      // Select a custom category by its real UUID (not its display name)
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: realUUID } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '75' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: realUUID,
            allocated_amount: 75,
          })
        )
        // Ensure it's the UUID, not the category name or any concatenated string
        const call = mockOnSubmit.mock.calls[0][0] as { custom_category_id?: string }
        expect(call.custom_category_id).toBe('f47ac10b-58cc-4372-a567-0e02b2c3d479')
        expect(call.custom_category_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i)
        expect(call.custom_category_id).not.toContain('Restaurants')
        expect(call.custom_category_id).not.toContain('|')
      })
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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

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

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        const preview = screen.getByTestId('category-preview')
        expect(preview).toHaveTextContent(/transaction totaling/i)
        expect(preview).not.toHaveTextContent(/transactions totaling/i)
      })
    })
  })
})
