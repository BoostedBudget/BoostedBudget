import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { SankeyViewModeControl, resolveViewModeSelection } from './SankeyViewModeControl'

// Issue #1323: toggle-group is centrally mocked via bunfig.toml preload (toggle-group.tsx)

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
        focusedLabel="FOOD_AND_DRINK"
        onClearFocus={onClearFocus}
      />,
    )

    // While focused the mode toggle is replaced: switching consolidation level mid-focus would be
    // ambiguous, and the user's one obvious need is to get back out.
    // Queried by TEXT, not label: the shared toggle-group mock does not forward an item's
    // aria-label, and widening that mock breaks accessible-name queries across ~450 other files.
    expect(screen.queryByText('Subcategories')).toBeNull()
    fireEvent.click(screen.getByText(/Back to all categories/))
    expect(onClearFocus).toHaveBeenCalled()
  })
})
