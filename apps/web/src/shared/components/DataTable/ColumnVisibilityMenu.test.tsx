import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ColumnVisibilityMenu } from './ColumnVisibilityMenu'
import type { ColumnDef } from './types'

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mock(() => {}),
}))

type Row = { id: string; name: string }

const hideableColumns: ColumnDef<Row>[] = [
  { id: 'name', header: 'Name', key: 'name', hideable: true },
  { id: 'amount', header: 'Amount', key: 'amount', hideable: true },
]

const noVisibility = {}
const noChange = mock((_: Record<string, boolean>) => {})

function openMenu() {
  fireEvent.click(screen.getByRole('button', { name: /columns/i }))
}

describe('ColumnVisibilityMenu', () => {
  beforeEach(() => noChange.mockClear())

  describe('"Reset column widths" item', () => {
    it('is disabled when columnSizing is empty', () => {
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={noVisibility}
          onChange={noChange}
          columnSizing={{}}
          onResetSizing={mock(() => {})}
        />
      )
      openMenu()
      const resetBtn = screen.getByRole('menuitem', { name: /reset column widths/i })
      expect(resetBtn).toBeDefined()
      expect(resetBtn.getAttribute('disabled')).not.toBeNull()
    })

    it('is enabled when columnSizing has overrides', () => {
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={noVisibility}
          onChange={noChange}
          columnSizing={{ name: 280 }}
          onResetSizing={mock(() => {})}
        />
      )
      openMenu()
      const resetBtn = screen.getByRole('menuitem', { name: /reset column widths/i })
      expect(resetBtn.getAttribute('disabled')).toBeNull()
    })

    it('calls onResetSizing and closes menu on click when enabled', () => {
      const onResetSizing = mock(() => {})
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={noVisibility}
          onChange={noChange}
          columnSizing={{ name: 280 }}
          onResetSizing={onResetSizing}
        />
      )
      openMenu()
      fireEvent.click(screen.getByRole('menuitem', { name: /reset column widths/i }))
      expect(onResetSizing.mock.calls.length).toBe(1)
      // Menu should close after reset
      expect(screen.queryByRole('menu')).toBeNull()
    })

    it('does NOT call onResetSizing on click when disabled (no overrides)', () => {
      const onResetSizing = mock(() => {})
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={noVisibility}
          onChange={noChange}
          columnSizing={{}}
          onResetSizing={onResetSizing}
        />
      )
      openMenu()
      fireEvent.click(screen.getByRole('menuitem', { name: /reset column widths/i }))
      expect(onResetSizing.mock.calls.length).toBe(0)
    })
  })

  describe('relaxed gating (C5 fix)', () => {
    it('renders menu with only the reset item when no hideable columns but onResetSizing provided', () => {
      const nonHideableColumns: ColumnDef<Row>[] = [
        { id: 'name', header: 'Name', key: 'name' }, // no hideable:true
      ]
      render(
        <ColumnVisibilityMenu
          columns={nonHideableColumns}
          visibility={noVisibility}
          onChange={noChange}
          columnSizing={{ name: 280 }}
          onResetSizing={mock(() => {})}
        />
      )
      openMenu()
      // No visibility checkboxes
      expect(screen.queryByRole('menuitemcheckbox')).toBeNull()
      // But reset item is present
      const resetBtn = screen.queryByRole('menuitem', { name: /reset column widths/i })
      expect(resetBtn).not.toBeNull()
    })
  })

  describe('visibility toggle items', () => {
    it('renders a checkbox item per hideable column', () => {
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={noVisibility}
          onChange={noChange}
        />
      )
      openMenu()
      const checkboxes = screen.getAllByRole('menuitemcheckbox')
      expect(checkboxes.length).toBe(2)
    })

    it('checked state reflects visibility prop', () => {
      render(
        <ColumnVisibilityMenu
          columns={hideableColumns}
          visibility={{ name: false }}
          onChange={noChange}
        />
      )
      openMenu()
      const [nameItem, amountItem] = screen.getAllByRole('menuitemcheckbox')
      expect(nameItem.getAttribute('aria-checked')).toBe('false')
      expect(amountItem.getAttribute('aria-checked')).toBe('true')
    })
  })
})
