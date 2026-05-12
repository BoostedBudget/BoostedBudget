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
