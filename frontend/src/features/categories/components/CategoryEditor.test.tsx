import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { CategoryEditor } from './CategoryEditor'
import type { CustomCategory } from '../types'

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

  const defaultProps = {
    selectedCategory: ['Food & Dining'],
    availableCategories: mockCategories,
    onChange: vi.fn(),
    disabled: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render selected category', () => {
      render(<CategoryEditor {...defaultProps} />)

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
    })

    it('should show dropdown button', () => {
      render(<CategoryEditor {...defaultProps} />)

      expect(screen.getByRole('button', { name: /change category/i })).toBeInTheDocument()
    })

    it('should show empty state when no category selected', () => {
      render(<CategoryEditor {...defaultProps} selectedCategory={[]} />)

      expect(screen.getByText(/uncategorized/i)).toBeInTheDocument()
    })
  })

  describe('Category selection', () => {
    it('should show dropdown with all categories when button clicked', async () => {
      render(<CategoryEditor {...defaultProps} />)

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'Food & Dining' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Shopping' })).toBeInTheDocument()
        expect(screen.getByRole('option', { name: 'Entertainment' })).toBeInTheDocument()
      })
    })

    it('should call onChange when selecting a different category', async () => {
      const onChange = vi.fn()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />)

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      expect(onChange).toHaveBeenCalledWith(['Shopping'])
    })

    it('should close dropdown after selection', async () => {
      render(<CategoryEditor {...defaultProps} />)

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
      render(<CategoryEditor {...defaultProps} />)

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
      render(<CategoryEditor {...defaultProps} />)

      expect(screen.getByRole('button', { name: /clear category/i })).toBeInTheDocument()
    })

    it('should call onChange with empty array when clear button clicked', () => {
      const onChange = vi.fn()
      render(<CategoryEditor {...defaultProps} onChange={onChange} />)

      const clearButton = screen.getByRole('button', { name: /clear category/i })
      fireEvent.click(clearButton)

      expect(onChange).toHaveBeenCalledWith([])
    })

    it('should not show clear button when no category selected', () => {
      render(<CategoryEditor {...defaultProps} selectedCategory={[]} />)

      expect(screen.queryByRole('button', { name: /clear category/i })).not.toBeInTheDocument()
    })
  })

  describe('Disabled state', () => {
    it('should disable dropdown button when disabled', () => {
      render(<CategoryEditor {...defaultProps} disabled={true} />)

      const button = screen.getByRole('button', { name: /change category/i })
      expect(button).toBeDisabled()
    })

    it('should disable clear button when disabled', () => {
      render(<CategoryEditor {...defaultProps} disabled={true} />)

      const clearButton = screen.getByRole('button', { name: /clear category/i })
      expect(clearButton).toBeDisabled()
    })
  })

  describe('Category colors', () => {
    it('should display color indicator for selected category', () => {
      render(<CategoryEditor {...defaultProps} />)

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

      render(
        <CategoryEditor
          {...defaultProps}
          availableCategories={categoriesWithNoColor}
        />
      )

      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })

  describe('Edge cases', () => {
    it('should not toggle dropdown when clicking button while disabled', () => {
      render(<CategoryEditor {...defaultProps} disabled={true} />)

      const button = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(button)

      // Dropdown should not appear
      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should handle category not found in availableCategories', () => {
      render(
        <CategoryEditor
          {...defaultProps}
          selectedCategory={['Unknown Category']}
        />
      )

      // Should still render with default color
      const colorIndicator = screen.getByTestId('category-color')
      expect(colorIndicator).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })
})
