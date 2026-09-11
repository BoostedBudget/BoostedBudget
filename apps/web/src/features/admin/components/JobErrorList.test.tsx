/**
 * JobErrorList — the structured DLQ error view (#1631).
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { JobErrorList } from './JobErrorList'

const MOTIVATING_DETAIL =
  'Key (account_id, security_id)=(ae944376-3d01-41eb-bc3e-f275a3c36c02, 7c1f0b2e-1111-4444-8888-999999999999) already exists.'

const NEW_SHAPE = [
  {
    attempt: 5,
    message:
      'webhook processing failed: investment sync failed: failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02: ERROR: duplicate key value violates unique constraint "unique_account_security" (SQLSTATE 23505)',
    timestamp: '2026-04-01T11:00:00Z',
    chain: [
      'webhook processing failed',
      'investment sync failed',
      'failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02',
    ],
    pg_error: {
      code: '23505',
      severity: 'ERROR',
      message: 'duplicate key value violates unique constraint "unique_account_security"',
      detail: MOTIVATING_DETAIL,
      hint: 'consider upserting instead',
      constraint_name: 'unique_account_security',
      table_name: 'holdings',
      schema_name: 'public',
      column_name: 'security_id',
      routine: '_bt_check_unique',
    },
  },
]

// ⚠ Existing production rows. Rendering one must not throw and must not lose the message.
const OLD_SHAPE = [
  { attempt: 1, message: 'sync failed: connection reset', timestamp: '2026-04-01T11:00:00Z' },
]

describe('JobErrorList', () => {
  it('shows the SQLSTATE and the constraint name inline — the two things triage needs first', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    expect(screen.getByText('23505')).toBeInTheDocument()
    expect(screen.getByText('unique_account_security')).toBeInTheDocument()
    expect(screen.getByText(/attempt 5/i)).toBeInTheDocument()
  })

  it('surfaces the Postgres DETAIL that used to be discarded', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    expect(screen.getByText(MOTIVATING_DETAIL)).toBeInTheDocument()
  })

  it('keeps the wrapped chain as ordered, separate layers', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    const items = screen.getAllByTestId('job-error-chain-layer').map((el) => el.textContent)
    expect(items).toEqual(NEW_SHAPE[0].chain)
  })

  it('puts the chain and the SQL strings behind an expander, collapsed by default', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    const expander = screen.getByTestId('job-error-expander') as HTMLDetailsElement
    expect(expander.open).toBe(false)
  })

  // ── PII ───────────────────────────────────────────────────────────────────────────────────
  it('masks every SQL free-text field — detail, hint, pg message, chain layers, raw message', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    for (const text of [
      MOTIVATING_DETAIL,
      'consider upserting instead',
      'duplicate key value violates unique constraint "unique_account_security"',
      'failed to replace holdings for account ae944376-3d01-41eb-bc3e-f275a3c36c02',
    ]) {
      const el = screen.getByText(text)
      expect(el.closest('[data-ph-mask]')).not.toBeNull()
    }
  })

  it('never puts a SQL string in an attribute — replay records attributes verbatim and cannot mask them', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    const UNMASKABLE = ['aria-label', 'title', 'alt', 'placeholder', 'aria-description']
    const secrets = [
      MOTIVATING_DETAIL,
      'consider upserting instead',
      'duplicate key value violates unique constraint',
      'failed to replace holdings for account',
    ]
    for (const el of Array.from(document.body.querySelectorAll('*'))) {
      for (const attr of el.getAttributeNames()) {
        if (!UNMASKABLE.includes(attr)) continue
        const value = el.getAttribute(attr) ?? ''
        for (const secret of secrets) expect(value).not.toContain(secret)
      }
    }
  })

  it('leaves SQLSTATE and constraint name legible — they are schema identifiers, not row data', () => {
    render(<JobErrorList errors={NEW_SHAPE} />)
    expect(screen.getByText('23505').closest('[data-ph-mask]')).toBeNull()
    expect(screen.getByText('unique_account_security').closest('[data-ph-mask]')).toBeNull()
  })

  // ── Backward compatibility ───────────────────────────────────────────────────────────────
  it('renders an OLD production row without throwing and without losing the message', () => {
    expect(() => render(<JobErrorList errors={OLD_SHAPE} />)).not.toThrow()
    const message = screen.getByText('sync failed: connection reset')
    expect(message).toBeInTheDocument()
    expect(message.closest('[data-ph-mask]')).not.toBeNull()
    // Nothing claims a SQLSTATE the row never carried.
    expect(screen.queryByTestId('job-error-sqlstate')).toBeNull()
    expect(screen.queryByTestId('job-error-chain-layer')).toBeNull()
  })

  it('renders the OLD shape as a raw JSON string from the column, too', () => {
    render(<JobErrorList errors={JSON.stringify(OLD_SHAPE)} />)
    expect(screen.getByText('sync failed: connection reset')).toBeInTheDocument()
  })

  it('degrades to a placeholder for absent, empty or unparseable columns', () => {
    for (const value of [null, undefined, '', 'not json', 42, {}, []]) {
      const { unmount } = render(<JobErrorList errors={value} />)
      expect(screen.getByText('(none)')).toBeInTheDocument()
      unmount()
    }
  })

  it('renders an entry that carries no attempt number without printing "undefined"', () => {
    render(<JobErrorList errors={[{ message: 'boom' }]} />)
    expect(screen.getByText('boom')).toBeInTheDocument()
    expect(document.body.textContent ?? '').not.toContain('undefined')
  })
})
