/**
 * RuleEditor Component
 *
 * Form for creating and editing categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState, useCallback } from 'react'
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  RuleCondition,
  ConditionType,
  ConditionLogic,
  TagBehavior,
} from '../types'
import { CONDITION_TYPE_LABELS } from '../types'
import './RuleEditor.css'

export interface RuleEditorProps {
  mode: 'create' | 'edit'
  rule?: CategorizationRule
  onSave: (data: CreateRuleRequest | UpdateRuleRequest) => Promise<void>
  onCancel: () => void
  categories: string[][]
}

interface FormErrors {
  name?: string
  conditions?: string
  target?: string
}

const DEFAULT_CONDITION: RuleCondition = {
  type: 'merchant_contains',
  value: '',
}

export function RuleEditor({ mode, rule, onSave, onCancel, categories }: RuleEditorProps) {
  // Form state
  const [name, setName] = useState(rule?.name || '')
  const [description, setDescription] = useState(rule?.description || '')
  const [priority, setPriority] = useState(rule?.priority || 10)
  const [conditions, setConditions] = useState<RuleCondition[]>(
    rule?.conditions || []
  )
  const [conditionLogic, setConditionLogic] = useState<ConditionLogic>(
    rule?.condition_logic || 'AND'
  )
  const [targetCategory, setTargetCategory] = useState<string[] | null>(
    rule?.target_category || null
  )
  const [targetTags, setTargetTags] = useState<string[]>(rule?.target_tags || [])
  const [tagBehavior, setTagBehavior] = useState<TagBehavior>(rule?.tag_behavior || 'add')
  const [newTag, setNewTag] = useState('')

  // UI state
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<FormErrors>({})

  // Validation
  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {}

    if (!name.trim()) {
      newErrors.name = 'Rule name is required'
    }

    if (conditions.length === 0) {
      newErrors.conditions = 'At least one condition is required'
    }

    if (!targetCategory && targetTags.length === 0) {
      newErrors.target = 'Target category or tags required'
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }, [name, conditions, targetCategory, targetTags])

  // Handlers
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!validate()) {
      return
    }

    setSaving(true)
    try {
      const data: CreateRuleRequest | UpdateRuleRequest = {
        name: name.trim(),
        description: description.trim() || undefined,
        priority,
        conditions,
        condition_logic: conditionLogic,
        target_category: targetCategory || undefined,
        target_tags: targetTags.length > 0 ? targetTags : undefined,
        tag_behavior: tagBehavior,
      }
      await onSave(data)
    } finally {
      setSaving(false)
    }
  }

  const addCondition = () => {
    setConditions([...conditions, { ...DEFAULT_CONDITION }])
  }

  const removeCondition = (index: number) => {
    setConditions(conditions.filter((_, i) => i !== index))
  }

  const updateCondition = (index: number, updates: Partial<RuleCondition>) => {
    setConditions(conditions.map((c, i) => (i === index ? { ...c, ...updates } : c)))
  }

  const handleCategoryChange = (value: string) => {
    if (value === '') {
      setTargetCategory(null)
    } else {
      setTargetCategory(value.split(' > '))
    }
  }

  const addTag = () => {
    const tag = newTag.trim()
    if (tag && !targetTags.includes(tag)) {
      setTargetTags([...targetTags, tag])
      setNewTag('')
    }
  }

  const removeTag = (tag: string) => {
    setTargetTags(targetTags.filter((t) => t !== tag))
  }

  const categoryValue = targetCategory ? targetCategory.join(' > ') : ''

  return (
    <form className="rule-editor" onSubmit={handleSubmit}>
      <h2 className="rule-editor__title">
        {mode === 'create' ? 'Create Rule' : 'Edit Rule'}
      </h2>

      {/* Basic Info */}
      <div className="rule-editor__section">
        <div className="rule-editor__field">
          <label htmlFor="rule-name" className="rule-editor__label">
            Rule Name
          </label>
          <input
            id="rule-name"
            type="text"
            className="rule-editor__input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={saving}
            placeholder="e.g., Coffee Shops"
          />
          {errors.name && <span className="rule-editor__error">{errors.name}</span>}
        </div>

        <div className="rule-editor__field">
          <label htmlFor="rule-description" className="rule-editor__label">
            Description
          </label>
          <input
            id="rule-description"
            type="text"
            className="rule-editor__input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={saving}
            placeholder="Optional description"
          />
        </div>

        <div className="rule-editor__field rule-editor__field--small">
          <label htmlFor="rule-priority" className="rule-editor__label">
            Priority
          </label>
          <input
            id="rule-priority"
            type="number"
            className="rule-editor__input"
            value={priority}
            onChange={(e) => setPriority(parseInt(e.target.value, 10) || 0)}
            disabled={saving}
            min={1}
            max={100}
          />
        </div>
      </div>

      {/* Conditions */}
      <div className="rule-editor__section">
        <div className="rule-editor__section-header">
          <h3 className="rule-editor__section-title">Conditions</h3>
          {conditions.length > 1 && (
            <div className="rule-editor__logic-toggle">
              <button
                type="button"
                className={`rule-editor__logic-btn ${conditionLogic === 'AND' ? 'active' : ''}`}
                onClick={() => setConditionLogic('AND')}
                disabled={saving}
              >
                AND
              </button>
              <button
                type="button"
                className={`rule-editor__logic-btn ${conditionLogic === 'OR' ? 'active' : ''}`}
                onClick={() => setConditionLogic('OR')}
                disabled={saving}
              >
                OR
              </button>
            </div>
          )}
        </div>

        {errors.conditions && (
          <span className="rule-editor__error">{errors.conditions}</span>
        )}

        <div className="rule-editor__conditions">
          {conditions.map((condition, index) => (
            <div key={index} className="rule-editor__condition">
              <label htmlFor={`condition-type-${index}`} className="visually-hidden">
                Condition type
              </label>
              <select
                id={`condition-type-${index}`}
                aria-label="Condition type"
                className="rule-editor__select"
                value={condition.type}
                onChange={(e) => updateCondition(index, { type: e.target.value as ConditionType })}
                disabled={saving}
              >
                {Object.entries(CONDITION_TYPE_LABELS).map(([type, label]) => (
                  <option key={type} value={type}>
                    {label}
                  </option>
                ))}
              </select>
              <input
                type="text"
                className="rule-editor__input"
                value={String(condition.value)}
                onChange={(e) => updateCondition(index, { value: e.target.value })}
                disabled={saving}
                placeholder="Enter value"
              />
              <button
                type="button"
                className="rule-editor__remove-btn"
                onClick={() => removeCondition(index)}
                disabled={saving}
                aria-label="Remove condition"
              >
                &times;
              </button>
            </div>
          ))}
        </div>

        <button
          type="button"
          className="rule-editor__add-btn"
          onClick={addCondition}
          disabled={saving}
        >
          Add Condition
        </button>
      </div>

      {/* Target Category */}
      <div className="rule-editor__section">
        <h3 className="rule-editor__section-title">Actions</h3>
        {errors.target && <span className="rule-editor__error">{errors.target}</span>}

        <div className="rule-editor__field">
          <label htmlFor="target-category" className="rule-editor__label">
            Target Category
          </label>
          <select
            id="target-category"
            className="rule-editor__select"
            value={categoryValue}
            onChange={(e) => handleCategoryChange(e.target.value)}
            disabled={saving}
          >
            <option value="">No category change</option>
            {categories.map((cat) => {
              const value = cat.join(' > ')
              return (
                <option key={value} value={value}>
                  {value}
                </option>
              )
            })}
          </select>
        </div>

        {/* Tags */}
        <div className="rule-editor__field">
          <label className="rule-editor__label">Target Tags</label>
          <div className="rule-editor__tags">
            {targetTags.map((tag) => (
              <span key={tag} className="rule-editor__tag">
                {tag}
                <button
                  type="button"
                  className="rule-editor__tag-remove"
                  onClick={() => removeTag(tag)}
                  disabled={saving}
                  aria-label={`Remove tag ${tag}`}
                >
                  &times;
                </button>
              </span>
            ))}
          </div>
          <div className="rule-editor__tag-input-row">
            <input
              type="text"
              className="rule-editor__input"
              value={newTag}
              onChange={(e) => setNewTag(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addTag()
                }
              }}
              disabled={saving}
              placeholder="Add tag"
            />
            <button
              type="button"
              className="rule-editor__add-btn"
              onClick={addTag}
              disabled={saving || !newTag.trim()}
            >
              Add Tag
            </button>
          </div>
        </div>

        {/* Tag behavior */}
        {targetTags.length > 0 && (
          <div className="rule-editor__field">
            <label className="rule-editor__label">Tag Behavior</label>
            <div className="rule-editor__radio-group">
              <label className="rule-editor__radio">
                <input
                  type="radio"
                  name="tag-behavior"
                  value="add"
                  checked={tagBehavior === 'add'}
                  onChange={() => setTagBehavior('add')}
                  disabled={saving}
                />
                Add to existing tags
              </label>
              <label className="rule-editor__radio">
                <input
                  type="radio"
                  name="tag-behavior"
                  value="replace"
                  checked={tagBehavior === 'replace'}
                  onChange={() => setTagBehavior('replace')}
                  disabled={saving}
                />
                Replace existing tags
              </label>
            </div>
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="rule-editor__footer">
        <button
          type="button"
          className="rule-editor__btn rule-editor__btn--cancel"
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </button>
        <button
          type="submit"
          className="rule-editor__btn rule-editor__btn--save"
          disabled={saving}
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>
    </form>
  )
}
