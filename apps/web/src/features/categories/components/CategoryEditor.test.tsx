/**
 * CategoryEditor Component Tests
 * Issue #564: Migrated to mock generated React Query hooks
 * Issue #575: Migrated to bun:test with centralized categories-api mocks
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { CategoryEditor } from './CategoryEditor'
import type { CustomCategory } from '../types'
import { mockCreateCategoryMutateAsync, mockUseGetPlaidDetailedCategories, mockUseGetUserCategories, mockUseGetPlaidPrimaryCategories } from '@/test/mocks/categories-api'

// Helper to create wrapper with QueryClient
function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={new QueryClient({
        defaultOptions: {
          queries: { retry: false },
        },
      })}>
        {children}
      </QueryClientProvider>
    )
  }
}

// Helper to open category creation form
async function openCreationForm() {
  const button = screen.getByRole('button', { name: /change category/i })
  fireEvent.click(button)

  fireEvent.click(screen.getByRole('button', { name: /create new category/i }))
}

describe('CategoryEditor', () => {
  const mockCategories: CustomCategory[] = [
    {
      id: 'cat-1',
      user_id: 'user-1',
      name: 'Food & Dining',
      color: '#FF5733',
      usage_count: 10,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'cat-2',
      user_id: 'user-1',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'cat-3',
      user_id: 'user-1',
      name: 'Entertainment',
      color: '#3357FF',
      usage_count: 3,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  const mockOnChange = mock(() => {})
  const defaultProps = {
    selectedCategory: { kind: 'custom' as const, custom_category_id: 'cat-1' },
    availableCategories: mockCategories,
    onChange: mockOnChange,
    disabled: false,
  }

  beforeEach(() => {
    mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [], isLoading: false, error: null })
    mockUseGetUserCategories.mockReturnValue({ data: undefined, isLoading: false, error: null, refetch: mock(async () => {}) })
    mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null })
    mockCreateCategoryMutateAsync.mockClear()
    mockOnChange.mockClear()
    // Mock createCategory to return the new category with an id
    mockCreateCategoryMutateAsync.mockImplementation(async ({ data }: { data: { name: string; color?: string } }) => ({
      id: 'new-cat-id',
      user_id: 'user-1',
      name: data.name,
      color: data.color || '#6B7280',
      usage_count: 0,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }))
  })

  describe('Rendering', () => {
    it('selects same-name bank and custom categories by distinct identities', () => {
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} availableCategories={[{ ...mockCategories[0], id: 'custom-income', name: 'Income' }]} plaidPrimaries={[{ enum: 'INCOME', display: 'Income' }]} onChange={onChange} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      fireEvent.click(screen.getByRole('option', { name: 'Income Custom' }))
      expect(onChange).toHaveBeenLastCalledWith({ kind: 'custom', custom_category_id: 'custom-income' })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      fireEvent.click(screen.getByRole('option', { name: 'Income' }))
      expect(onChange).toHaveBeenLastCalledWith({ kind: 'pfc', pfc_primary: 'INCOME' })
    })

    it('shows loading without clearing saved custom identity or its fallback label', () => {
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [], isLoading: true, error: null })
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} availableCategories={[]} selectedCategory={{ kind: 'custom', custom_category_id: 'saved' }} currentCategory={{ kind: 'custom', custom_category_id: 'saved', custom_category_display: 'Saved custom' }} onChange={onChange} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      expect(screen.getByRole('status')).toHaveTextContent('Loading categories')
      expect(screen.getByText('Saved custom')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Reset category override' })).toBeEnabled()
      expect(onChange).not.toHaveBeenCalled()
    })
    it('shows the retained bank category after explicitly resetting an override', () => {
      render(<CategoryEditor {...defaultProps} selectedCategory={{ kind: 'clear' }} bankCategory={{ kind: 'pfc', pfc_primary: 'INCOME', pfc_detailed: 'INCOME_WAGES' }} />, { wrapper: createWrapper() })
      expect(screen.getByText('Income › Wages')).toBeInTheDocument()
      expect(screen.queryByText('Uncategorized')).not.toBeInTheDocument()
      expect(screen.getByText('Uses the bank category when available.')).toBeInTheDocument()
    })
    it('Enter selects a directly matching detail instead of its contextual parent', () => {
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [{ enum: 'RENT_AND_UTILITIES_TELEPHONE', primary_enum: 'RENT_AND_UTILITIES', primary_display: 'Rent & Utilities', display: 'Telephone' }], isLoading: false, error: null })
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} onChange={onChange} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'telephone' } })
      fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Enter' })
      expect(onChange).toHaveBeenCalledWith({ kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE' })
    })
    it('searches ancestry and keeps a three-level bank/custom/custom path selectable', () => {
      const nested = [
        { ...mockCategories[0], id: 'parent', name: 'Family', parent_pfc_primary: 'FOOD_AND_DRINK' },
        { ...mockCategories[1], id: 'leaf', name: 'Birthday', parent_id: 'parent' },
      ]
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} selectedCategory={{ kind: 'custom', custom_category_id: 'leaf' }} availableCategories={nested} plaidPrimaries={[{ enum: 'FOOD_AND_DRINK', display: 'Food & Drink' }]} onChange={onChange} />, { wrapper: createWrapper() })
      expect(screen.getByText('Food & Drink › Family › Birthday')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      const search = screen.getByRole('combobox')
      fireEvent.change(search, { target: { value: 'FAMILY BIRTHDAY' } })
      expect(screen.getByRole('option', { name: 'Food & Drink' })).toBeInTheDocument()
      fireEvent.click(screen.getByRole('option', { name: 'Birthday Custom' }))
      expect(onChange).toHaveBeenCalledWith({ kind: 'custom', custom_category_id: 'leaf' })
    })

    it('keeps descendants for parent search and ancestors for leaf search', () => {
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [{ enum: 'FOOD_AND_DRINK_VENDING_MACHINES', primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food & Drink', display: 'Vending Machines' }], isLoading: false, error: null })
      render(<CategoryEditor {...defaultProps} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      for (const query of ['food & drink', 'VENDING']) {
        fireEvent.change(screen.getByRole('combobox'), { target: { value: query } })
        expect(screen.getByRole('option', { name: 'Food & Drink' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Vending Machines' })).toBeInTheDocument()
      }
    })

    it('supports arrows, Enter, Escape, visible active focus and return to Change', () => {
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} availableCategories={[]} plaidPrimaries={[{ enum: 'A', display: 'Alpha' }, { enum: 'B', display: 'Beta' }]} onChange={onChange} />, { wrapper: createWrapper() })
      const change = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(change)
      const search = screen.getByRole('combobox')
      expect(search).toHaveFocus()
      fireEvent.keyDown(search, { key: 'ArrowDown' })
      const beta = screen.getByRole('option', { name: 'Beta' })
      expect(search).toHaveAttribute('aria-activedescendant', beta.id)
      expect(beta).toHaveClass('ring-2')
      fireEvent.keyDown(search, { key: 'Enter' })
      expect(onChange).toHaveBeenCalledWith({ kind: 'pfc', pfc_primary: 'B' })
      expect(change).toHaveFocus()
      fireEvent.click(change)
      fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Escape' })
      expect(change).toHaveFocus()
      expect(onChange).toHaveBeenCalledTimes(1)
    })

    it('preserves unknown saved override during metadata errors and retries without changing it', () => {
      const retry = mock(async () => {})
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [], isLoading: false, error: new Error('offline'), refetch: retry })
      mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null, refetch: retry })
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} selectedCategory={{ kind: 'custom', custom_category_id: 'missing' }} currentCategory={{ kind: 'custom', custom_category_id: 'missing', custom_category_display: 'Saved private name' }} onChange={onChange} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      expect(screen.getByText('Saved private name')).toBeInTheDocument()
      expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded')
      fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
      expect(retry).toHaveBeenCalled()
      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'no match' } })
      expect(screen.getByText('No categories found.')).toBeInTheDocument()
      expect(onChange).not.toHaveBeenCalled()
      fireEvent.click(screen.getByRole('button', { name: 'Reset category override' }))
      expect(onChange).toHaveBeenCalledWith({ kind: 'clear' })
    })

    it('keeps create failures inline with the draft name', async () => {
      mockCreateCategoryMutateAsync.mockRejectedValueOnce(new Error('offline'))
      render(<CategoryEditor {...defaultProps} />, { wrapper: createWrapper() })
      await openCreationForm()
      fireEvent.change(screen.getByLabelText('Category name'), { target: { value: 'Private budget' } })
      fireEvent.click(screen.getByRole('button', { name: 'Create' }))
      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Could not create category'))
      expect(screen.getByLabelText('Category name')).toHaveValue('Private budget')
      expect(mockOnChange).not.toHaveBeenCalled()
    })

    it('keeps custom labels masked and out of option attributes', () => {
      render(<CategoryEditor {...defaultProps} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      const option = screen.getByRole('option', { name: 'Shopping Custom' })
      expect(option.querySelector('[data-ph-mask]')).toHaveTextContent('Shopping')
      for (const attribute of option.attributes) expect(attribute.value).not.toContain('Shopping')
      expect(screen.getByRole('combobox')).toHaveAttribute('data-ph-mask')
    })
    it('searches the complete detailed bank taxonomy and selects its enum', () => {
      mockUseGetPlaidDetailedCategories.mockReturnValue({ data: [{ enum: 'FOOD_AND_DRINK_COFFEE', primary_enum: 'FOOD_AND_DRINK', primary_display: 'Food & Drink', display: 'Coffee' }], isLoading: false, error: null })
      const onChange = mock(() => {})
      render(<CategoryEditor {...defaultProps} onChange={onChange} plaidPrimaries={[{ enum: 'FOOD_AND_DRINK', display: 'Food & Drink' }]} />, { wrapper: createWrapper() })
      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      fireEvent.change(screen.getByRole('combobox', { name: /search categories/i }), { target: { value: 'coffee' } })
      fireEvent.click(screen.getByRole('option', { name: /coffee/i }))
      expect(onChange).toHaveBeenCalledWith({ kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE' })
    })
    it('should render selected category and dropdown button', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /change category/i })).toBeInTheDocument()
    })

    it('should show empty state when no category selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} selectedCategory={{ kind: 'clear' }} />, { wrapper: Wrapper })

      expect(screen.getByText('Bank category')).toBeInTheDocument()
    })
  })

  describe('Category selection', () => {
    it('should show dropdown with all categories when button clicked', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'Food & Dining Custom' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Shopping Custom' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Entertainment Custom' })).toBeInTheDocument()
      })
    })

    it('should call onChange when selecting a different category', async () => {
      const onChange = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping Custom' })
        fireEvent.click(option)
      })

      expect(onChange).toHaveBeenCalledWith({ kind: 'custom', custom_category_id: 'cat-2' })
    })

    it('should close dropdown after selection', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping Custom' })
        fireEvent.click(option)
      })

      await waitFor(() => {
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should highlight currently selected category in dropdown', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const selectedOption = screen.getByRole('option', { name: 'Food & Dining Custom' })
        expect(selectedOption).toHaveClass('bg-primary/5')
      })
    })
  })

  describe('Clear category', () => {
    it('should show clear button when category is selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByRole('button', { name: /reset category override/i })).toBeInTheDocument()
    })

    it('should call onChange with empty array when clear button clicked', () => {
      const onChange = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />, { wrapper: Wrapper })

      const clearButton = screen.getByRole('button', { name: /reset category override/i })
      fireEvent.click(clearButton)

      expect(onChange).toHaveBeenCalledWith({ kind: 'clear' })
    })

    it('should not show clear button when no category selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} selectedCategory={{ kind: 'clear' }} />, { wrapper: Wrapper })

      expect(screen.queryByRole('button', { name: /reset category override/i })).not.toBeInTheDocument()
    })
  })

  describe('Disabled state', () => {
    it('should disable buttons when disabled prop is true', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} disabled={true} />, { wrapper: Wrapper })

      expect(screen.getByRole('button', { name: /change category/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /reset category override/i })).toBeDisabled()
    })
  })

  describe('Category colors', () => {
    it('should display color indicator for selected category', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#FF5733' })
    })

    it('resolves an existing custom override passed by id to its name and color (#1203)', () => {
      // Transactions carry an existing custom override as a raw id (custom_category_id),
      // so the editor must match the option by key (id), not only by pfc/name.
      const Wrapper = createWrapper()
      render(
        <CategoryEditor {...defaultProps} selectedCategory={{ kind: 'custom', custom_category_id: 'cat-1' }} />,
        { wrapper: Wrapper }
      )

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#FF5733' })
    })

    it('should use default color when category has no color', () => {
      const categoriesWithNoColor = [
        {
          ...mockCategories[0],
          color: null,
        },
        ...mockCategories.slice(1),
      ]

      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={categoriesWithNoColor}
        />,
        { wrapper: Wrapper }
      )

      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })

  describe('Inline category creation', () => {
    it('should show "Create new category" option in dropdown', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByText(/create new category/i)).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Shopping Custom' })).toBeInTheDocument()
      })
    })

    it('should show creation form with name, color, and create button', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      await openCreationForm()

      expect(screen.getByPlaceholderText(/category name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/color/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /create$/i })).toBeInTheDocument()
    })

    it('should call onCategoryCreated and auto-select newly created category', async () => {
      const onChange = mock(() => {})
      const onCategoryCreated = mock(() => {})
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          onChange={onChange}
          onCategoryCreated={onCategoryCreated}
        />,
        { wrapper: Wrapper }
      )

      await openCreationForm()

      const nameInput = screen.getByPlaceholderText(/category name/i)
      fireEvent.change(nameInput, { target: { value: 'New Category' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(onCategoryCreated).toHaveBeenCalledWith(expect.objectContaining({
          name: 'New Category',
        }))
        expect(onChange).toHaveBeenCalledWith({ kind: 'custom', custom_category_id: 'new-cat-id' })
      })
    })

    it('should close creation form and dropdown after successful creation', async () => {
      const onCategoryCreated = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onCategoryCreated={onCategoryCreated} />, { wrapper: Wrapper })

      await openCreationForm()

      const nameInput = screen.getByPlaceholderText(/category name/i)
      fireEvent.change(nameInput, { target: { value: 'New Category' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      await act(async () => { fireEvent.click(createButton) })

      await waitFor(() => {
        expect(screen.queryByPlaceholderText(/category name/i)).not.toBeInTheDocument()
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should allow creating category with custom color', async () => {
      const onCategoryCreated = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onCategoryCreated={onCategoryCreated} />, { wrapper: Wrapper })

      await openCreationForm()

      const nameInput = screen.getByPlaceholderText(/category name/i)
      fireEvent.change(nameInput, { target: { value: 'New Category' } })

      const colorInput = screen.getByLabelText(/color/i)
      fireEvent.change(colorInput, { target: { value: '#ff0000' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(onCategoryCreated).toHaveBeenCalledWith(expect.objectContaining({
          name: 'New Category',
          color: '#ff0000',
        }))
      })
    })

    it('should disable create button when name is empty and allow cancel', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      await openCreationForm()

      const createButton = screen.getByRole('button', { name: /create$/i })
      expect(createButton).toBeDisabled()

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      await waitFor(() => {
        expect(screen.queryByPlaceholderText(/category name/i)).not.toBeInTheDocument()
        expect(screen.getByRole('listbox')).toBeInTheDocument()
      })
    })
  })

  describe('Edge cases', () => {
    it('should not toggle dropdown when clicking button while disabled', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} disabled={true} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      // Dropdown should not appear
      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should handle category not found in availableCategories', () => {
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          selectedCategory={{ kind: 'custom', custom_category_id: 'Unknown Category' }}
        />,
        { wrapper: Wrapper }
      )

      // Should still render with default color
      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })

  describe('plaidPrimaries merging', () => {
    const plaidPrimaries = [
      { enum: 'FOOD_AND_DRINK', display: 'Food And Drink' },
      { enum: 'TRANSPORTATION', display: 'Transportation' },
      { enum: 'INCOME', display: 'Income' },
    ]

    it('merges_custom_and_plaid_primaries', async () => {
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={[mockCategories[0]]} // only Food & Dining
          plaidPrimaries={plaidPrimaries}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'Food & Dining Custom' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Transportation' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Income' })).toBeInTheDocument()
      })
    })

    it('keeps_same_name_custom_and_bank_options_distinct', async () => {
      const customEntertainment: CustomCategory = {
        id: 'ent-custom',
        user_id: 'user-1',
        name: 'entertainment', // lowercase version
        color: '#ABCDEF',
        usage_count: 2,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }

      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={[customEntertainment]}
          plaidPrimaries={[{ enum: 'ENTERTAINMENT', display: 'Entertainment' }]}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        // Exactly one entry matching "entertainment" (custom wins, keeps custom casing)
        const options = screen.getAllByRole('option')
        const entOptions = options.filter(opt =>
          opt.textContent?.trim().toLowerCase().startsWith('entertainment')
        )
        expect(entOptions).toHaveLength(2)
      })
    })

    it('offers_explicit_reset_without_hiding_categories_named_uncategorized', async () => {
      const uncatCustom: CustomCategory = {
        id: 'uncat-custom',
        user_id: 'user-1',
        name: 'Uncategorized',
        color: '#000000',
        usage_count: 0,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      }

      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={[uncatCustom]}
          plaidPrimaries={[{ enum: 'OTHER', display: 'Uncategorized' }]}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const options = screen.getAllByRole('option')
        expect(options.filter(option => option.textContent?.startsWith('Uncategorized'))).toHaveLength(2)
        expect(screen.getByRole('button', { name: 'Reset category override' })).toBeInTheDocument()
      })
    })

    it('sorts_alphabetically', async () => {
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={[]}
          plaidPrimaries={[
            { enum: 'TRANSPORTATION', display: 'Transportation' },
            { enum: 'FOOD_AND_DRINK', display: 'Food And Drink' },
            { enum: 'INCOME', display: 'Income' },
          ]}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const options = screen.getAllByRole('option')
        const labels = options
          .map(o => o.textContent?.trim())
          .filter(Boolean)
          .filter(l => l !== 'Uncategorized')
        expect(labels).toEqual(['Food And Drink', 'Income', 'Transportation'])
      })
    })

    it('falls_back_when_plaidPrimaries_undefined', async () => {
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          plaidPrimaries={undefined}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        // Original custom categories still shown
        expect(screen.getByRole('option', { name: 'Food & Dining Custom' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Shopping Custom' })).toBeInTheDocument()
      })
    })

    it('dropdown_renders_actual_option_nodes_after_open', async () => {
      const Wrapper = createWrapper()
      render(
        <CategoryEditor
          {...defaultProps}
          plaidPrimaries={plaidPrimaries}
          selectedCategory={{ kind: 'clear' }}
        />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const listbox = screen.getByRole('listbox')
        expect(listbox).toBeInTheDocument()
        const optionNodes = screen.getAllByRole('option')
        // Uncategorized + custom (3) + PFC-only (Transportation, Income — Food And Drink deduped with Food & Dining? No, different names)
        // Actually: custom = Food & Dining, Shopping, Entertainment (3); PFC = Food And Drink, Transportation, Income (3 distinct)
        // Total = 3 + 3 + 1 (Uncategorized sentinel) = 7
        expect(optionNodes.length).toBeGreaterThan(3)
      })
    })
  })
})
