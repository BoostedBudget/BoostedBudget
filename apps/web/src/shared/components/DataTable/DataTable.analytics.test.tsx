import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, fireEvent, act } from '@testing-library/react'
import { DataTable } from './DataTable'
import type { ColumnDef, SortState } from './types'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

// Narrow, focused file — mock.module is process-global but safe here since
// this file tests only analytics behavior and runs in its own Bun process.
const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

type Row = { id: string; name: string; amount: number }

const rows: Row[] = [
  { id: '1', name: 'Alice', amount: 100 },
]

const resizableColumns: ColumnDef<Row>[] = [
  { id: 'name', header: 'Name', key: 'name', resizable: true, defaultSize: 150, minSize: 50, maxSize: 400 },
  { id: 'amount', header: 'Amount', key: 'amount' },
]

const noopSort = () => mock((_: SortState) => {})
const noopFilters = () => mock(() => {})

describe('DataTable — PostHog analytics', () => {
  beforeEach(() => mockTrackEvent.mockClear())

  it('fires DATATABLE_COLUMN_RESIZED on pointer-drag completion', async () => {
    const { container } = render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        tableId="test-analytics"
      />
    )

    const handle = container.querySelector('[data-testid="resize-handle"]')
    expect(handle).not.toBeNull()

    await act(async () => {
      fireEvent.pointerDown(handle!, { clientX: 100, pointerId: 1 })
    })
    await act(async () => {
      fireEvent.mouseMove(document, { clientX: 150 })
    })
    await act(async () => {
      fireEvent.mouseUp(document, { clientX: 150 })
    })

    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED,
      { tableId: 'test-analytics', columnId: 'name' }
    )
  })

  it('does NOT fire DATATABLE_COLUMN_RESIZED when no drag occurs', async () => {
    render(
      <DataTable<Row>
        data={rows}
        columns={resizableColumns}
        rowKey={(r) => r.id}
        sort={null}
        onSortChange={noopSort()}
        filters={{}}
        onFiltersChange={noopFilters()}
        tableId="test-analytics"
      />
    )

    await act(async () => {})
    expect(mockTrackEvent).not.toHaveBeenCalledWith(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED, expect.anything())
  })

  it('uses tableId=undefined when tableId prop is absent', async () => {
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

    await act(async () => { fireEvent.pointerDown(handle, { clientX: 100, pointerId: 1 }) })
    await act(async () => { fireEvent.mouseMove(document, { clientX: 160 }) })
    await act(async () => { fireEvent.mouseUp(document, { clientX: 160 }) })

    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED,
      { tableId: undefined, columnId: 'name' }
    )
  })
})
