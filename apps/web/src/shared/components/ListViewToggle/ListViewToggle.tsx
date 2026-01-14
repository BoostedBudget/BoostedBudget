/**
 * ListViewToggle Component
 * Issue #39: Rule Builder UI
 *
 * Reusable toggle component for switching between Cards and Table view.
 * Persists preference to localStorage when storageKey is provided.
 */

import { useCallback } from 'react'
import { LayoutGrid, List } from 'lucide-react'
import './ListViewToggle.css'

export type ListView = 'cards' | 'table'

export interface ListViewToggleProps {
  /** Current view mode */
  view: ListView
  /** Called when view changes */
  onChange: (view: ListView) => void
  /** localStorage key for persisting preference (optional) */
  storageKey?: string
  /** Additional CSS class */
  className?: string
  /** Disable the toggle */
  disabled?: boolean
}

/**
 * A toggle button group for switching between cards and table views.
 *
 * @example
 * ```tsx
 * function RulesPage() {
 *   const [view, setView] = useState<ListView>('cards')
 *
 *   return (
 *     <div>
 *       <ListViewToggle
 *         view={view}
 *         onChange={setView}
 *         storageKey="rules-list-view"
 *       />
 *       {view === 'cards' ? <RuleCards /> : <RuleTable />}
 *     </div>
 *   )
 * }
 * ```
 */
export function ListViewToggle({
  view,
  onChange,
  storageKey,
  className = '',
  disabled = false,
}: ListViewToggleProps) {
  const handleChange = useCallback(
    (newView: ListView) => {
      if (disabled || newView === view) return

      onChange(newView)

      // Persist to localStorage if storageKey provided
      if (storageKey) {
        try {
          localStorage.setItem(storageKey, newView)
        } catch {
          // Ignore localStorage errors (e.g., quota exceeded, private browsing)
        }
      }
    },
    [view, onChange, storageKey, disabled]
  )

  return (
    <div
      role="group"
      aria-label="View toggle"
      className={`list-view-toggle ${className}`.trim()}
    >
      <button
        type="button"
        onClick={() => handleChange('cards')}
        aria-pressed={view === 'cards'}
        disabled={disabled}
        className={`list-view-toggle__button ${
          view === 'cards' ? 'list-view-toggle__button--active' : ''
        }`}
        title="Cards view"
      >
        <LayoutGrid className="list-view-toggle__icon" aria-hidden="true" />
        <span className="list-view-toggle__label">Cards</span>
      </button>
      <button
        type="button"
        onClick={() => handleChange('table')}
        aria-pressed={view === 'table'}
        disabled={disabled}
        className={`list-view-toggle__button ${
          view === 'table' ? 'list-view-toggle__button--active' : ''
        }`}
        title="Table view"
      >
        <List className="list-view-toggle__icon" aria-hidden="true" />
        <span className="list-view-toggle__label">Table</span>
      </button>
    </div>
  )
}
