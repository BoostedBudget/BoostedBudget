import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DataTable } from './DataTable'
import type { ColumnDef, SortState, FilterState } from './types'

type Row = { id: string; name: string; amount: number }

const rows: Row[] = [
  { id: '1', name: 'Alice', amount: 100 },
  { id: '2', name: 'Bob', amount: 200 },
]

const baseColumns: ColumnDef<Row>[] = [
  { id: 'name', header: 'Name', sortable: true, key: 'name' },
  { id: 'amount', header: 'Amount', sortable: true, numeric: true, key: 'amount' },
]

function noopSort() {
  return mock((_: SortState) => {})
}
function noopFilters() {
  return mock((_: FilterState) => {})
}

function renderTable(overrides: Partial<{
  data: Row[]
  columns: ColumnDef<Row>[]
  sort: SortState
  onSortChange: ReturnType<typeof mock>
  filters: FilterState
  onFiltersChange: ReturnType<typeof mock>
  [key: string]: unknown
}> = {}) {
  return render(
    <DataTable<Row>
      data={rows}
      columns={baseColumns}
      rowKey={(r) => r.id}
      sort={null}
      onSortChange={noopSort()}
      filters={{}}
      onFiltersChange={noopFilters()}
      {...overrides}
    />
  )
}

// ── Critical Regression Test 1 ───────────────────────────────────────────────
describe('sort — asc → desc → null lifecycle', () => {
  it('emits asc on first click, desc on second, null on third; sets aria-sort on th', () => {
    const onSortChange = noopSort()

    const { rerender, container } = render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={onSortChange}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )

    const nameBtn = screen.getAllByRole('button').find((b) =>
      b.textContent?.includes('Name')
    )!

    // First click → asc
    fireEvent.click(nameBtn)
    expect(onSortChange.mock.calls[0][0]).toEqual({ columnId: 'name', direction: 'asc' })

    // Rerender with asc sort; aria-sort should be ascending on the Name th
    rerender(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={{ columnId: 'name', direction: 'asc' }}
        onSortChange={onSortChange}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    expect(container.querySelector('th[aria-sort="ascending"]')).not.toBeNull()

    // Second click → desc
    fireEvent.click(nameBtn)
    expect(onSortChange.mock.calls[1][0]).toEqual({ columnId: 'name', direction: 'desc' })

    // Rerender with desc sort
    rerender(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={{ columnId: 'name', direction: 'desc' }}
        onSortChange={onSortChange}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    expect(container.querySelector('th[aria-sort="descending"]')).not.toBeNull()

    // Third click → null (clear)
    fireEvent.click(nameBtn)
    expect(onSortChange.mock.calls[2][0]).toBeNull()
  })
})

// ── Critical Regression Test 2 ───────────────────────────────────────────────
describe('multi-select filter', () => {
  it('emits string[] payload keyed by column id via onFiltersChange', async () => {
    const onFiltersChange = noopFilters()

    const columns: ColumnDef<Row>[] = [
      {
        id: 'name',
        header: 'Name',
        filter: {
          type: 'select',
          options: [
            { label: 'Alice', value: 'alice' },
            { label: 'Bob', value: 'bob' },
          ],
          multiSelect: true,
        },
      },
    ]

    render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={onFiltersChange}
      />
    )

    // Open filter dropdown
    await userEvent.click(screen.getByRole('button', { name: /filter by name/i }))
    // Click Alice option
    await userEvent.click(screen.getByText('Alice'))

    expect(onFiltersChange).toHaveBeenCalledWith({ name: ['alice'] })
  })

  it('merges into existing filter state', async () => {
    const onFiltersChange = noopFilters()

    const columns: ColumnDef<Row>[] = [
      {
        id: 'name',
        header: 'Name',
        filter: {
          type: 'select',
          options: [{ label: 'Alice', value: 'alice' }],
          multiSelect: true,
        },
      },
    ]

    render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{ amount: ['100'] }}
        onFiltersChange={onFiltersChange}
      />
    )

    await userEvent.click(screen.getByRole('button', { name: /filter by name/i }))
    await userEvent.click(screen.getByText('Alice'))

    expect(onFiltersChange).toHaveBeenCalledWith({ amount: ['100'], name: ['alice'] })
  })
})

// ── Critical Regression Test 3 ───────────────────────────────────────────────
describe('range filter', () => {
  it('emits { min, max } payload keyed by column id via onFiltersChange', async () => {
    const onFiltersChange = noopFilters()

    const columns: ColumnDef<Row>[] = [
      {
        id: 'amount',
        header: 'Amount',
        filter: { type: 'range' },
      },
    ]

    render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={onFiltersChange}
      />
    )

    await userEvent.click(screen.getByRole('button', { name: /filter by amount/i }))
    await userEvent.type(screen.getByLabelText(/min/i), '100')
    await userEvent.type(screen.getByLabelText(/max/i), '500')
    await userEvent.click(screen.getByRole('button', { name: /apply/i }))

    expect(onFiltersChange).toHaveBeenCalledWith({ amount: { min: 100, max: 500 } })
  })
})

// ── Critical Regression Test 4 ───────────────────────────────────────────────
describe('maskedColumnIds', () => {
  it('applies data-ph-mask only to specified column tds, not others', () => {
    const { container } = renderTable({ maskedColumnIds: ['amount'] })

    const maskedCells = container.querySelectorAll('[data-ph-mask]')
    // One masked cell per row
    expect(maskedCells.length).toBe(rows.length)
    // Masked cells contain amount values, not names
    for (const cell of maskedCells) {
      expect(cell.textContent).not.toBe('Alice')
      expect(cell.textContent).not.toBe('Bob')
    }

    // Name column tds are NOT masked
    const allTds = container.querySelectorAll('td')
    const nameCells = Array.from(allTds).filter((td) =>
      td.textContent === 'Alice' || td.textContent === 'Bob'
    )
    for (const cell of nameCells) {
      expect(cell.hasAttribute('data-ph-mask')).toBe(false)
    }
  })

  it('applies no masks when maskedColumnIds is empty', () => {
    const { container } = renderTable({ maskedColumnIds: [] })
    expect(container.querySelectorAll('[data-ph-mask]').length).toBe(0)
  })
})

// ── Critical Regression Test 5 ───────────────────────────────────────────────
describe('empty state', () => {
  it('renders emptyState inside tbody with colSpan = columns + rowActions column', () => {
    render(
      <DataTable<Row>
        data={[]}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        emptyState={<p>Nothing here</p>}
        rowActions={() => <button type="button">Delete</button>}
      />
    )

    expect(screen.getByText('Nothing here')).toBeInTheDocument()
    const td = screen.getByText('Nothing here').closest('td')
    expect(td).not.toBeNull()
    // columns.length (2) + rowActions (1) = 3
    expect(td?.getAttribute('colspan')).toBe(String(baseColumns.length + 1))

    // Must be inside tbody, not thead
    expect(td?.closest('tbody')).not.toBeNull()
  })

  it('uses columns.length colSpan when no rowActions', () => {
    const { container } = render(
      <DataTable<Row>
        data={[]}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        emptyState={<p>Empty</p>}
      />
    )
    const td = container.querySelector('tbody td')
    expect(td?.getAttribute('colspan')).toBe(String(baseColumns.length))
  })
})

// ── Critical Regression Test 6 ───────────────────────────────────────────────
describe('rowActions', () => {
  it('td stops propagation so onRowClick does not fire when action is clicked', () => {
    const onRowClick = mock((_: Row) => {})

    render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onRowClick={onRowClick}
        rowActions={() => <button type="button">Delete</button>}
      />
    )

    const deleteBtns = screen.getAllByRole('button', { name: 'Delete' })
    fireEvent.click(deleteBtns[0])

    expect(onRowClick).not.toHaveBeenCalled()
  })

  it('renders actions column header labeled Actions', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        rowActions={() => <button type="button">Edit</button>}
      />
    )

    const ths = screen.getAllByRole('columnheader')
    const actionsTh = ths.find((th) => th.textContent?.includes('Actions'))
    expect(actionsTh).toBeDefined()
  })
})

// ── Critical Regression Test 7 ───────────────────────────────────────────────
describe('custom ReactNode header', () => {
  it('renders ReactNode header content directly in th', () => {
    const columns: ColumnDef<Row>[] = [
      {
        id: 'select',
        header: <input type="checkbox" aria-label="Select all" />,
      },
      { id: 'name', header: 'Name', key: 'name' },
    ]

    render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )

    expect(screen.getByRole('checkbox', { name: /select all/i })).toBeInTheDocument()
  })
})

// ── Additional coverage ───────────────────────────────────────────────────────
describe('caption', () => {
  it('renders caption element when caption prop is provided', () => {
    const { container } = renderTable({ caption: 'My Table' })
    const caption = container.querySelector('caption')
    expect(caption).not.toBeNull()
    expect(caption?.textContent).toBe('My Table')
  })

  it('does not render caption element when caption prop is absent', () => {
    const { container } = renderTable()
    expect(container.querySelector('caption')).toBeNull()
  })
})

describe('loading', () => {
  it('sets aria-busy="true" and applies opacity/pointer-events classes', () => {
    const { container } = renderTable({ loading: true })
    const table = container.querySelector('table')
    expect(table?.getAttribute('aria-busy')).toBe('true')
    expect(table?.className).toContain('pointer-events-none')
    expect(table?.className).toContain('opacity-60')
  })

  it('does not set aria-busy when not loading', () => {
    const { container } = renderTable({ loading: false })
    const table = container.querySelector('table')
    expect(table?.getAttribute('aria-busy')).toBeNull()
  })
})

describe('custom render', () => {
  it('uses render function for cell content over key fallback', () => {
    const columns: ColumnDef<Row>[] = [
      {
        id: 'name',
        header: 'Name',
        render: (row) => (
          <strong data-testid={`name-cell-${row.id}`}>{row.name.toUpperCase()}</strong>
        ),
      },
    ]

    render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )

    expect(screen.getByTestId('name-cell-1')).toBeInTheDocument()
    expect(screen.getByTestId('name-cell-2')).toBeInTheDocument()
    expect(screen.getByText('ALICE')).toBeInTheDocument()
  })
})

describe('keyboard a11y', () => {
  it('sortable header buttons are present and not disabled', () => {
    renderTable()
    const thButtons = screen
      .getAllByRole('button')
      .filter((b) => b.closest('th') !== null)
    expect(thButtons.length).toBeGreaterThan(0)
    for (const btn of thButtons) {
      expect(btn.getAttribute('disabled')).toBeNull()
    }
  })
})

describe('onRowClick', () => {
  it('fires onRowClick when a data row is clicked', () => {
    const onRowClick = mock((_: Row) => {})
    render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onRowClick={onRowClick}
      />
    )

    const trs = screen.getAllByRole('row')
    // First row is thead; second row is first data row
    fireEvent.click(trs[1])
    expect(onRowClick).toHaveBeenCalledTimes(1)
    expect(onRowClick.mock.calls[0][0]).toEqual(rows[0])
  })
})

// ── Phase 1 Addition: PII mask retention ─────────────────────────────────────
describe('maskedColumnIds data-ph-mask attribute', () => {
  it('column with render function + in maskedColumnIds → td has data-ph-mask', () => {
    const columns: ColumnDef<Row>[] = [
      {
        id: 'name',
        header: 'Name',
        render: (row) => <span>{row.name}</span>,
      },
    ]
    const { container } = renderTable({ columns, maskedColumnIds: ['name'] })
    const tds = container.querySelectorAll('td[data-ph-mask]')
    expect(tds.length).toBe(rows.length)
  })

  it('column with key (no render) + in maskedColumnIds → td has data-ph-mask', () => {
    const columns: ColumnDef<Row>[] = [
      { id: 'amount', header: 'Amount', key: 'amount' },
    ]
    const { container } = renderTable({ columns, maskedColumnIds: ['amount'] })
    const tds = container.querySelectorAll('td[data-ph-mask]')
    expect(tds.length).toBe(rows.length)
  })

  it('column NOT in maskedColumnIds → td has NO data-ph-mask', () => {
    const columns: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', key: 'name' },
    ]
    const { container } = renderTable({ columns, maskedColumnIds: ['amount'] })
    const tds = container.querySelectorAll('td')
    for (const td of tds) {
      expect(td.hasAttribute('data-ph-mask')).toBe(false)
    }
  })

  it('null/undefined cell value in masked column → td still has data-ph-mask', () => {
    type NullRow = { id: string; val: string | null }
    const nullRows: NullRow[] = [{ id: '1', val: null }]
    const columns: ColumnDef<NullRow>[] = [
      { id: 'val', header: 'Val', key: 'val' },
    ]
    const { container } = render(
      <DataTable<NullRow>
        data={nullRows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        maskedColumnIds={['val']}
      />
    )
    const tds = container.querySelectorAll('td[data-ph-mask]')
    expect(tds.length).toBe(1)
  })
})

// ── Phase 1 Addition: rowSelection API skeleton ───────────────────────────────
describe('rowSelection prop', () => {
  it('DataTable without rowSelection renders no checkbox column', () => {
    const { container } = renderTable()
    const checkboxes = container.querySelectorAll('input[type="checkbox"]')
    expect(checkboxes.length).toBe(0)
  })

  it('DataTable with rowSelection={{}} renders without crashing', () => {
    const onRowSelectionChange = mock((_: Record<string, boolean>) => {})
    expect(() =>
      renderTable({
        rowSelection: {},
        onRowSelectionChange,
      } as Parameters<typeof renderTable>[0])
    ).not.toThrow()
  })
})

// ── Critical Regression Test 8 ───────────────────────────────────────────────
// Server-side filter boundary contract: DataTable never calls APIs.
// It only emits via onFiltersChange — parents decide whether that triggers
// a server refetch or an in-memory filter. This test locks the boundary so
// a future refactor cannot accidentally add a fetch() inside DataTable.
describe('server-side filter boundary', () => {
  it('does not call fetch when filters change — only emits via onFiltersChange', async () => {
    const fetchSpy = mock(() => Promise.resolve(new Response('{}', { status: 200 })))
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    try {
      const onFiltersChange = mock((_: FilterState) => {})
      const columns: ColumnDef<Row>[] = [
        {
          id: 'name',
          header: 'Name',
          filter: {
            type: 'select',
            options: [
              { label: 'Alice', value: 'Alice' },
              { label: 'Bob', value: 'Bob' },
            ],
            multiSelect: true,
          },
        },
        { id: 'amount', header: 'Amount', key: 'amount' },
      ]

      render(
        <DataTable<Row>
          data={rows}
          columns={columns}
          rowKey={(r) => r.id}
          sort={null}
          onSortChange={noopSort()}
          filters={{}}
          onFiltersChange={onFiltersChange}
        />
      )

      // Open the filter dropdown and select an option
      // Multi-select checkboxes fire onFiltersChange immediately on click (no Apply button)
      await userEvent.click(screen.getByRole('button', { name: /filter by name/i }))
      await userEvent.click(screen.getByRole('checkbox', { name: /alice/i }))

      // onFiltersChange must have fired
      expect(onFiltersChange).toHaveBeenCalledTimes(1)
      expect(onFiltersChange.mock.calls[0][0]).toEqual({ name: ['Alice'] })

      // fetch must NOT have been called — DataTable is data-agnostic
      expect(fetchSpy).not.toHaveBeenCalled()
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

// Phase 3a: Column Resize
describe('column resize', () => {
  const resizableColumns: ColumnDef<Row>[] = [
    { id: 'name', header: 'Name', key: 'name', resizable: true },
    { id: 'amount', header: 'Amount', key: 'amount' },
  ]

  it('renders resize handle when resizable:true; NOT rendered when resizable is absent', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handles = container.querySelectorAll('[data-testid="resize-handle"]')
    // Only the "name" column has resizable:true -> 1 handle
    expect(handles.length).toBe(1)
  })

  it('resize handle has correct ARIA attributes', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    expect(handle).not.toBeNull()
    expect(handle.getAttribute('role')).toBe('separator')
    expect(handle.getAttribute('aria-orientation')).toBe('vertical')
    const valuenow = handle.getAttribute('aria-valuenow')
    expect(valuenow).not.toBeNull()
    expect(Number(valuenow)).toBeGreaterThan(0)
  })

  it('keyboard ArrowRight increases column width by 8px', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    const before = Number(handle.getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    const after = Number(handle.getAttribute('aria-valuenow'))
    expect(after).toBe(before + 8)
  })

  it('keyboard ArrowLeft decreases column width by 8px', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    // bump up first so ArrowLeft won't hit MIN_WIDTH
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    const before = Number(handle.getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    const after = Number(handle.getAttribute('aria-valuenow'))
    expect(after).toBe(before - 8)
  })

  it('keyboard Shift+ArrowRight increases width by 32px', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    const before = Number(handle.getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle, { key: 'ArrowRight', shiftKey: true })
    const after = Number(handle.getAttribute('aria-valuenow'))
    expect(after).toBe(before + 32)
  })

  it('keyboard Shift+ArrowLeft decreases width by 32px', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    fireEvent.keyDown(handle, { key: 'ArrowRight', shiftKey: true }) // up first
    const before = Number(handle.getAttribute('aria-valuenow'))
    fireEvent.keyDown(handle, { key: 'ArrowLeft', shiftKey: true })
    const after = Number(handle.getAttribute('aria-valuenow'))
    expect(after).toBe(before - 32)
  })

  it('keyboard Home resets width to defaultSize', () => {
    const columnsWithDefault: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', key: 'name', resizable: true, defaultSize: 120 },
      { id: 'amount', header: 'Amount', key: 'amount' },
    ]
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={columnsWithDefault}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    fireEvent.keyDown(handle, { key: 'ArrowRight', shiftKey: true }) // change width
    fireEvent.keyDown(handle, { key: 'Home' })
    expect(Number(handle.getAttribute('aria-valuenow'))).toBe(120)
  })

  it('width cannot be resized below 50px (min-width)', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    for (let i = 0; i < 50; i++) {
      fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    }
    expect(Number(handle.getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(50)
  })

  it('coarse pointer: resize handle is aria-hidden', () => {
    const originalMatchMedia = window.matchMedia
    window.matchMedia = mock((query: string) => ({
      matches: query === '(pointer: fine)' ? false : true,
      media: query,
      onchange: null,
      addListener: mock(() => {}),
      removeListener: mock(() => {}),
      addEventListener: mock(() => {}),
      removeEventListener: mock(() => {}),
      dispatchEvent: mock(() => true),
    })) as unknown as typeof window.matchMedia

    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )

    const handle = container.querySelector('[data-testid="resize-handle"]')!
    expect(handle).not.toBeNull() // still in DOM
    expect(handle.getAttribute('aria-hidden')).toBe('true')

    window.matchMedia = originalMatchMedia
  })

  it('controlled mode: accepts columnSizing and calls onColumnSizingChange', () => {
    const onColumnSizingChange = mock((_: Record<string, number>) => {})
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnSizing={{ name: 200 }}
        onColumnSizingChange={onColumnSizingChange}
      />
    )
    const handle = container.querySelector('[data-testid="resize-handle"]')!
    expect(Number(handle.getAttribute('aria-valuenow'))).toBe(200)
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    expect(onColumnSizingChange).toHaveBeenCalledWith({ name: 208 })
  })

  // TODO: PostHog event on resize -- skip in automated tests (PostHog not configured in test infra).
  // Manual verification: confirm `column_resized` event fires with { column_id, new_width } in
  // PostHog dashboard after dragging or keyboard-resizing a column header in staging/prod.
})

// Phase 3b: Column Reorder
describe('column reorder', () => {
  const reorderableColumns: ColumnDef<Row>[] = [
    { id: 'name', header: 'Name', key: 'name', reorderable: true },
    { id: 'amount', header: 'Amount', key: 'amount', reorderable: true },
  ]

  it('renders drag handle when reorderable:true; NOT rendered when reorderable is absent', () => {
    const mixedColumns: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', key: 'name', reorderable: true },
      { id: 'amount', header: 'Amount', key: 'amount' },
    ]
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={mixedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    expect(handles.length).toBe(1)
  })

  it('drag handle has aria-pressed=false when not dragging', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelectorAll('[data-testid="drag-handle"]')[0]!
    expect(handle.getAttribute('aria-pressed')).toBe('false')
  })

  it('Space key picks up column: aria-pressed becomes true and aria-live announces pickup', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelectorAll('[data-testid="drag-handle"]')[0]!
    fireEvent.keyDown(handle, { key: ' ' })
    expect(handle.getAttribute('aria-pressed')).toBe('true')
    const liveRegion = container.querySelector('[aria-live="polite"]')!
    expect(liveRegion.textContent).toMatch(/Picked up column Name/i)
  })

  it('ArrowRight moves picked-up column one position right; live region updates', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    const nameHandle = handles[0]!
    fireEvent.keyDown(nameHandle, { key: ' ' })        // pick up
    fireEvent.keyDown(nameHandle, { key: 'ArrowRight' }) // move right
    const liveRegion = container.querySelector('[aria-live="polite"]')!
    expect(liveRegion.textContent).toMatch(/position 2/i)
  })

  it('Space again drops the column and calls onColumnOrderChange', () => {
    const onColumnOrderChange = mock((_: string[]) => {})
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onColumnOrderChange={onColumnOrderChange}
      />
    )
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    const nameHandle = handles[0]!
    fireEvent.keyDown(nameHandle, { key: ' ' })          // pick up
    fireEvent.keyDown(nameHandle, { key: 'ArrowRight' }) // move
    fireEvent.keyDown(nameHandle, { key: ' ' })          // drop
    expect(onColumnOrderChange).toHaveBeenCalledTimes(1)
    expect(onColumnOrderChange.mock.calls[0][0]).toEqual(['amount', 'name'])
  })

  it('Escape cancels reorder: column returns to original position, live announces cancel', () => {
    const onColumnOrderChange = mock((_: string[]) => {})
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onColumnOrderChange={onColumnOrderChange}
      />
    )
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    const nameHandle = handles[0]!
    fireEvent.keyDown(nameHandle, { key: ' ' })          // pick up
    fireEvent.keyDown(nameHandle, { key: 'ArrowRight' }) // move
    fireEvent.keyDown(nameHandle, { key: 'Escape' })     // cancel
    // onColumnOrderChange must NOT have been called
    expect(onColumnOrderChange).not.toHaveBeenCalled()
    const liveRegion = container.querySelector('[aria-live="polite"]')!
    expect(liveRegion.textContent).toMatch(/Cancelled/i)
  })

  it('ArrowLeft moves picked-up column one position left; live region updates', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnOrder={['name', 'amount']}
      />
    )
    // Pick up 'amount' (index 1) and move left to index 0
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    const amountHandle = handles[1]!
    fireEvent.keyDown(amountHandle, { key: ' ' })        // pick up amount
    fireEvent.keyDown(amountHandle, { key: 'ArrowLeft' }) // move left
    const liveRegion = container.querySelector('[aria-live="polite"]')!
    expect(liveRegion.textContent).toMatch(/position 1/i)
  })

  it('Enter key picks up column (alternate to Space)', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const handle = container.querySelectorAll('[data-testid="drag-handle"]')[0]!
    fireEvent.keyDown(handle, { key: 'Enter' })
    expect(handle.getAttribute('aria-pressed')).toBe('true')
    const liveRegion = container.querySelector('[aria-live="polite"]')!
    expect(liveRegion.textContent).toMatch(/Picked up column Name/i)
  })

  it('Enter key drops the column (alternate drop key)', () => {
    const onColumnOrderChange = mock((_: string[]) => {})
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onColumnOrderChange={onColumnOrderChange}
      />
    )
    const handles = container.querySelectorAll('[data-testid="drag-handle"]')
    const nameHandle = handles[0]!
    fireEvent.keyDown(nameHandle, { key: 'Enter' })      // pick up via Enter
    fireEvent.keyDown(nameHandle, { key: 'ArrowRight' }) // move right
    fireEvent.keyDown(nameHandle, { key: 'Enter' })      // drop via Enter
    expect(onColumnOrderChange).toHaveBeenCalledTimes(1)
    expect(onColumnOrderChange.mock.calls[0][0]).toEqual(['amount', 'name'])
  })

  it('ArrowRight does nothing when not in dragging mode', () => {
    const onColumnOrderChange = mock((_: string[]) => {})
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        onColumnOrderChange={onColumnOrderChange}
      />
    )
    const handle = container.querySelectorAll('[data-testid="drag-handle"]')[0]!
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    expect(onColumnOrderChange).not.toHaveBeenCalled()
  })

  it('controlled mode: accepts columnOrder and renders columns in given order', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnOrder={['amount', 'name']}
      />
    )
    const ths = container.querySelectorAll('th')
    // First column header should be Amount
    expect(ths[0].textContent).toContain('Amount')
    expect(ths[1].textContent).toContain('Name')
  })

  it('aria-live region exists in DOM for screen reader announcements', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={reorderableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const liveRegion = container.querySelector('[aria-live="polite"]')
    expect(liveRegion).not.toBeNull()
    expect(liveRegion!.getAttribute('aria-atomic')).toBe('true')
  })
})

// Phase 3d: Column Visibility
describe('column visibility', () => {
  const visibilityColumns: ColumnDef<Row>[] = [
    { id: 'name', header: 'Name', key: 'name', hideable: true },
    { id: 'amount', header: 'Amount', key: 'amount', hideable: true },
    { id: 'id', header: 'ID', key: 'id' }, // non-hideable
  ]

  it('hideable column appears in ColumnVisibilityMenu; non-hideable does not', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{name: true, amount: true}}
        onColumnVisibilityChange={mock((_: Record<string, boolean>) => {})}
      />
    )
    // Menu trigger should be present
    expect(screen.getByRole('button', { name: /columns/i })).toBeInTheDocument()
    // Open the menu
    fireEvent.click(screen.getByRole('button', { name: /columns/i }))
    // hideable columns should be in menu
    const menuItems = screen.getAllByRole('menuitemcheckbox')
    const labels = menuItems.map((el) => el.textContent)
    expect(labels).toContain('Name')
    expect(labels).toContain('Amount')
    // non-hideable column should NOT appear
    expect(labels).not.toContain('ID')
  })

  it('toggling a checkbox calls onColumnVisibilityChange with column id set to false', () => {
    const onColumnVisibilityChange = mock((_: Record<string, boolean>) => {})
    render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{ name: true, amount: true }}
        onColumnVisibilityChange={onColumnVisibilityChange}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /columns/i }))
    const nameItem = screen.getByRole('menuitemcheckbox', { name: 'Name' })
    fireEvent.click(nameItem)
    expect(onColumnVisibilityChange).toHaveBeenCalledWith({ name: false, amount: true })
  })

  it('hidden column (visibility[id]=false) is not rendered in the table', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{ name: false, amount: true }}
        onColumnVisibilityChange={mock((_: Record<string, boolean>) => {})}
      />
    )
    // Name column header and cells should not be present
    const ths = container.querySelectorAll('th')
    const thTexts = Array.from(ths).map((th) => th.textContent)
    expect(thTexts.some((t) => t?.includes('Name'))).toBe(false)
    // Alice and Bob are row data but via the 'name' column — should be absent
    expect(screen.queryByText('Alice')).toBeNull()
    expect(screen.queryByText('Bob')).toBeNull()
  })

  it('hidden column still exists in orderedColumns: when made visible again it returns to same position', () => {
    const { rerender, container } = render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{ name: false, amount: true }}
        onColumnVisibilityChange={mock((_: Record<string, boolean>) => {})}
      />
    )
    // name is hidden — check amount is first visible column in thead
    let ths = container.querySelectorAll('th')
    expect(ths[0].textContent).toContain('Amount')

    // make name visible again
    rerender(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{ name: true, amount: true }}
        onColumnVisibilityChange={mock((_: Record<string, boolean>) => {})}
      />
    )
    // name should be back at position 0 (original order)
    ths = container.querySelectorAll('th')
    expect(ths[0].textContent).toContain('Name')
  })

  it('ColumnVisibilityMenu is not rendered when no column has hideable:true', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    // baseColumns have no hideable:true → no Columns button
    expect(screen.queryByRole('button', { name: /columns/i })).toBeNull()
  })

  it('ColumnVisibilityMenu is not rendered when hideable columns exist but onColumnVisibilityChange is absent', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        // onColumnVisibilityChange intentionally omitted — menu should not render
      />
    )
    expect(screen.queryByRole('button', { name: /columns/i })).toBeNull()
  })

  it('ArrowDown moves focus to next menu item, ArrowUp wraps to last item', async () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={visibilityColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        columnVisibility={{}}
        onColumnVisibilityChange={mock((_: Record<string, boolean>) => {})}
      />
    )
    // Open menu
    fireEvent.click(screen.getByRole('button', { name: /columns/i }))
    const items = screen.getAllByRole('menuitemcheckbox')
    // First item should be focused on open
    expect(document.activeElement).toBe(items[0])
    // ArrowDown moves focus to second item
    fireEvent.keyDown(items[0], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[1])
    // ArrowUp from second item moves to first
    fireEvent.keyDown(items[1], { key: 'ArrowUp' })
    expect(document.activeElement).toBe(items[0])
  })
})

// Phase 3d: Density
describe('density', () => {
  it('density="compact" applies compact padding classes to body cells', () => {
    const { container } = renderTable({ density: 'compact' })
    const td = container.querySelector('tbody td')!
    expect(td.className).toContain('px-2')
    expect(td.className).toContain('py-1.5')
    expect(td.className).toContain('text-xs')
  })

  it('density="spacious" applies spacious padding classes to body cells', () => {
    const { container } = renderTable({ density: 'spacious' })
    const td = container.querySelector('tbody td')!
    expect(td.className).toContain('px-6')
    expect(td.className).toContain('py-5')
  })

  it('default density (no prop) uses comfortable padding (unchanged behavior)', () => {
    const { container } = renderTable()
    const td = container.querySelector('tbody td')!
    // comfortable defaults: px-4 py-3.5 (same as before)
    expect(td.className).toContain('px-4')
    expect(td.className).toContain('py-3.5')
  })
})

// Phase 3c: Column Pin
describe('column pin', () => {
  const pinnedColumns: ColumnDef<Row>[] = [
    { id: 'name', header: 'Name', key: 'name', defaultPinned: 'left' },
    { id: 'amount', header: 'Amount', key: 'amount', defaultPinned: 'right' },
  ]

  it('left-pinned column th and td both get data-pinned="left"', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    // th
    const pinnedTh = container.querySelector('th[data-pinned="left"]')
    expect(pinnedTh).not.toBeNull()
    // tds — one per data row
    const pinnedTds = container.querySelectorAll('td[data-pinned="left"]')
    expect(pinnedTds.length).toBe(rows.length)
  })

  it('right-pinned column th and td both get data-pinned="right"', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const pinnedTh = container.querySelector('th[data-pinned="right"]')
    expect(pinnedTh).not.toBeNull()
    const pinnedTds = container.querySelectorAll('td[data-pinned="right"]')
    expect(pinnedTds.length).toBe(rows.length)
  })

  it('non-pinned column has no data-pinned attribute', () => {
    const columns: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', key: 'name' },
    ]
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={columns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const anyPinned = container.querySelectorAll('[data-pinned]')
    expect(anyPinned.length).toBe(0)
  })

  it('left-pinned th has sticky, left-0, and background classes', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const pinnedTh = container.querySelector('th[data-pinned="left"]')!
    expect(pinnedTh.className).toContain('sticky')
    expect(pinnedTh.className).toContain('left-0')
    expect(pinnedTh.className).toContain('bg-muted')
  })

  it('right-pinned th has sticky, right-0, background, and mobile degradation classes', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const pinnedTh = container.querySelector('th[data-pinned="right"]')!
    expect(pinnedTh.className).toContain('sticky')
    expect(pinnedTh.className).toContain('right-0')
    expect(pinnedTh.className).toContain('bg-muted')
    expect(pinnedTh.className).toContain('max-md:relative')
  })

  it('left-pinned td has sticky, left-0, z-10, and bg-card classes', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const pinnedTd = container.querySelector('td[data-pinned="left"]')!
    expect(pinnedTd.className).toContain('sticky')
    expect(pinnedTd.className).toContain('left-0')
    expect(pinnedTd.className).toContain('z-10')
    expect(pinnedTd.className).toContain('bg-card')
  })

  it('right-pinned td has sticky, right-0, z-10, bg-card, and max-md:relative classes', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={pinnedColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    const pinnedTd = container.querySelector('td[data-pinned="right"]')!
    expect(pinnedTd.className).toContain('sticky')
    expect(pinnedTd.className).toContain('right-0')
    expect(pinnedTd.className).toContain('z-10')
    expect(pinnedTd.className).toContain('bg-card')
    expect(pinnedTd.className).toContain('max-md:relative')
  })
})

// ── Phase G1/RC2: columnheader a11y-name ─────────────────────────────────────
describe('columnheader accessible name (RC2 — DragHandle pollution fix)', () => {
  it('reorderable+sortable column: getByRole columnheader finds header by exact column label', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={[{ id: 'name', header: 'Account', sortable: true, reorderable: true, key: 'name' }]}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    // Without aria-label on <th>, "Name from Contents" would compute
    // "Drag to reorder column Account" — anchored regex would fail.
    // With aria-label={col.header} the header's accessible name is just "Account".
    const header = screen.getByRole('columnheader', { name: /^Account$/i })
    expect(header).toBeInTheDocument()
  })

  it('non-reorderable column: columnheader accessible name is derived from header text naturally', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={[{ id: 'name', header: 'Balance', sortable: true, key: 'name' }]}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    // Non-reorderable columns have no DragHandle — no pollution, no aria-label needed.
    const header = screen.getByRole('columnheader', { name: /^Balance$/i })
    expect(header).toBeInTheDocument()
  })
})

// ── Phase G1/D2b: DragHandle column-specific accessible names ─────────────────
describe('DragHandle column-specific accessible names (D2b — Codex finding)', () => {
  const multiReorderColumns: ColumnDef<Row>[] = [
    { id: 'name', header: 'Account', sortable: true, reorderable: true, key: 'name' },
    { id: 'amount', header: 'Balance', sortable: true, reorderable: true, key: 'amount' },
  ]

  it('each drag handle has a unique column-specific accessible name', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={multiReorderColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    expect(screen.getByRole('button', { name: /reorder Account column/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /reorder Balance column/i })).toBeInTheDocument()
  })

  it('sort button is still found by exact column name (not confused with DragHandle)', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={multiReorderColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
      />
    )
    // Sort button has accessible name "Account" (just the label text from TableHeaderCell)
    // DragHandle has name "Reorder Account column" — does NOT match /^Account$/i
    const sortBtn = screen.getByRole('columnheader', { name: /^Account$/i })
      .querySelector('button:not([data-testid="drag-handle"])')
    expect(sortBtn).not.toBeNull()
  })
})

// ── Task I: Row Virtualization ────────────────────────────────────────────────
describe('DataTable virtualization', () => {
  // JSDOM doesn't implement real layout.
  // @tanstack/react-virtual reads offsetHeight (via getRect) synchronously on mount
  // to determine the visible viewport size. Without mocking it, outerSize = 0 and
  // the virtualizer renders nothing. We also mock getBoundingClientRect for
  // measureElement (dynamic row heights).
  const originalGetBCR = Element.prototype.getBoundingClientRect
  let offsetHeightDesc: PropertyDescriptor | undefined
  let offsetWidthDesc: PropertyDescriptor | undefined

  beforeEach(() => {
    Element.prototype.getBoundingClientRect = mock(() => ({
      height: 56,
      width: 800,
      top: 0,
      bottom: 56,
      left: 0,
      right: 800,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    }))
    offsetHeightDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetHeight')
    offsetWidthDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetWidth')
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
      configurable: true,
      get: () => 400,
    })
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
      configurable: true,
      get: () => 800,
    })
  })

  afterEach(() => {
    Element.prototype.getBoundingClientRect = originalGetBCR
    if (offsetHeightDesc) {
      Object.defineProperty(HTMLElement.prototype, 'offsetHeight', offsetHeightDesc)
    } else {
      // Restore to undefined if it wasn't originally defined
      delete (HTMLElement.prototype as unknown as Record<string, unknown>)['offsetHeight']
    }
    if (offsetWidthDesc) {
      Object.defineProperty(HTMLElement.prototype, 'offsetWidth', offsetWidthDesc)
    } else {
      delete (HTMLElement.prototype as unknown as Record<string, unknown>)['offsetWidth']
    }
  })

  const manyRows: Row[] = Array.from({ length: 1000 }, (_, i) => ({
    id: String(i + 1),
    name: `User ${i + 1}`,
    amount: (i + 1) * 10,
  }))

  it('renders a scroll container div with overflow-auto when virtualization.enabled=true', () => {
    const { container } = render(
      <DataTable<Row>
        data={manyRows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
          overscan: 8,
          scrollContainerHeight: '400px',
        }}
      />
    )
    const scrollContainer = container.querySelector('[data-testid="virtual-scroll-container"]')
    expect(scrollContainer).not.toBeNull()
    expect(scrollContainer?.className).toContain('overflow-auto')
  })

  it('tbody has position:relative style in virtualized path', () => {
    const { container } = render(
      <DataTable<Row>
        data={manyRows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
          scrollContainerHeight: '400px',
        }}
      />
    )
    const tbody = container.querySelector('tbody')
    expect(tbody?.style.position).toBe('relative')
  })

  it('shows sentinel row when isFetchingMore=true', () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
          isFetchingMore: true,
        }}
      />
    )
    expect(screen.getByText(/loading more/i)).toBeInTheDocument()
  })

  it('shows sentinel row when hasMore=true', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
          hasMore: true,
        }}
      />
    )
    // Sentinel tr exists (even though it has no text when only hasMore and not isFetchingMore)
    const sentinelTd = container.querySelector('[data-testid="virtual-sentinel"]')
    expect(sentinelTd).not.toBeNull()
  })

  it('preserves data-ph-mask on virtualized rows', () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        maskedColumnIds={['amount']}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
        }}
      />
    )
    const maskedCells = container.querySelectorAll('td[data-ph-mask]')
    // At least some rows rendered (JSDOM may virtualise to a small set)
    expect(maskedCells.length).toBeGreaterThan(0)
    // All masked cells belong to the amount column (not name)
    for (const cell of maskedCells) {
      expect(cell.textContent).not.toBe('Alice')
      expect(cell.textContent).not.toBe('Bob')
    }
  })

  it('non-virtualized path unchanged — no scroll container', () => {
    const { container } = renderTable()
    const scrollContainer = container.querySelector('[data-testid="virtual-scroll-container"]')
    expect(scrollContainer).toBeNull()
  })

  it('calls onScrollEnd when last virtual item is within prefetchRows of the end', () => {
    const onScrollEnd = mock(() => {})
    // Use a tiny dataset (5 rows) so the virtualizer renders ALL rows in the
    // initial viewport (offsetHeight=400, estimateRowHeight=56 → ~7 visible).
    // With prefetchRows=3 the threshold is count-3 = 2. lastIndex(4) >= 2 → fires.
    const tinyRows: Row[] = Array.from({ length: 5 }, (_, i) => ({
      id: String(i + 1),
      name: `User ${i + 1}`,
      amount: (i + 1) * 10,
    }))
    render(
      <DataTable<Row>
        data={tinyRows}
        columns={baseColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        virtualization={{
          enabled: true,
          estimateRowHeight: 56,
          prefetchRows: 3,
          hasMore: true,
          onScrollEnd,
        }}
      />
    )
    // The virtualizer renders all 5 rows on mount (viewport=400px > total=5×56=280px).
    // The useEffect fires: lastIndex(4) >= count(5)-prefetchRows(3)=2 → onScrollEnd called.
    expect(onScrollEnd).toHaveBeenCalled()
  })
})
