import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { SankeyViewModeControl, resolveViewModeSelection } from './SankeyViewModeControl'

// Issue #1626: this file runs process-isolated (apps/web/scripts/test-conflict-files.sh Group G)
// against the REAL @radix-ui/react-toggle-group, not the centrally-mocked stub (Issue #1323's
// bunfig.toml preload). The stub does not forward an item's aria-label, so running this file
// under the shared, bunfig-preloaded process could never prove SankeyViewModeControl actually
// supplies one — only that the wrapper forwards what it's given. Do NOT widen the shared mock to
// "fix" that: it was tried on this branch, broke ~450 test files / ~2458 tests, and was reverted.

beforeEach(() => {
  mockTrackEvent.mockClear()
})

describe('resolveViewModeSelection', () => {
  it('accepts the two real modes', () => {
    expect(resolveViewModeSelection('parents')).toBe('parents')
    expect(resolveViewModeSelection('expanded')).toBe('expanded')
  })

  it('rejects the empty string Radix emits when deselecting', () => {
    // ToggleGroup type="single" fires onValueChange('') when the user clicks the item that is
    // already selected. Passing that through would leave the chart with no mode at all.
    expect(resolveViewModeSelection('')).toBeNull()
    expect(resolveViewModeSelection('nonsense')).toBeNull()
  })
})

describe('SankeyViewModeControl', () => {
  it('reports a mode change and tracks it', () => {
    const onChange = mock(() => {})
    render(<SankeyViewModeControl value="parents" onChange={onChange} />)

    fireEvent.click(screen.getByText('Subcategories'))

    expect(onChange).toHaveBeenCalledWith('expanded')
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.REPORT_SANKEY_VIEW_MODE_CHANGED,
      { mode: 'expanded' },
    )
  })

  it('never puts a category name, id or amount in an analytics property', () => {
    const onChange = mock(() => {})
    render(<SankeyViewModeControl value="parents" onChange={onChange} />)
    fireEvent.click(screen.getByText('Subcategories'))

    // Event properties are stored verbatim and are not covered by data-ph-mask.
    const call = mockTrackEvent.mock.calls[0] as unknown as [string, Record<string, unknown>]
    expect(Object.keys(call[1])).toEqual(['mode'])
  })

  it('offers a way back when a category is focused', () => {
    const onClearFocus = mock(() => {})
    render(
      <SankeyViewModeControl
        value="parents"
        onChange={mock(() => {})}
        focusedKey="FOOD_AND_DRINK"
        onClearFocus={onClearFocus}
      />,
    )

    // While focused the mode toggle is replaced: switching consolidation level mid-focus would be
    // ambiguous, and the user's one obvious need is to get back out.
    expect(screen.queryByText('Subcategories')).toBeNull()
    fireEvent.click(screen.getByText(/Back to all categories/))
    expect(onClearFocus).toHaveBeenCalled()
  })

  it('gives every toggle item a non-empty, structural accessible name', () => {
    // Real Radix (this file runs isolated, see header comment) renders type="single" items with
    // role="radio". Asserted via toHaveAccessibleName(), not a text query, because the whole
    // point is the ATTRIBUTE: session replay records aria-label verbatim and data-ph-mask does
    // NOT cover attributes (see CashFlowSankeyChart.test.tsx's
    // "never interpolates a category name into an attribute").
    render(<SankeyViewModeControl value="parents" onChange={mock(() => {})} />)

    const items = screen.getAllByRole('radio')
    expect(items).toHaveLength(2)
    for (const item of items) {
      const name = item.getAttribute('aria-label')
      expect(name).toBeTruthy()
      expect(item).toHaveAccessibleName()
      // Structural, not a restatement of the internal 'parents' | 'expanded' mode key, and never
      // a category name/id/amount (there is none in scope here to leak).
      expect(name).not.toBe('parents')
      expect(name).not.toBe('expanded')
    }
  })
})
