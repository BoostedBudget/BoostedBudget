/**
 * ListViewToggle Component
 * Issue #39: Rule Builder UI
 *
 * Reusable toggle component for switching between Cards and Table view.
 * Persists preference to localStorage when storageKey is provided.
 */

import { useCallback } from 'react'
import { LayoutGrid, List } from 'lucide-react'

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
      className={`inline-flex gap-0 rounded-lg bg-muted p-1 ${className}`.trim()}
    >
      <button
        type="button"
        onClick={() => handleChange('cards')}
        aria-pressed={view === 'cards'}
        disabled={disabled}
        className={[
          'inline-flex items-center gap-1.5 px-3 py-2 border-none rounded-md text-sm font-medium cursor-pointer transition-all duration-150',
          'focus-visible:outline-2 focus-visible:outline-primary focus-visible:outline-offset-2',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          view === 'cards'
            ? 'bg-card text-foreground shadow-sm'
            : 'bg-transparent text-muted-foreground hover:text-foreground',
        ].join(' ')}
        title="Cards view"
      >
        <LayoutGrid className="w-4 h-4 shrink-0" aria-hidden="true" />
        <span className="max-sm:hidden">Cards</span>
      </button>
      <button
        type="button"
        onClick={() => handleChange('table')}
        aria-pressed={view === 'table'}
        disabled={disabled}
        className={[
          'inline-flex items-center gap-1.5 px-3 py-2 max-sm:px-2 border-none rounded-md text-sm font-medium cursor-pointer transition-all duration-150',
          'focus-visible:outline-2 focus-visible:outline-primary focus-visible:outline-offset-2',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          view === 'table'
            ? 'bg-card text-foreground shadow-sm'
            : 'bg-transparent text-muted-foreground hover:text-foreground',
        ].join(' ')}
        title="Table view"
      >
        <List className="w-4 h-4 shrink-0" aria-hidden="true" />
        <span className="max-sm:hidden">Table</span>
      </button>
    </div>
  )
}
