import { describe, it, expect } from 'bun:test'
import { toTanStackSizingColumns } from './columnSizingAdapter'
import type { ColumnDef } from '../types'

type Row = { id: string; name: string }

describe('toTanStackSizingColumns', () => {
  it('maps id, size, minSize, maxSize, and enableResizing', () => {
    const cols: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', defaultSize: 200, minSize: 120, maxSize: 400, resizable: true },
    ]
    const result = toTanStackSizingColumns(cols)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('name')
    expect(result[0].size).toBe(200)
    expect(result[0].minSize).toBe(120)
    expect(result[0].maxSize).toBe(400)
    expect(result[0].enableResizing).toBe(true)
  })

  it('omits size/minSize/maxSize when not provided — lets TanStack use its own defaults', () => {
    const cols: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name' },
    ]
    const result = toTanStackSizingColumns(cols)
    expect(result[0].size).toBeUndefined()
    expect(result[0].minSize).toBeUndefined()
    expect(result[0].maxSize).toBeUndefined()
  })

  it('sets enableResizing: false for columns without resizable: true', () => {
    const cols: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', resizable: false },
      { id: 'amount', header: 'Amount' },
    ]
    const result = toTanStackSizingColumns(cols)
    expect(result[0].enableResizing).toBe(false)
    expect(result[1].enableResizing).toBe(false)
  })

  it('maps multiple columns in order', () => {
    const cols: ColumnDef<Row>[] = [
      { id: 'name', header: 'Name', defaultSize: 200 },
      { id: 'id', header: 'ID', defaultSize: 80 },
    ]
    const result = toTanStackSizingColumns(cols)
    expect(result.map((c) => c.id)).toEqual(['name', 'id'])
    expect(result[0].size).toBe(200)
    expect(result[1].size).toBe(80)
  })
})
