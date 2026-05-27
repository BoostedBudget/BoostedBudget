/**
 * HierarchicalCategoryTreeSelect Tests
 * Issue #1200: Multi-Level Category Hierarchy
 *
 * TDD: tests written before implementation (RED phase)
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

// Mock useCuratedPfcDetailed with a small, controlled fixture
// NOTE: mock.module() is process-global per CLAUDE.md feedback; this module is
// only used in this component so a local mock.module() call is acceptable here.
mock.module('@/shared/categories/pfcOptions', () => ({
  useCuratedPfcDetailed: () => ({
    data: [
      {
        primary: 'FOOD_AND_DRINK',
        primaryLabel: 'Food & Drink',
        detailed: [
          { value: 'FOOD_AND_DRINK_COFFEE', label: 'Coffee' },
          { value: 'FOOD_AND_DRINK_GROCERIES', label: 'Groceries' },
          { value: 'FOOD_AND_DRINK_RESTAURANT', label: 'Restaurant' },
        ],
      },
      {
        primary: 'TRANSPORTATION',
        primaryLabel: 'Transportation',
        detailed: [
          { value: 'TRANSPORTATION_GAS', label: 'Gas' },
          { value: 'TRANSPORTATION_PARKING', label: 'Parking' },
        ],
      },
    ],
  }),
}))

// Import AFTER mock.module() is set up
import { HierarchicalCategoryTreeSelect } from './HierarchicalCategoryTreeSelect'

describe('HierarchicalCategoryTreeSelect', () => {
  // ─── Rendering ──────────────────────────────────────────────────────────────

  it('renders trigger button', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    expect(screen.getByRole('button', { name: /category filter/i })).toBeInTheDocument()
  })

  it('shows active dot when selectedPrimaries is non-empty', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={['FOOD_AND_DRINK']}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    expect(screen.getByTestId('active-dot')).toBeInTheDocument()
  })

  it('shows active dot when selectedDetaileds is non-empty', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={['FOOD_AND_DRINK_COFFEE']}
        onDetailedsChange={() => {}}
      />
    )
    expect(screen.getByTestId('active-dot')).toBeInTheDocument()
  })

  it('does not show active dot when nothing is selected', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    expect(screen.queryByTestId('active-dot')).not.toBeInTheDocument()
  })

  it('opens popover on trigger click', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    // Before open: primary labels not visible
    expect(screen.queryByText('Food & Drink')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))

    // After open: primary labels visible
    expect(screen.getByText('Food & Drink')).toBeInTheDocument()
    expect(screen.getByText('Transportation')).toBeInTheDocument()
  })

  // ─── Multi-select: primary selection ────────────────────────────────────────

  it('calls onPrimariesChange when unselected primary row clicked', () => {
    const onPrimariesChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={onPrimariesChange}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('primary-checkbox-FOOD_AND_DRINK'))
    expect(onPrimariesChange).toHaveBeenCalledWith(['FOOD_AND_DRINK'])
  })

  it('deselects primary when already-selected primary is clicked', () => {
    const onPrimariesChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={['FOOD_AND_DRINK']}
        onPrimariesChange={onPrimariesChange}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('primary-checkbox-FOOD_AND_DRINK'))
    expect(onPrimariesChange).toHaveBeenCalledWith([])
  })

  it('shows indeterminate state on primary when some children are selected', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={['FOOD_AND_DRINK_COFFEE']}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    const primaryCheckbox = screen.getByTestId('primary-checkbox-FOOD_AND_DRINK')
    expect(primaryCheckbox).toHaveAttribute('data-state', 'indeterminate')
  })

  it('shows checked state on primary when it is in selectedPrimaries', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={['FOOD_AND_DRINK']}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    const primaryCheckbox = screen.getByTestId('primary-checkbox-FOOD_AND_DRINK')
    expect(primaryCheckbox).toHaveAttribute('data-state', 'checked')
  })

  it('does not call onPrimariesChange when detailed row clicked', () => {
    const onPrimariesChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={onPrimariesChange}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    // Expand primary first
    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    fireEvent.click(screen.getByTestId('detailed-checkbox-FOOD_AND_DRINK_COFFEE'))
    expect(onPrimariesChange).not.toHaveBeenCalled()
  })

  // ─── Multi-select: detailed selection ───────────────────────────────────────

  it('calls onDetailedsChange when detailed row clicked', () => {
    const onDetailedsChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={onDetailedsChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    fireEvent.click(screen.getByTestId('detailed-checkbox-FOOD_AND_DRINK_COFFEE'))
    expect(onDetailedsChange).toHaveBeenCalledWith(['FOOD_AND_DRINK_COFFEE'])
  })

  it('removes detailed value when already-selected detailed row clicked', () => {
    const onDetailedsChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={['FOOD_AND_DRINK_COFFEE']}
        onDetailedsChange={onDetailedsChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    fireEvent.click(screen.getByTestId('detailed-checkbox-FOOD_AND_DRINK_COFFEE'))
    expect(onDetailedsChange).toHaveBeenCalledWith([])
  })

  // ─── Expand / collapse ────────────────────────────────────────────────────

  it('expands and shows detailed rows when expand toggle clicked', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    // Detailed rows not shown by default
    expect(screen.queryByText('Coffee')).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    expect(screen.getByText('Coffee')).toBeInTheDocument()
    expect(screen.getByText('Groceries')).toBeInTheDocument()
  })

  it('collapses detailed rows when expand toggle clicked again', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    expect(screen.getByText('Coffee')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    expect(screen.queryByText('Coffee')).not.toBeInTheDocument()
  })

  // ─── Search ──────────────────────────────────────────────────────────────

  it('filters tree rows by search query matching primary label', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    const search = screen.getByPlaceholderText(/search/i)
    fireEvent.change(search, { target: { value: 'Transport' } })

    expect(screen.getByText('Transportation')).toBeInTheDocument()
    expect(screen.queryByText('Food & Drink')).not.toBeInTheDocument()
  })

  it('filters tree to show primary + all children when primary label matches', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    const search = screen.getByPlaceholderText(/search/i)
    fireEvent.change(search, { target: { value: 'Food' } })

    // Primary shown
    expect(screen.getByText('Food & Drink')).toBeInTheDocument()
    // All children auto-expanded when primary matches
    expect(screen.getByText('Coffee')).toBeInTheDocument()
    expect(screen.getByText('Groceries')).toBeInTheDocument()
  })

  it('filters tree to show matching detailed rows and their parent', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    const search = screen.getByPlaceholderText(/search/i)
    fireEvent.change(search, { target: { value: 'Gas' } })

    expect(screen.getByText('Transportation')).toBeInTheDocument()
    expect(screen.getByText('Gas')).toBeInTheDocument()
    // Groceries is not a match
    expect(screen.queryByText('Groceries')).not.toBeInTheDocument()
  })

  // ─── Clear ────────────────────────────────────────────────────────────────

  it('calls onPrimariesChange([]) and onDetailedsChange([]) when Clear clicked', () => {
    const onPrimariesChange = mock(() => {})
    const onDetailedsChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={['FOOD_AND_DRINK']}
        onPrimariesChange={onPrimariesChange}
        selectedDetaileds={['FOOD_AND_DRINK_COFFEE']}
        onDetailedsChange={onDetailedsChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByRole('button', { name: /clear/i }))
    expect(onPrimariesChange).toHaveBeenCalledWith([])
    expect(onDetailedsChange).toHaveBeenCalledWith([])
  })

  // ─── Single-select mode ──────────────────────────────────────────────────

  it('calls onSingleSelectChange with primary only when primary row clicked', () => {
    const onSingleSelectChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        singleSelectValue={null}
        onSingleSelectChange={onSingleSelectChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('primary-row-FOOD_AND_DRINK'))
    expect(onSingleSelectChange).toHaveBeenCalledWith({ primary: 'FOOD_AND_DRINK' })
  })

  it('calls onSingleSelectChange with primary+detailed when detailed row clicked', () => {
    const onSingleSelectChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        singleSelectValue={null}
        onSingleSelectChange={onSingleSelectChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    fireEvent.click(screen.getByTestId('detailed-row-FOOD_AND_DRINK_COFFEE'))
    expect(onSingleSelectChange).toHaveBeenCalledWith({
      primary: 'FOOD_AND_DRINK',
      detailed: 'FOOD_AND_DRINK_COFFEE',
    })
  })

  it('clears selection when same primary clicked again in single-select', () => {
    const onSingleSelectChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        singleSelectValue={{ primary: 'FOOD_AND_DRINK' }}
        onSingleSelectChange={onSingleSelectChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    fireEvent.click(screen.getByTestId('primary-row-FOOD_AND_DRINK'))
    expect(onSingleSelectChange).toHaveBeenCalledWith(null)
  })

  // ─── Disabled ────────────────────────────────────────────────────────────

  it('trigger button is disabled when disabled prop is true', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
        disabled
      />
    )
    expect(screen.getByRole('button', { name: /category filter/i })).toBeDisabled()
  })

  it('does not open when disabled trigger is clicked', () => {
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={() => {}}
        selectedDetaileds={[]}
        onDetailedsChange={() => {}}
        disabled
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
    expect(screen.queryByText('Food & Drink')).not.toBeInTheDocument()
  })

  // ─── Multi-select: primary clears detailed children ──────────────────────

  it('selecting a primary clears its already-selected detailed children', async () => {
    const onPrimariesChange = mock(() => {})
    const onDetailedsChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        selectedPrimaries={[]}
        onPrimariesChange={onPrimariesChange}
        selectedDetaileds={['FOOD_AND_DRINK_GROCERIES', 'FOOD_AND_DRINK_COFFEE']}
        onDetailedsChange={onDetailedsChange}
      />
    )
    // open popover
    await userEvent.click(screen.getByRole('button', { name: /category filter/i }))
    // click primary checkbox for Food & Drink
    await userEvent.click(screen.getByTestId('primary-checkbox-FOOD_AND_DRINK'))
    // should call onDetailedsChange with the children removed
    expect(onDetailedsChange).toHaveBeenCalledWith([])
  })

  // ─── ARIA structure ────────────────────────────────────────────────────────

  describe('ARIA structure', () => {
    it('places role="tree" on the scrollable rows container, not the popover root', () => {
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={() => {}}
          selectedDetaileds={[]}
          onDetailedsChange={() => {}}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /category filter/i }))

      const tree = screen.getByRole('tree')
      expect(tree).toBeInTheDocument()

      // The search input must NOT be inside the tree element
      const searchInput = screen.getByPlaceholderText('Search categories...')
      expect(tree).not.toContainElement(searchInput)
    })

    it('does not set role="tree" on the popover content root', () => {
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={() => {}}
          selectedDetaileds={[]}
          onDetailedsChange={() => {}}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /category filter/i }))

      const tree = screen.getByRole('tree')
      // The popover content root has class "w-64"; the tree element must be a
      // descendant of it, not the root itself.
      const popoverRoot = document.querySelector('.w-64')
      expect(popoverRoot).not.toBeNull()
      expect(popoverRoot).not.toBe(tree)
      expect(popoverRoot).toContainElement(tree)
    })
  })

  // ─── Keyboard navigation ─────────────────────────────────────────────────
  //
  // The component uses plain <button> elements with no custom onKeyDown handlers.
  // Keyboard behaviors available are therefore:
  //   • Enter / Space on trigger button → opens popover (native button + Radix PopoverTrigger)
  //   • Enter / Space on tree-row buttons → fires onClick (native button behavior)
  //   • Enter / Space on expand-toggle buttons → fires onClick (native button behavior)
  //   • Escape → closes popover (Radix Popover built-in)
  //
  // NOT implemented (no custom handlers): ArrowUp/Down focus movement,
  //   ArrowRight/Left expand-collapse. Tests for those are intentionally omitted.

  describe('keyboard navigation', () => {
    it('opens the popover when Enter is pressed on the trigger button', () => {
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={() => {}}
          selectedDetaileds={[]}
          onDetailedsChange={() => {}}
        />
      )
      const trigger = screen.getByRole('button', { name: /category filter/i })
      expect(screen.queryByText('Food & Drink')).not.toBeInTheDocument()

      fireEvent.keyDown(trigger, { key: 'Enter' })
      fireEvent.click(trigger) // Radix opens on click; Enter on a button fires click natively

      expect(screen.getByText('Food & Drink')).toBeInTheDocument()
    })

    it('fires primary row onClick when Enter is pressed on a primary row button', () => {
      const onPrimariesChange = mock(() => {})
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={onPrimariesChange}
          selectedDetaileds={[]}
          onDetailedsChange={() => {}}
        />
      )
      // Open popover first
      fireEvent.click(screen.getByRole('button', { name: /category filter/i }))

      const primaryBtn = screen.getByTestId('primary-checkbox-FOOD_AND_DRINK').closest('button')!
      fireEvent.keyDown(primaryBtn, { key: 'Enter' })
      fireEvent.click(primaryBtn) // simulate the native click-from-Enter

      expect(onPrimariesChange).toHaveBeenCalledWith(['FOOD_AND_DRINK'])
    })

    it('fires expand toggle onClick when Enter is pressed on the expand toggle', () => {
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={() => {}}
          selectedDetaileds={[]}
          onDetailedsChange={() => {}}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /category filter/i }))

      expect(screen.queryByText('Coffee')).not.toBeInTheDocument()

      const expandToggle = screen.getByTestId('expand-toggle-FOOD_AND_DRINK')
      fireEvent.keyDown(expandToggle, { key: 'Enter' })
      fireEvent.click(expandToggle) // simulate native click-from-Enter

      expect(screen.getByText('Coffee')).toBeInTheDocument()
    })

    it('fires detailed row onClick when Enter is pressed on a detailed row button', () => {
      const onDetailedsChange = mock(() => {})
      render(
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={[]}
          onPrimariesChange={() => {}}
          selectedDetaileds={[]}
          onDetailedsChange={onDetailedsChange}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /category filter/i }))
      fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))

      const detailedBtn = screen.getByTestId('detailed-checkbox-FOOD_AND_DRINK_COFFEE')
      fireEvent.keyDown(detailedBtn, { key: 'Enter' })
      fireEvent.click(detailedBtn) // simulate native click-from-Enter

      expect(onDetailedsChange).toHaveBeenCalledWith(['FOOD_AND_DRINK_COFFEE'])
    })
  })

  // ─── Single-select: detailed toggle-off ──────────────────────────────────

  it('clicking an already-selected detailed row calls onSingleSelectChange(null)', async () => {
    const onSingleSelectChange = mock(() => {})
    render(
      <HierarchicalCategoryTreeSelect
        singleSelectValue={{ primary: 'FOOD_AND_DRINK', detailed: 'FOOD_AND_DRINK_COFFEE' }}
        onSingleSelectChange={onSingleSelectChange}
      />
    )
    await userEvent.click(screen.getByRole('button', { name: /category filter/i }))
    // expand Food & Drink
    await userEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))
    // click the already-selected detailed row
    await userEvent.click(screen.getByTestId('detailed-row-FOOD_AND_DRINK_COFFEE'))
    expect(onSingleSelectChange).toHaveBeenCalledWith(null)
  })
})
