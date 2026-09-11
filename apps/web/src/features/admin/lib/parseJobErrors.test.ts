import { describe, test, expect } from 'bun:test'
import { parseJobErrors, pgErrorSummary } from './parseJobErrors'

// The motivating production row (#1631): a holdings sync that dead-lettered five times. The old
// shape showed only `message` — one flattened line — while Postgres had already sent the Detail
// naming the exact colliding row.
const NEW_SHAPE_ROW = [
  {
    attempt: 5,
    message:
      'webhook processing failed: investment sync failed: failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02: ERROR: duplicate key value violates unique constraint "unique_account_security" (SQLSTATE 23505)',
    timestamp: '2026-04-01T11:00:00Z',
    chain: [
      'webhook processing failed',
      'investment sync failed',
      'failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02',
      'ERROR: duplicate key value violates unique constraint "unique_account_security" (SQLSTATE 23505)',
    ],
    pg_error: {
      code: '23505',
      severity: 'ERROR',
      message: 'duplicate key value violates unique constraint "unique_account_security"',
      detail: 'Key (account_id, security_id)=(ae944376-…, 7c1f…) already exists.',
      constraint_name: 'unique_account_security',
      table_name: 'holdings',
      schema_name: 'public',
      routine: '_bt_check_unique',
    },
  },
]

// ⚠ HARD REQUIREMENT: `dead_letter_jobs.errors` is an existing JSON column with production rows
// already in the OLD {attempt, message, timestamp} shape. They must come through this parser
// intact, with every new field simply absent — never undefined-dereferenced, never thrown on.
const OLD_SHAPE_ROW = [
  { attempt: 1, message: 'sync failed: connection reset', timestamp: '2026-04-01T11:00:00Z' },
  { attempt: 2, message: 'sync failed: connection reset', timestamp: '2026-04-01T11:05:00Z' },
]

describe('parseJobErrors', () => {
  test('parses the new shape, keeping the chain ordered and the Postgres context structured', () => {
    const parsed = parseJobErrors(NEW_SHAPE_ROW)
    expect(parsed).toHaveLength(1)
    expect(parsed[0].attempt).toBe(5)
    expect(parsed[0].chain).toEqual(NEW_SHAPE_ROW[0].chain)
    expect(parsed[0].pg?.code).toBe('23505')
    expect(parsed[0].pg?.constraint_name).toBe('unique_account_security')
    expect(parsed[0].pg?.detail).toContain('already exists')
    // The flattened message is still there — nothing was replaced.
    expect(parsed[0].message).toBe(NEW_SHAPE_ROW[0].message)
  })

  test('parses an OLD production row with every new field absent', () => {
    const parsed = parseJobErrors(OLD_SHAPE_ROW)
    expect(parsed).toHaveLength(2)
    expect(parsed[0].message).toBe('sync failed: connection reset')
    expect(parsed[0].attempt).toBe(1)
    expect(parsed[0].chain).toEqual([])
    expect(parsed[0].pg).toBeUndefined()
  })

  test('accepts the column as a JSON string as well as a parsed array', () => {
    expect(parseJobErrors(JSON.stringify(OLD_SHAPE_ROW))).toHaveLength(2)
    expect(parseJobErrors(JSON.stringify(NEW_SHAPE_ROW))[0].pg?.code).toBe('23505')
  })

  test('returns [] for every absent or malformed value instead of throwing', () => {
    for (const value of [null, undefined, '', 'not json', 42, {}, [], [null, 7, 'x'], { a: 1 }]) {
      expect(() => parseJobErrors(value)).not.toThrow()
      expect(Array.isArray(parseJobErrors(value))).toBe(true)
    }
    expect(parseJobErrors([null, 7, 'x'])).toEqual([])
  })

  test('tolerates partial and wrongly-typed members of the new fields', () => {
    const parsed = parseJobErrors([
      { message: 'boom', chain: 'not-an-array', pg_error: 'not-an-object' },
      { message: 'boom2', chain: ['a', 5, null, 'b'], pg_error: { code: 7, detail: 'kept' } },
    ])
    expect(parsed[0].chain).toEqual([])
    expect(parsed[0].pg).toBeUndefined()
    // Non-string chain members are dropped, not rendered as "null".
    expect(parsed[1].chain).toEqual(['a', 'b'])
    expect(parsed[1].pg?.code).toBeUndefined()
    expect(parsed[1].pg?.detail).toBe('kept')
  })

  test('a message that is missing entirely still yields a renderable entry', () => {
    const parsed = parseJobErrors([{ attempt: 1 }])
    expect(parsed).toHaveLength(1)
    expect(parsed[0].message).toBe('')
    expect(parsed[0].attempt).toBe(1)
  })
})

describe('pgErrorSummary', () => {
  test('summarises with SQLSTATE and constraint only — never a row-value-bearing field', () => {
    const summary = pgErrorSummary(parseJobErrors(NEW_SHAPE_ROW))
    expect(summary).toEqual({ code: '23505', constraintName: 'unique_account_security' })
  })

  test('is undefined for old rows, which carry no Postgres context', () => {
    expect(pgErrorSummary(parseJobErrors(OLD_SHAPE_ROW))).toBeUndefined()
    expect(pgErrorSummary([])).toBeUndefined()
  })

  test('summarises the LAST attempt that has Postgres context', () => {
    const summary = pgErrorSummary(
      parseJobErrors([
        { message: 'a', pg_error: { code: '40001' } },
        { message: 'b', pg_error: { code: '23505', constraint_name: 'c2' } },
      ])
    )
    expect(summary).toEqual({ code: '23505', constraintName: 'c2' })
  })
})
