import { describe, it, expect, mock } from 'bun:test'
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
    fireEvent.keyDown(nameHandle, { key: ' ' })          // pick up
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
