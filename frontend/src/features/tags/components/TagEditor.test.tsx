import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TagEditor } from './TagEditor'
import * as tagsApi from '../api/tagsApi'
import type { UserTag } from '../types'

// Mock the tags API
vi.mock('../api/tagsApi')

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
    // Mock createTag to return the new tag with an id
    vi.mocked(tagsApi.createTag).mockImplementation(async (request) => ({
      id: 'new-tag-id',
      user_id: 'user-1',
      name: request.name,
      color: request.color || '#6B7280',
      usage_count: 0,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }))
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

  describe('Inline tag creation', () => {
    it('should show "Create new tag" option in dropdown even when availableTags is empty', async () => {
      render(<TagEditor {...defaultProps} availableTags={[]} selectedTags={[]} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        expect(screen.getByText(/create new tag/i)).toBeInTheDocument()
      })
    })

    it('should show "Create new tag" option at bottom of dropdown with existing tags', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        expect(screen.getByText(/create new tag/i)).toBeInTheDocument()
        expect(screen.getByText('Shopping')).toBeInTheDocument()
      })
    })

    it('should show creation form when "Create new tag" is clicked', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      await waitFor(() => {
        expect(screen.getByPlaceholderText(/tag name/i)).toBeInTheDocument()
        expect(screen.getByLabelText(/color/i)).toBeInTheDocument()
        expect(screen.getByRole('button', { name: /create$/i })).toBeInTheDocument()
      })
    })

    it('should call onTagCreated with new tag after creation', async () => {
      const onTagCreated = vi.fn()
      render(<TagEditor {...defaultProps} onTagCreated={onTagCreated} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const nameInput = screen.getByPlaceholderText(/tag name/i)
      fireEvent.change(nameInput, { target: { value: 'New Tag' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(onTagCreated).toHaveBeenCalledWith(expect.objectContaining({
          name: 'New Tag',
        }))
      })
    })

    it('should auto-select newly created tag', async () => {
      const onChange = vi.fn()
      const onTagCreated = vi.fn()
      render(
        <TagEditor
          {...defaultProps}
          onChange={onChange}
          onTagCreated={onTagCreated}
        />
      )

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const nameInput = screen.getByPlaceholderText(/tag name/i)
      fireEvent.change(nameInput, { target: { value: 'New Tag' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(onChange).toHaveBeenCalledWith(['Food', 'New Tag'])
      })
    })

    it('should close creation form and dropdown after successful creation', async () => {
      const onTagCreated = vi.fn()
      render(<TagEditor {...defaultProps} onTagCreated={onTagCreated} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const nameInput = screen.getByPlaceholderText(/tag name/i)
      fireEvent.change(nameInput, { target: { value: 'New Tag' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(screen.queryByPlaceholderText(/tag name/i)).not.toBeInTheDocument()
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      })
    })

    it('should allow creating tag with custom color', async () => {
      const onTagCreated = vi.fn()
      render(<TagEditor {...defaultProps} onTagCreated={onTagCreated} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const nameInput = screen.getByPlaceholderText(/tag name/i)
      fireEvent.change(nameInput, { target: { value: 'New Tag' } })

      const colorInput = screen.getByLabelText(/color/i)
      fireEvent.change(colorInput, { target: { value: '#FF0000' } })

      const createButton = screen.getByRole('button', { name: /create$/i })
      fireEvent.click(createButton)

      await waitFor(() => {
        expect(onTagCreated).toHaveBeenCalledWith(expect.objectContaining({
          name: 'New Tag',
          color: '#ff0000', // Browser returns lowercase hex
        }))
      })
    })

    it('should disable create button when tag name is empty', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const createButton = screen.getByRole('button', { name: /create$/i })
      expect(createButton).toBeDisabled()
    })

    it('should have cancel button to close creation form', async () => {
      render(<TagEditor {...defaultProps} />)

      const addButton = screen.getByRole('button', { name: /add tag/i })
      fireEvent.click(addButton)

      await waitFor(() => {
        const createOption = screen.getByText(/create new tag/i)
        fireEvent.click(createOption)
      })

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      await waitFor(() => {
        expect(screen.queryByPlaceholderText(/tag name/i)).not.toBeInTheDocument()
        // Dropdown should stay open to allow selecting existing tags
        expect(screen.getByRole('listbox')).toBeInTheDocument()
      })
    })
  })

  describe('Edge cases', () => {
    it('should enable add button even when all existing tags are selected (to allow creation)', () => {
      // All tags are already selected, but user can still create new ones
      render(
        <TagEditor
          {...defaultProps}
          selectedTags={['Food', 'Shopping', 'Entertainment']}
        />
      )

      const addButton = screen.getByRole('button', { name: /add tag/i })
      // Button should be enabled to allow creating new tags
      expect(addButton).not.toBeDisabled()
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
