import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

export type SankeyViewModeSelection = 'parents' | 'expanded'

/**
 * Resolves a raw ToggleGroup value into a valid selection, or null if invalid.
 *
 * Radix's ToggleGroup (type="single") fires onValueChange with an EMPTY STRING when the user clicks
 * the already-selected item, which is not a valid mode and must be filtered out rather than passed
 * through — the same guard ScopeSegmentedControl documents.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function resolveViewModeSelection(value: string): SankeyViewModeSelection | null {
  return value === 'parents' || value === 'expanded' ? value : null
}

interface SankeyViewModeControlProps {
  value: SankeyViewModeSelection
  onChange: (value: SankeyViewModeSelection) => void
  /**
   * The focused category's KEY, used purely as a truthiness flag to swap the toggle for a way out.
   * Named `focusedKey`, not `focusedLabel`, because it is NOT display text — a custom category's
   * key is `custom:<uuid>`, so rendering it would put a UUID on screen: the very bug #1626 fixes.
   */
  focusedKey?: string
  onClearFocus?: () => void
}

export function SankeyViewModeControl({ value, onChange, focusedKey, onClearFocus }: SankeyViewModeControlProps) {
  const trackEvent = useTrackEvent()

  const handleValueChange = (raw: string) => {
    const resolved = resolveViewModeSelection(raw)
    if (!resolved) return
    onChange(resolved)
    // Deliberately carries the MODE only — never a category name, id or amount.
    trackEvent(ANALYTICS_EVENTS.REPORT_SANKEY_VIEW_MODE_CHANGED, { mode: resolved })
  }

  return (
    <div className="flex items-center gap-2" data-testid="sankey-view-mode-control">
      {focusedKey ? (
        <button
          type="button"
          onClick={onClearFocus}
          className="rounded-md px-2 py-1 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          ← Back to all categories
        </button>
      ) : (
        <ToggleGroup type="single" value={value} onValueChange={handleValueChange}>
          <ToggleGroupItem value="parents" aria-label="Show top-level categories">
            Categories
          </ToggleGroupItem>
          <ToggleGroupItem value="expanded" aria-label="Show subcategories">
            Subcategories
          </ToggleGroupItem>
        </ToggleGroup>
      )}
    </div>
  )
}
