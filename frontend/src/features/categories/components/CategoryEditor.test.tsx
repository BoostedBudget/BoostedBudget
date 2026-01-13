/**
 * CategoryEditor Component Tests
 * Issue #564: Migrated to mock generated React Query hooks
 * Issue #575: Migrated to bun:test with centralized categories-api mocks
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { CategoryEditor } from './CategoryEditor'
import type { CustomCategory } from '../types'
import { mockCreateCategoryMutateAsync } from '@/test/mocks/categories-api'

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

  await waitFor(() => {
    const createOption = screen.getByText(/create new category/i)
    fireEvent.click(createOption)
  })
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
    selectedCategory: ['Food & Dining'],
    availableCategories: mockCategories,
    onChange: mockOnChange,
    disabled: false,
  }

  beforeEach(() => {
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
    it('should render selected category and dropdown button', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /change category/i })).toBeInTheDocument()
    })

    it('should show empty state when no category selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} selectedCategory={[]} />, { wrapper: Wrapper })

      expect(screen.getByText(/uncategorized/i)).toBeInTheDocument()
    })
  })

  describe('Category selection', () => {
    it('should show dropdown with all categories when button clicked', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'Food & Dining' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Shopping' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Entertainment' })).toBeInTheDocument()
      })
    })

    it('should call onChange when selecting a different category', async () => {
      const onChange = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      expect(onChange).toHaveBeenCalledWith(['Shopping'])
    })

    it('should close dropdown after selection', async () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
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
        const selectedOption = screen.getByRole('option', { name: 'Food & Dining' })
        expect(selectedOption).toHaveClass('selected')
      })
    })
  })

  describe('Clear category', () => {
    it('should show clear button when category is selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByRole('button', { name: /clear category/i })).toBeInTheDocument()
    })

    it('should call onChange with empty array when clear button clicked', () => {
      const onChange = mock(() => {})
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />, { wrapper: Wrapper })

      const clearButton = screen.getByRole('button', { name: /clear category/i })
      fireEvent.click(clearButton)

      expect(onChange).toHaveBeenCalledWith([])
    })

    it('should not show clear button when no category selected', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} selectedCategory={[]} />, { wrapper: Wrapper })

      expect(screen.queryByRole('button', { name: /clear category/i })).not.toBeInTheDocument()
    })
  })

  describe('Disabled state', () => {
    it('should disable buttons when disabled prop is true', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} disabled={true} />, { wrapper: Wrapper })

      expect(screen.getByRole('button', { name: /change category/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /clear category/i })).toBeDisabled()
    })
  })

  describe('Category colors', () => {
    it('should display color indicator for selected category', () => {
      const Wrapper = createWrapper()
      render(<CategoryEditor {...defaultProps} />, { wrapper: Wrapper })

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
        expect(screen.getByRole('option', { name: 'Shopping' })).toBeInTheDocument()
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
        expect(onChange).toHaveBeenCalledWith(['New Category'])
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
      fireEvent.click(createButton)

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
          selectedCategory={['Unknown Category']}
        />,
        { wrapper: Wrapper }
      )

      // Should still render with default color
      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })
})
