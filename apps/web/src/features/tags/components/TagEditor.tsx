import { useState } from 'react'
import type { UserTag } from '../types'
import { useCreateTag } from '@boostedbudget/api-client/endpoints/tags/tags'
import { ReadOnlyGuard } from '@/auth'

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
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2 min-h-8 items-center">
        {selectedTags.length === 0 ? (
          <span className="text-muted-foreground text-sm italic">No tags</span>
        ) : (
          selectedTags.map((tagName) => {
            const tag = getTagByName(tagName)
            return (
              <span
                key={tagName}
                className="tag-chip inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium text-white"
                style={{ backgroundColor: tag?.color || '#6B7280' }}
              >
                {tagName}
                <button
                  type="button"
                  className="inline-flex items-center justify-center w-4 h-4 p-0 border-none bg-white/30 rounded-full text-white cursor-pointer text-xs leading-none hover:bg-white/50 disabled:cursor-not-allowed disabled:opacity-50"
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

      <div className="relative">
        <ReadOnlyGuard message="Resubscribe to add tags">
          <button
            type="button"
            className="px-3 py-1.5 border border-dashed border-input rounded-md bg-transparent text-muted-foreground text-sm cursor-pointer hover:border-border hover:text-foreground transition-all disabled:cursor-not-allowed disabled:opacity-50"
            onClick={toggleDropdown}
            disabled={disabled}
            aria-label="Add tag"
          >
            + Add tag
          </button>
        </ReadOnlyGuard>

        {isDropdownOpen && (
          <ul className="absolute top-full left-0 z-50 min-w-40 mt-1 py-1 list-none m-0 bg-card border border-border rounded-md shadow-md" role="listbox">
            {availableForSelection.map((tag) => (
              <li
                key={tag.id}
                role="option"
                aria-label={tag.name}
                className="flex items-center gap-2 px-3 py-2 cursor-pointer text-sm hover:bg-muted"
                onClick={() => handleAddTag(tag.name)}
              >
                <span
                  className="w-3 h-3 rounded-full shrink-0"
                  style={{ backgroundColor: tag.color || '#6B7280' }}
                />
                {tag.name}
              </li>
            ))}

            {isCreating ? (
              <li className="px-3 py-2 flex flex-col gap-2">
                <input
                  type="text"
                  placeholder="Tag name"
                  value={newTagName}
                  onChange={(e) => setNewTagName(e.target.value)}
                  className="w-full px-2 py-1 text-sm border border-input rounded bg-background text-foreground"
                />
                <input
                  type="color"
                  value={newTagColor}
                  onChange={(e) => setNewTagColor(e.target.value)}
                  className="w-full h-8 border border-input rounded cursor-pointer"
                  aria-label="Color"
                />
                <div className="flex gap-2">
                  <ReadOnlyGuard message="Resubscribe to create tags">
                    <button
                      type="button"
                      onClick={handleCreateTag}
                      disabled={!newTagName.trim()}
                      className="flex-1 px-2 py-1 text-xs bg-primary text-primary-foreground border-none rounded cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      Create
                    </button>
                  </ReadOnlyGuard>
                  <button
                    type="button"
                    onClick={resetCreateForm}
                    className="flex-1 px-2 py-1 text-xs border border-input rounded bg-card text-foreground cursor-pointer hover:bg-muted"
                  >
                    Cancel
                  </button>
                </div>
              </li>
            ) : (
              <li
                className="flex items-center gap-2 px-3 py-2 cursor-pointer text-sm hover:bg-muted text-muted-foreground"
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
