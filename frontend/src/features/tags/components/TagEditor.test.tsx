import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TagEditor } from './TagEditor'
import type { UserTag } from '../types'

describe('TagEditor', () => {
  const mockTags: UserTag[] = [
    {
      id: 'tag-1',
      user_id: 'user-1',
      name: 'Food',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'tag-2',
      user_id: 'user-1',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 3,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'tag-3',
      user_id: 'user-1',
      name: 'Entertainment',
      color: '#3357FF',
      usage_count: 2,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  const defaultProps = {
    selectedTags: ['Food'],
    availableTags: mockTags,
    onChange: vi.fn(),
    disabled: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render selected tags as chips', () => {
      render(<TagEditor {...defaultProps} />)

      expect(screen.getByText('Food')).toBeInTheDocument()
      expect(screen.queryByText('Shopping')).not.toBeInTheDocument()
    })

    it('should render multiple selected tags', () => {
      render(<TagEditor {...defaultProps} selectedTags={['Food', 'Shopping']} />)

      expect(screen.getByText('Food')).toBeInTheDocument()
      expect(screen.getByText('Shopping')).toBeInTheDocument()
    })

    it('should show add tag button', () => {
      render(<TagEditor {...defaultProps} />)

      expect(screen.getByRole('button', { name: /add tag/i })).toBeInTheDocument()
    })

    it('should show empty state when no tags selected', () => {
      render(<TagEditor {...defaultProps} selectedTags={[]} />)

      expect(screen.getByText(/no tags/i)).toBeInTheDocument()
    })
  })

  describe('Tag removal', () => {
    it('should call onChange when removing a tag', () => {
      const onChange = vi.fn()
      render(<TagEditor {...defaultProps} onChange={onChange} />)

      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      expect(onChange).toHaveBeenCalledWith([])
    })

    it('should remove specific tag from multiple', () => {
      const onChange = vi.fn()
      render(
        <TagEditor
          {...defaultProps}
          selectedTags={['Food', 'Shopping']}
          onChange={onChange}
        />
      )

      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      expect(onChange).toHaveBeenCalledWith(['Shopping'])
    })
  })

  describe('Tag addition', () => {
    it('should show dropdown with available tags when add button clicked', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        expect(screen.getByText('Shopping')).toBeInTheDocument()
        expect(screen.getByText('Entertainment')).toBeInTheDocument()
      })
    })

    it('should not show already selected tags in dropdown', async () => {
      render(<TagEditor {...defaultProps} selectedTags={['Food', 'Shopping']} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'Entertainment' })).toBeInTheDocument()
        expect(screen.queryByRole('option', { name: 'Food' })).not.toBeInTheDocument()
        expect(screen.queryByRole('option', { name: 'Shopping' })).not.toBeInTheDocument()
      })
    })

    it('should call onChange when adding a tag', async () => {
      const onChange = vi.fn()
      render(<TagEditor {...defaultProps} onChange={onChange} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      expect(onChange).toHaveBeenCalledWith(['Food', 'Shopping'])
    })

    it('should close dropdown after selection', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      await waitFor(() => {
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })
  })

  describe('Disabled state', () => {
    it('should disable add button when disabled', () => {
      render(<TagEditor {...defaultProps} disabled={true} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      expect(addButton).toBeDisabled()
    })

    it('should disable remove buttons when disabled', () => {
      render(<TagEditor {...defaultProps} disabled={true} />)

      const removeButton = screen.getByRole('button', { name: /remove food/i })
      expect(removeButton).toBeDisabled()
    })
  })

  describe('Tag colors', () => {
    it('should apply tag color to chips', () => {
      render(<TagEditor {...defaultProps} />)

      const tagChip = screen.getByText('Food').closest('.tag-chip')
      expect(tagChip).toHaveStyle({ backgroundColor: '#FF5733' })
    })

    it('should use default color when tag has no color', () => {
      const tagsWithNoColor = [
        {
          ...mockTags[0],
          color: null,
        },
        ...mockTags.slice(1),
      ]

      render(
        <TagEditor
          {...defaultProps}
          availableTags={tagsWithNoColor}
        />
      )

      const tagChip = screen.getByText('Food').closest('.tag-chip')
      expect(tagChip).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })

  describe('Edge cases', () => {
    it('should disable add button when no tags available for selection', () => {
      // All tags are already selected
      render(
        <TagEditor
          {...defaultProps}
          selectedTags={['Food', 'Shopping', 'Entertainment']}
        />
      )

      const addButton = screen.getByRole('button', { name: /add tag/i })
      expect(addButton).toBeDisabled()
    })

    it('should not toggle dropdown when clicking add button while disabled', () => {
      render(<TagEditor {...defaultProps} disabled={true} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      // Dropdown should not appear
      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should handle tag not found in availableTags', () => {
      render(
        <TagEditor
          {...defaultProps}
          selectedTags={['Unknown Tag']}
        />
      )

      // Should still render the tag with default color
      const tagChip = screen.getByText('Unknown Tag').closest('.tag-chip')
      expect(tagChip).toHaveStyle({ backgroundColor: '#6B7280' })
    })
  })
})
