import { useState } from 'react'
import type { UserTag } from '../types'
import './TagEditor.css'

interface TagEditorProps {
  selectedTags: string[]
  availableTags: UserTag[]
  onChange: (tags: string[]) => void
  disabled?: boolean
}

export function TagEditor({
  selectedTags,
  availableTags,
  onChange,
  disabled = false,
}: TagEditorProps) {
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)

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
        <button
          type="button"
          className="tag-editor__add-button"
          onClick={toggleDropdown}
          disabled={disabled || availableForSelection.length === 0}
          aria-label="Add tag"
        >
          + Add tag
        </button>

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
          </ul>
        )}
      </div>
    </div>
  )
}
