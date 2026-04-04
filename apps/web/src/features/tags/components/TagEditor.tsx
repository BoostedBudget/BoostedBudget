import { useState } from 'react'
import type { UserTag } from '../types'
import { useCreateTag } from '@boostedbudget/api-client/endpoints/tags/tags'
import { ReadOnlyGuard } from '@/auth'
import './TagEditor.css'

interface TagEditorProps {
  selectedTags: string[]
  availableTags: UserTag[]
  onChange: (tags: string[]) => void
  onTagCreated?: (tag: UserTag) => void
  disabled?: boolean
}

export function TagEditor({
  selectedTags,
  availableTags,
  onChange,
  onTagCreated,
  disabled = false,
}: TagEditorProps) {
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)
  const [isCreating, setIsCreating] = useState(false)
  const [newTagName, setNewTagName] = useState('')
  const [newTagColor, setNewTagColor] = useState('#6B7280')

  // Use generated mutation hook
  const createTagMutation = useCreateTag()

  // Get tag object by name
  const getTagByName = (name: string): UserTag | undefined => {
    return availableTags.find((tag) => tag.name === name)
  }

  // Filter out already selected tags from dropdown
  const availableForSelection = availableTags.filter(
    (tag) => !selectedTags.includes(tag.name)
  )

  const handleRemoveTag = (tagName: string) => {
    const newTags = selectedTags.filter((t) => t !== tagName)
    onChange(newTags)
  }

  const handleAddTag = (tagName: string) => {
    onChange([...selectedTags, tagName])
    setIsDropdownOpen(false)
  }

  const toggleDropdown = () => {
    if (!disabled) {
      setIsDropdownOpen(!isDropdownOpen)
    }
  }

  const resetCreateForm = () => {
    setIsCreating(false)
    setNewTagName('')
    setNewTagColor('#6B7280')
  }

  const handleCreateTag = async () => {
    if (!newTagName.trim()) return

    try {
      const newTag = await createTagMutation.mutateAsync({
        data: {
          name: newTagName.trim(),
          color: newTagColor,
        },
      })

      onTagCreated?.(newTag as UserTag)
      onChange([...selectedTags, newTag.name])
      resetCreateForm()
      setIsDropdownOpen(false)
    } catch (error) {
      console.error('Failed to create tag:', error)
    }
  }

  return (
    <div className="tag-editor">
      <div className="tag-editor__chips">
        {selectedTags.length === 0 ? (
          <span className="tag-editor__empty">No tags</span>
        ) : (
          selectedTags.map((tagName) => {
            const tag = getTagByName(tagName)
            return (
              <span
                key={tagName}
                className="tag-chip"
                style={{ backgroundColor: tag?.color || '#6B7280' }}
              >
                {tagName}
                <button
                  type="button"
                  className="tag-chip__remove"
                  onClick={() => handleRemoveTag(tagName)}
                  disabled={disabled}
                  aria-label={`Remove ${tagName}`}
                >
                  ×
                </button>
              </span>
            )
          })
        )}
      </div>

      <div className="tag-editor__add-container">
        <ReadOnlyGuard message="Resubscribe to add tags">
          <button
            type="button"
            className="tag-editor__add-button"
            onClick={toggleDropdown}
            disabled={disabled}
            aria-label="Add tag"
          >
            + Add tag
          </button>
        </ReadOnlyGuard>

        {isDropdownOpen && (
          <ul className="tag-editor__dropdown" role="listbox">
            {availableForSelection.map((tag) => (
              <li
                key={tag.id}
                role="option"
                aria-label={tag.name}
                className="tag-editor__option"
                onClick={() => handleAddTag(tag.name)}
              >
                <span
                  className="tag-editor__option-color"
                  style={{ backgroundColor: tag.color || '#6B7280' }}
                />
                {tag.name}
              </li>
            ))}

            {isCreating ? (
              <li className="tag-editor__create-form">
                <input
                  type="text"
                  placeholder="Tag name"
                  value={newTagName}
                  onChange={(e) => setNewTagName(e.target.value)}
                  className="tag-editor__create-input"
                />
                <input
                  type="color"
                  value={newTagColor}
                  onChange={(e) => setNewTagColor(e.target.value)}
                  className="tag-editor__create-color"
                  aria-label="Color"
                />
                <div className="tag-editor__create-actions">
                  <ReadOnlyGuard message="Resubscribe to create tags">
                    <button
                      type="button"
                      onClick={handleCreateTag}
                      disabled={!newTagName.trim()}
                      className="tag-editor__create-button"
                    >
                      Create
                    </button>
                  </ReadOnlyGuard>
                  <button
                    type="button"
                    onClick={resetCreateForm}
                    className="tag-editor__cancel-button"
                  >
                    Cancel
                  </button>
                </div>
              </li>
            ) : (
              <li
                className="tag-editor__option tag-editor__create-option"
                onClick={() => setIsCreating(true)}
              >
                + Create new tag
              </li>
            )}
          </ul>
        )}
      </div>
    </div>
  )
}
