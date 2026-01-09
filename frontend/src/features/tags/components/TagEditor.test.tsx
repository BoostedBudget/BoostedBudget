/**
 * TagEditor Component Tests
 * Issue #564: Frontend Test Coverage Improvement
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { UserTag } from '../types'

// Mock the generated mutation hook
const mockMutateAsync = mock(() => Promise.resolve({}))

mock.module('@/api/generated/endpoints/tags/tags', () => ({
  useCreateTag: () => ({
    mutateAsync: mockMutateAsync,
    isPending: false,
  }),
}))

import { TagEditor } from './TagEditor'

// Mock tags data
function createMockTags(): UserTag[] {
  return [
    { id: 'tag-1', user_id: 'user-1', name: 'Essential', color: '#FF5733', usage_count: 5, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
    { id: 'tag-2', user_id: 'user-1', name: 'Discretionary', color: '#33FF57', usage_count: 3, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
    { id: 'tag-3', user_id: 'user-1', name: 'Business', color: '#3357FF', usage_count: 2, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
  ]
}

const mockOnChange = mock(() => {})
const mockOnTagCreated = mock(() => {})

describe('TagEditor', () => {
  beforeEach(() => {
    mockMutateAsync.mockClear()
    mockOnChange.mockClear()
    mockOnTagCreated.mockClear()
  })

  describe('Rendering', () => {
    it('should render the component', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('+ Add tag')).toBeInTheDocument()
    })

    it('should show "No tags" when no tags selected', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('No tags')).toBeInTheDocument()
    })

    it('should render selected tag chips', () => {
      render(
        <TagEditor
          selectedTags={['Essential', 'Business']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByText('Essential')).toBeInTheDocument()
      expect(screen.getByText('Business')).toBeInTheDocument()
    })

    it('should render tag chips with correct colors', () => {
      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      const tagChip = screen.getByText('Essential').closest('.tag-chip') as HTMLElement
      // Happy-dom returns hex, jsdom returns rgb
      expect(['rgb(255, 87, 51)', '#FF5733']).toContain(tagChip.style.backgroundColor)
    })

    it('should render remove button for each selected tag', () => {
      render(
        <TagEditor
          selectedTags={['Essential', 'Business']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      expect(screen.getByLabelText('Remove Essential')).toBeInTheDocument()
      expect(screen.getByLabelText('Remove Business')).toBeInTheDocument()
    })
  })

  describe('Dropdown Behavior', () => {
    it('should not show dropdown by default', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should show dropdown when Add tag clicked', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      expect(screen.getByRole('listbox')).toBeInTheDocument()
    })

    it('should close dropdown when Add tag clicked again', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      expect(screen.getByRole('listbox')).toBeInTheDocument()

      fireEvent.click(screen.getByText('+ Add tag'))
      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })

    it('should show available tags in dropdown', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      expect(screen.getByLabelText('Essential')).toBeInTheDocument()
      expect(screen.getByLabelText('Discretionary')).toBeInTheDocument()
      expect(screen.getByLabelText('Business')).toBeInTheDocument()
    })

    it('should not show already selected tags in dropdown', () => {
      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      expect(screen.queryByLabelText('Essential')).not.toBeInTheDocument()
      expect(screen.getByLabelText('Discretionary')).toBeInTheDocument()
      expect(screen.getByLabelText('Business')).toBeInTheDocument()
    })

    it('should show Create new tag option in dropdown', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      expect(screen.getByText('+ Create new tag')).toBeInTheDocument()
    })
  })

  describe('Tag Selection', () => {
    it('should add tag to selection when clicked in dropdown', () => {
      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByLabelText('Discretionary'))

      expect(mockOnChange).toHaveBeenCalledWith(['Essential', 'Discretionary'])
    })

    it('should close dropdown after selecting a tag', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByLabelText('Essential'))

      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })
  })

  describe('Tag Removal', () => {
    it('should remove tag when remove button clicked', () => {
      render(
        <TagEditor
          selectedTags={['Essential', 'Business']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByLabelText('Remove Essential'))

      expect(mockOnChange).toHaveBeenCalledWith(['Business'])
    })

    it('should remove last tag correctly', () => {
      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByLabelText('Remove Essential'))

      expect(mockOnChange).toHaveBeenCalledWith([])
    })
  })

  describe('Tag Creation', () => {
    it('should show create form when Create new tag clicked', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))

      expect(screen.getByPlaceholderText('Tag name')).toBeInTheDocument()
      expect(screen.getByLabelText('Color')).toBeInTheDocument()
    })

    it('should have Create button disabled when tag name empty', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))

      expect(screen.getByText('Create')).toBeDisabled()
    })

    it('should enable Create button when tag name entered', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))
      fireEvent.change(screen.getByPlaceholderText('Tag name'), { target: { value: 'New Tag' } })

      expect(screen.getByText('Create')).not.toBeDisabled()
    })

    it('should hide create form when Cancel clicked', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))
      fireEvent.click(screen.getByText('Cancel'))

      expect(screen.queryByPlaceholderText('Tag name')).not.toBeInTheDocument()
      expect(screen.getByText('+ Create new tag')).toBeInTheDocument()
    })

    it('should call mutation when Create clicked', async () => {
      const newTag = { id: 'tag-4', user_id: 'user-1', name: 'New Tag', color: '#6B7280', usage_count: 0, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }
      mockMutateAsync.mockResolvedValueOnce(newTag)

      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          onTagCreated={mockOnTagCreated}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))
      fireEvent.change(screen.getByPlaceholderText('Tag name'), { target: { value: 'New Tag' } })
      fireEvent.click(screen.getByText('Create'))

      await waitFor(() => {
        expect(mockMutateAsync).toHaveBeenCalledWith({
          data: { name: 'New Tag', color: '#6B7280' },
        })
      })
    })

    it('should add new tag to selection after creation', async () => {
      const newTag = { id: 'tag-4', user_id: 'user-1', name: 'New Tag', color: '#6B7280', usage_count: 0, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }
      mockMutateAsync.mockResolvedValueOnce(newTag)

      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          onTagCreated={mockOnTagCreated}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))
      fireEvent.change(screen.getByPlaceholderText('Tag name'), { target: { value: 'New Tag' } })
      fireEvent.click(screen.getByText('Create'))

      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalledWith(['Essential', 'New Tag'])
      })
    })

    it('should call onTagCreated callback after creation', async () => {
      const newTag = { id: 'tag-4', user_id: 'user-1', name: 'New Tag', color: '#6B7280', usage_count: 0, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }
      mockMutateAsync.mockResolvedValueOnce(newTag)

      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          onTagCreated={mockOnTagCreated}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))
      fireEvent.click(screen.getByText('+ Create new tag'))
      fireEvent.change(screen.getByPlaceholderText('Tag name'), { target: { value: 'New Tag' } })
      fireEvent.click(screen.getByText('Create'))

      await waitFor(() => {
        expect(mockOnTagCreated).toHaveBeenCalledWith(newTag)
      })
    })
  })

  describe('Disabled State', () => {
    it('should disable Add tag button when disabled', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          disabled={true}
        />
      )

      expect(screen.getByText('+ Add tag')).toBeDisabled()
    })

    it('should disable remove buttons when disabled', () => {
      render(
        <TagEditor
          selectedTags={['Essential']}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          disabled={true}
        />
      )

      expect(screen.getByLabelText('Remove Essential')).toBeDisabled()
    })

    it('should not open dropdown when Add tag clicked while disabled', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={createMockTags()}
          onChange={mockOnChange}
          disabled={true}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should handle empty available tags', () => {
      render(
        <TagEditor
          selectedTags={[]}
          availableTags={[]}
          onChange={mockOnChange}
        />
      )

      fireEvent.click(screen.getByText('+ Add tag'))

      // Should still show create option
      expect(screen.getByText('+ Create new tag')).toBeInTheDocument()
    })

    it('should handle tag without color', () => {
      const tagsWithoutColor: UserTag[] = [
        { id: 'tag-1', user_id: 'user-1', name: 'NoColor', color: undefined, usage_count: 0, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
      ]

      render(
        <TagEditor
          selectedTags={['NoColor']}
          availableTags={tagsWithoutColor}
          onChange={mockOnChange}
        />
      )

      const tagChip = screen.getByText('NoColor').closest('.tag-chip') as HTMLElement
      // Should use default gray color (happy-dom returns hex, jsdom returns rgb)
      expect(['rgb(107, 114, 128)', '#6B7280']).toContain(tagChip.style.backgroundColor)
    })
  })
})
